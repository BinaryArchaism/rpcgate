package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/valyala/fasthttp"

	"github.com/BinaryArchaism/rpcgate/internal/balancer"
	"github.com/BinaryArchaism/rpcgate/internal/config"
	"github.com/BinaryArchaism/rpcgate/internal/metrics"
)

type Server struct {
	srv                 *fasthttp.Server
	cli                 *fasthttp.Client
	port                string
	rr                  map[string]*balancer.RoundRobin
	noRequestValidation bool
	rpcs                []config.RPC
	clients             config.Clients
	metricsCfg          config.Metrics
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
	srv.clients = cfg.Clients
	srv.metricsCfg = cfg.Metrics

	handler := srv.recoverHandler(
		srv.loggingMiddleware(
			srv.metricsMiddleware(
				srv.authMiddleware(
					srv.routerHandler(
						srv.requestValidationMiddleware(
							srv.handler))))))

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
	provider := srv.rr[string(ctx.RequestURI())].Next()

	log.Debug().Uint64("id", ctx.ID()).Str("name", provider.Name).Msg("request goes to")

	SetProviderToReqCtx(ctx, provider.Name)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	var request JSONRPCRequest
	err := json.Unmarshal(ctx.Request.Body(), &request)
	if err != nil {
		log.Error().Err(err).Msg("can not parse request")
	}
	SetJSONRPCRequestToCtx(ctx, request)

	req.SetRequestURI(provider.ConnURL)
	req.SetBody(ctx.Request.Body())
	req.Header.SetMethod(fasthttp.MethodPost)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	err = srv.cli.Do(req, resp)
	if err != nil {
		log.Error().Err(err).Msg("error while request")
		return
	}

	var response JSONRPCResponse
	err = json.Unmarshal(resp.Body(), &response)
	if err != nil {
		log.Error().Err(err).Msg("can not parse response")
	}
	SetJSONRPCResponceToCtx(ctx, response)

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
	if !srv.metricsCfg.Enabled {
		return func(ctx *fasthttp.RequestCtx) {
			f(ctx)
		}
	}

	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()

		f(ctx)

		reqctx := GetReqCtx(ctx)

		const base = 10
		chainID := strconv.FormatInt(reqctx.ChainID, base)
		metrics.RequestLatencySeconds.WithLabelValues(
			chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request.Method, reqctx.Client).
			Observe(time.Since(start).Seconds())
		metrics.RequestTotalCounter.WithLabelValues(
			chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request.Method, reqctx.Client).Inc()
		if reqctx.Response.HasError() {
			metrics.ClientRequestError.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request.Method, reqctx.Client).Inc()
		}
		if ctx.Response.StatusCode() != http.StatusOK {
			metrics.RequestError.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request.Method, reqctx.Client).Inc()
		}
	}
}

func (srv *Server) routerHandler(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const base = 10
	chainToConnUrls := make(map[string][]config.Provider)
	chains := make(map[string]int64)
	chainIDToName := make(map[int64]string)

	for _, rpc := range srv.rpcs {
		key := "/" + strconv.FormatInt(rpc.ChainID, base)
		chains[key] = rpc.ChainID
		chainIDToName[rpc.ChainID] = rpc.Name
		chainToConnUrls[key] = append(chainToConnUrls[key], rpc.Providers...)
	}
	for chain, urls := range chainToConnUrls {
		srv.rr[chain] = balancer.NewRoundRobin(urls)
	}

	return func(ctx *fasthttp.RequestCtx) {
		chainID, exist := chains[string(ctx.Request.RequestURI())]
		if !exist {
			log.Debug().Msg("unknown path")
			ctx.Error("not found", fasthttp.StatusNotFound)
			return
		}
		SetChainIDToReqCtx(ctx, chainID)
		SetChainNameToReqCtx(ctx, chainIDToName[chainID])
		f(ctx)
	}
}

func (srv *Server) authMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const authHeaderName = "Authorization"
	loginToPass := make(map[string]string)
	for _, c := range srv.clients.Clients {
		loginToPass[c.Login] = c.Password
	}
	return func(ctx *fasthttp.RequestCtx) {
		header := ctx.Request.Header.Peek(authHeaderName)
		login, pass, err := GetBasicAuthDecoded(string(header))

		SetClientToReqCtx(ctx, login)

		if !srv.clients.AuthRequired {
			f(ctx)
			return
		}
		if err != nil {
			log.Error().Err(err).Msg("failed to decode basic auth")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		expectedPass, exist := loginToPass[login]
		if !exist {
			log.Info().
				Uint64("id", ctx.ID()).
				Uint64("conn_id", ctx.ConnID()).
				Err(err).Msg("invalid login")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		if expectedPass != pass {
			log.Info().
				Uint64("id", ctx.ID()).
				Uint64("conn_id", ctx.ConnID()).
				Err(err).Msg("invalid pass")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		f(ctx)
	}
}

func GetBasicAuthDecoded(header string) (string, string, error) {
	const (
		prefix        = "Basic "
		separator     = ":"
		defaultClient = "_unknown_"
	)
	removedPrefix := strings.TrimPrefix(header, prefix)
	decodedLoginPass, err := base64.StdEncoding.DecodeString(removedPrefix)
	if err != nil {
		return defaultClient, "", fmt.Errorf("failed to decode auth header: %w", err)
	}
	log, pass, _ := strings.Cut(string(decodedLoginPass), separator)
	if log == "" {
		log = defaultClient
	}
	return log, pass, nil
}
