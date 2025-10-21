package proxy

import (
	"bytes"
	"context"
	"io"

	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"github.com/BinaryArchaism/rpcgate/internal/balancer"
	"github.com/BinaryArchaism/rpcgate/internal/config"
)

type Server struct {
	srv                 *fasthttp.Server
	cli                 *fasthttp.Client
	port                string
	rr                  *balancer.RoundRobin
	noRequestValidation bool
	done                chan struct{}
}

func New(cfg config.Config) *Server {
	var srv Server

	handler := srv.recoverHandler(
		srv.loggingMiddleware(
			srv.metricsMiddleware(
				srv.requestValidationMiddleware(
					srv.handler))))

	srv.srv = &fasthttp.Server{ //nolint: exhaustruct // server setup
		Handler: handler,
	}
	var cli fasthttp.Client
	srv.cli = &cli

	connStrs := make([]string, 0, len(cfg.RPCs[0].Providers))
	for _, conn := range cfg.RPCs[0].Providers {
		connStrs = append(connStrs, conn.ConnURL)
	}
	srv.rr = balancer.NewRoundRobin(connStrs)
	srv.port = cfg.Port
	srv.done = make(chan struct{})
	srv.noRequestValidation = cfg.NoRequestValidation
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
	url := srv.rr.Next()
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
		log.Info().
			Uint64("id", ctx.ID()).
			Uint64("conn_id", ctx.ConnID()).
			Str("remote_ip", ctx.RemoteIP().String()).
			Msg("request")
		f(ctx)
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
