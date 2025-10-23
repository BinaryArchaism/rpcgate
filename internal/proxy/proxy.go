package proxy

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"github.com/BinaryArchaism/rpcgate/internal/balancer"
	"github.com/BinaryArchaism/rpcgate/internal/config"
)

type Server struct {
	srv                 *fasthttp.Server
	cli                 *fasthttp.Client
	port                string
	rr                  map[string]*balancer.RoundRobin
	noRequestValidation bool
	rpcs                []config.RPC
	done                chan struct{}
}

func New(cfg config.Config) *Server {
	var srv Server

	var cli fasthttp.Client
	srv.cli = &cli
	srv.rpcs = cfg.RPCs
	srv.port = cfg.Port
	srv.done = make(chan struct{})
	srv.rr = make(map[string]*balancer.RoundRobin)
	srv.noRequestValidation = cfg.NoRequestValidation

	handler := srv.recoverHandler(
		srv.loggingMiddleware(
			srv.metricsMiddleware(
				srv.routerHandler(
					srv.requestValidationMiddleware(
						srv.handler)))))

	srv.srv = &fasthttp.Server{ //nolint: exhaustruct // server setup
		Handler: handler,
	}

	return &srv
}

func (srv *Server) Start(ctx context.Context) {
	go func() {
		err := srv.srv.ListenAndServe(srv.port)
		if err != nil {
			log.Ctx(ctx).Panic().Err(err).Msg("Proxy server failed to start")
		}
	}()
	log.Ctx(ctx).Info().Msg("Proxy server started")
}

func (srv *Server) Stop() {
	err := srv.srv.Shutdown()
	if err != nil {
		log.Panic().Err(err).Msg("Proxy server failed to stop")
	}
	log.Info().Msg("Proxy server stopped")
}

func (srv *Server) handler(ctx *fasthttp.RequestCtx) {
	url := srv.rr[string(ctx.RequestURI())].Next()
	log.Debug().Uint64("id", ctx.ID()).Str("url", url).Msg("request goes to")

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(url)
	req.SetBody(ctx.Request.Body())
	req.Header.SetMethod(fasthttp.MethodPost)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	err := srv.cli.Do(req, resp)
	if err != nil {
		log.Error().Err(err).Msg("error while request")
		return
	}

	_, err = io.Copy(ctx, bytes.NewReader(resp.Body()))
	if err != nil {
		log.Error().Err(err).Msg("error while request")
		return
	}
}

func (srv *Server) recoverHandler(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Any("panic", r).Stack().Msg("panic at handler")
				ctx.Error("internal server error", fasthttp.StatusInternalServerError)
			}
		}()
		f(ctx)
	}
}

func (srv *Server) loggingMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		f(ctx)
		log.Info().
			Uint64("id", ctx.ID()).
			Uint64("conn_id", ctx.ConnID()).
			Str("remote_ip", ctx.RemoteIP().String()).
			Int("status", ctx.Response.StatusCode()).
			Str("latency", time.Since(start).String()).
			Msg("request complete")
	}
}

func (srv *Server) requestValidationMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	if srv.noRequestValidation {
		return func(ctx *fasthttp.RequestCtx) {
			f(ctx)
		}
	}
	return func(ctx *fasthttp.RequestCtx) {
		f(ctx)
	}
}

func (srv *Server) metricsMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		f(ctx)
	}
}

func (srv *Server) routerHandler(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const base = 10
	chainToConnUrls := make(map[string][]string)
	chains := make(map[string]struct{})

	for _, rpc := range srv.rpcs {
		key := "/" + strconv.FormatInt(rpc.ChainID, base)
		chains[key] = struct{}{}
		for _, provider := range rpc.Providers {
			chainToConnUrls[key] = append(chainToConnUrls[key], provider.ConnURL)
		}
	}
	for chain, urls := range chainToConnUrls {
		srv.rr[chain] = balancer.NewRoundRobin(urls)
	}

	return func(ctx *fasthttp.RequestCtx) {
		_, exist := chains[string(ctx.Request.RequestURI())]
		if !exist {
			log.Debug().Msg("unknown path")
			ctx.Error("not found", fasthttp.StatusNotFound)
			return
		}
		f(ctx)
	}
}
