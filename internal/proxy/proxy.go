package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	srv        *fasthttp.Server
	cli        *fasthttp.Client
	port       string
	rr         map[string]*balancer.RoundRobin
	rpcs       []config.RPC
	clients    config.Clients
	metricsCfg config.Metrics
	done       chan struct{}
}

func New(cfg config.Config) *Server {
	var srv Server

	var cli fasthttp.Client
	srv.cli = &cli
	srv.rpcs = cfg.RPCs
	srv.port = cfg.Port
	srv.done = make(chan struct{})
	srv.rr = make(map[string]*balancer.RoundRobin)
	srv.clients = cfg.Clients
	srv.metricsCfg = cfg.Metrics

	handler := srv.recoverHandler(
		srv.healthzProbeMiddleware(
			srv.loggingMiddleware(
				srv.metricsMiddleware(
					srv.authMiddleware(
						srv.routerHandler(
							srv.requestResponseParserMiddleware(
								srv.handler)))))))

	srv.srv = &fasthttp.Server{
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

	log.Debug().Uint64("request_id", ctx.ID()).Str("name", provider.Name).Msg("request goes to")

	SetProviderToReqCtx(ctx, provider.Name)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(provider.ConnURL)
	req.SetBody(ctx.Request.Body())
	req.Header.SetMethod(fasthttp.MethodPost)
	req.Header.SetContentType("application/json")

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	err := srv.cli.Do(req, resp)
	if err != nil {
		log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("error while request")
		return
	}

	_, err = io.Copy(ctx, bytes.NewReader(resp.Body()))
	if err != nil {
		log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("error while request")
		return
	}
	ctx.Response.SetStatusCode(resp.StatusCode())
	resp.Header.CopyTo(&ctx.Response.Header)
}

func (srv *Server) recoverHandler(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		defer func() {
			if r := recover(); r != nil {
				log.Error().Uint64("request_id", ctx.ID()).Any("panic", r).Stack().Msg("panic at handler")
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
			Uint64("request_id", ctx.ID()).
			Uint64("conn_id", ctx.ConnID()).
			Str("remote_ip", ctx.RemoteIP().String()).
			Int("status", ctx.Response.StatusCode()).
			Str("latency", time.Since(start).String()).
			Str("path", string(ctx.Request.RequestURI())).
			Msg("request complete")
	}
}

func (srv *Server) metricsMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const base = 10

	if !srv.metricsCfg.Enabled {
		return func(ctx *fasthttp.RequestCtx) {
			f(ctx)
		}
	}

	return func(ctx *fasthttp.RequestCtx) {
		start := time.Now()
		f(ctx)
		latency := time.Since(start).Seconds()

		reqctx := GetReqCtx(ctx)
		chainID := strconv.FormatInt(reqctx.ChainID, base)

		if len(reqctx.Request) == 1 {
			metrics.RequestLatencySeconds.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[0].Method, reqctx.Client).
				Observe(latency)
			metrics.RequestTotalCounter.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[0].Method, reqctx.Client).Inc()
			if reqctx.Response[0].HasError() {
				metrics.ClientRequestError.WithLabelValues(
					chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[0].Method, reqctx.Client).Inc()
			}
			if ctx.Response.StatusCode() != fasthttp.StatusOK {
				metrics.RequestError.WithLabelValues(
					chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[0].Method, reqctx.Client).Inc()
			}
			return
		}

		metrics.RequestLatencySeconds.WithLabelValues(
			chainID, reqctx.ChainName, reqctx.Provider, "batch", reqctx.Client).
			Observe(latency)
		if ctx.Response.StatusCode() != fasthttp.StatusOK {
			metrics.RequestError.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, "batch", reqctx.Client).Inc()
		}
		for i := range len(reqctx.Request) {
			metrics.RequestTotalCounter.WithLabelValues(
				chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[i].Method, reqctx.Client).Inc()
			if reqctx.Response[i].HasError() {
				metrics.ClientRequestError.WithLabelValues(
					chainID, reqctx.ChainName, reqctx.Provider, reqctx.Request[i].Method, reqctx.Client).Inc()
			}
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
			log.Debug().Uint64("request_id", ctx.ID()).Msg("unknown path")
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
			log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("failed to decode basic auth")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		expectedPass, exist := loginToPass[login]
		if !exist {
			log.Info().
				Uint64("request_id", ctx.ID()).
				Uint64("conn_id", ctx.ConnID()).
				Err(err).Msg("invalid login")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		if expectedPass != pass {
			log.Info().
				Uint64("request_id", ctx.ID()).
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

func (srv *Server) healthzProbeMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const healthzProbePath = "/healthz"

	return func(ctx *fasthttp.RequestCtx) {
		if string(ctx.Request.RequestURI()) != healthzProbePath {
			f(ctx)
			return
		}

		ctx.Response.SetStatusCode(fasthttp.StatusOK)
		ctx.Response.SetBodyString("ok")
	}
}

func (srv *Server) requestResponseParserMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		isBatched, err := isBodyArray(ctx.Request.Body())
		if err != nil {
			log.Info().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse body")
		}

		var request []JSONRPCRequest
		if isBatched {
			err = json.Unmarshal(ctx.Request.Body(), &request)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse request")
			}
		} else {
			var singleReq JSONRPCRequest
			err = json.Unmarshal(ctx.Request.Body(), &singleReq)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse request")
			}
			request = append(request, singleReq)
		}
		SetJSONRPCRequestToCtx(ctx, request)

		f(ctx)

		var response []JSONRPCResponse
		if isBatched {
			err = json.Unmarshal(ctx.Response.Body(), &response)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse response")
			}
		} else {
			var singleResp JSONRPCResponse
			err = json.Unmarshal(ctx.Response.Body(), &singleResp)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse response")
			}
			response = append(response, singleResp)
		}
		SetJSONRPCResponseToCtx(ctx, response)
	}
}

func isBodyArray(body []byte) (bool, error) {
	body = bytes.TrimSpace(body)
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	if len(body) == 0 {
		return false, errors.New("body is empty after space trim")
	}
	if body[0] == '[' {
		return true, nil
	}
	if body[0] == '{' {
		return false, nil
	}

	return false, errors.New("body is invalid")
}
