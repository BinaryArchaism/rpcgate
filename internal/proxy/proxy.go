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

type Balancer interface {
	Borrow() (balancer.Payload, balancer.Release)
}

type Server struct {
	srv            *fasthttp.Server
	cli            *fasthttp.Client
	port           int64
	rpcs           []config.RPC
	clients        config.Clients
	metricsCfg     config.Metrics
	chainToP2CEWMA map[string]*balancer.P2CEWMA
	chainToRR      map[string]*balancer.RoundRobin
	chainToLC      map[string]*balancer.LeastConnection
	done           chan struct{}
}

func New(cfg config.Config) *Server {
	srv := Server{
		cli:            &fasthttp.Client{},
		rpcs:           cfg.RPCs,
		port:           cfg.Port,
		done:           make(chan struct{}),
		chainToP2CEWMA: make(map[string]*balancer.P2CEWMA),
		chainToRR:      make(map[string]*balancer.RoundRobin),
		chainToLC:      make(map[string]*balancer.LeastConnection),
		clients:        cfg.Clients,
		metricsCfg:     cfg.Metrics,
	}

	handler := srv.recoverHandler(
		srv.healthzProbeMiddleware(
			srv.loggingMiddleware(
				srv.metricsMiddleware(
					srv.authMiddleware(
						srv.routerHandler(
							srv.loadBalancerMiddleware(
								srv.requestResponseParserMiddleware(
									srv.handler)),
						))))))

	for _, rpc := range cfg.RPCs {
		providers := make([]balancer.Payload, 0, len(rpc.Providers))
		for _, provider := range rpc.Providers {
			providers = append(providers, balancer.Payload{
				URL:  provider.ConnURL,
				Name: provider.Name,
			})
		}
		key := "/" + rpc.Name
		switch rpc.BalancerType {
		case "p2cewma":
			srv.chainToP2CEWMA[key] = balancer.NewP2CEWMA(
				providers,
				rpc.P2CEWMA.Smooth,
				rpc.P2CEWMA.LoadNormalizer,
				rpc.P2CEWMA.PenaltyDecay,
				rpc.P2CEWMA.CooldownTimeout,
			)
		case "round-robin":
			srv.chainToRR[key] = balancer.NewRoundRobin(providers)
		case "least-connection":
			srv.chainToLC[key] = balancer.NewLeastConnection(providers)
		}
	}

	srv.srv = &fasthttp.Server{
		Handler: handler,
	}

	return &srv
}

func (srv *Server) Start(ctx context.Context) {
	go func() {
		err := srv.srv.ListenAndServe(fmt.Sprintf(":%d", srv.port))
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
	reqctx := GetReqCtx(ctx)

	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(reqctx.ConnURL)
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
				log.Error().
					// TODO this doesnt print stack
					Stack().
					Err(errors.New("panic")).
					Uint64("request_id", ctx.ID()).
					Any("recover", r).
					Msg("panic at handler")
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

		reqctx := GetReqCtx(ctx)
		log.Info().
			Uint64("request_id", ctx.ID()).
			Uint64("conn_id", ctx.ConnID()).
			Str("remote_ip", ctx.RemoteIP().String()).
			Int("status", ctx.Response.StatusCode()).
			Str("latency", time.Since(start).String()).
			Str("path", string(ctx.Path())).
			Str("client", reqctx.Client).
			Str("provider", reqctx.Provider).
			Msg("request completed")
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
		f(ctx)

		reqctx := GetReqCtx(ctx)
		chainID := strconv.FormatInt(reqctx.ChainID, base)

		observeLatency := func(method string) {
			metrics.RequestLatencySeconds.WithLabelValues(
				chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, method, reqctx.Client).
				Observe(reqctx.Latency)
		}
		observeTotal := func(method string) {
			metrics.RequestTotalCounter.WithLabelValues(
				chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, method, reqctx.Client,
			).Inc()
		}
		observeClientError := func(hasErr bool, method string) {
			if hasErr {
				metrics.ClientRequestError.WithLabelValues(
					chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, method, reqctx.Client,
				).Inc()
			}
		}
		observeRequestError := func(method string) {
			if ctx.Response.StatusCode() != fasthttp.StatusOK {
				metrics.RequestError.WithLabelValues(
					chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, method, reqctx.Client,
				).Inc()
			}
		}
		observeResponseSizeBytes := func(method string) {
			metrics.ResponseSizeBytes.WithLabelValues(
				chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, method, reqctx.Client,
			).Observe(float64(len(ctx.Response.Body())))
		}

		if len(reqctx.Request) == 1 && len(reqctx.Response) == 1 {
			observeLatency(reqctx.Request[0].Method)
			observeTotal(reqctx.Request[0].Method)
			observeClientError(reqctx.Response[0].HasError(), reqctx.Request[0].Method)
			observeRequestError(reqctx.Request[0].Method)
			observeResponseSizeBytes(reqctx.Request[0].Method)
			return
		}

		observeLatency("batch")
		observeRequestError("batch")
		observeResponseSizeBytes("batch")
		if len(reqctx.Request) != len(reqctx.Response) {
			log.Debug().
				Int("len(reqctx.Request)", len(reqctx.Request)).
				Int("len(reqctx.Response)", len(reqctx.Response)).
				Msg("count mismatched")
			return
		}
		for i := range len(reqctx.Request) {
			observeTotal(reqctx.Request[i].Method)
			observeClientError(reqctx.Response[i].HasError(), reqctx.Request[i].Method)
		}
	}
}

func (srv *Server) routerHandler(
	httpFunc fasthttp.RequestHandler,
) fasthttp.RequestHandler {
	nameToChainID := make(map[string]int64)
	for _, rpc := range srv.rpcs {
		nameToChainID["/"+rpc.Name] = rpc.ChainID
	}

	return func(ctx *fasthttp.RequestCtx) {
		chainID, exist := nameToChainID[string(ctx.Path())]
		if !exist {
			log.Debug().Uint64("request_id", ctx.ID()).Msg("unknown path")
			ctx.Error("not found", fasthttp.StatusNotFound)
			return
		}
		SetToReqCtx(ctx, func(rc *ReqCtx) {
			rc.ChainID = chainID
			rc.RPCName = strings.TrimPrefix(string(ctx.Path()), "/")
		})

		httpFunc(ctx)
	}
}

func (srv *Server) authMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const authHeaderName = "Authorization"
	loginToPass := make(map[string]string)
	for _, c := range srv.clients.Clients {
		loginToPass[c.Login] = c.Password
	}

	if srv.clients.Type == "query" {
		return func(ctx *fasthttp.RequestCtx) {
			c := string(ctx.QueryArgs().Peek("client"))
			if c == "" {
				c = "_unknown_"
			}
			SetToReqCtx(ctx, func(rc *ReqCtx) { rc.Client = c })
			f(ctx)
		}
	}

	return func(ctx *fasthttp.RequestCtx) {
		header := ctx.Request.Header.Peek(authHeaderName)
		login, pass, err := GetBasicAuthDecoded(string(header))

		SetToReqCtx(ctx, func(rc *ReqCtx) { rc.Client = login })

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
				Err(err).Msg("invalid login")
			ctx.Error("", fasthttp.StatusUnauthorized)
			return
		}
		if expectedPass != pass {
			log.Info().
				Uint64("request_id", ctx.ID()).
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
	login, pass, _ := strings.Cut(string(decodedLoginPass), separator)
	if login == "" {
		login = defaultClient
	}
	return login, pass, nil
}

func (srv *Server) healthzProbeMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	const healthzProbePath = "/healthz"

	return func(ctx *fasthttp.RequestCtx) {
		if string(ctx.Path()) != healthzProbePath {
			f(ctx)
			return
		}

		ctx.Response.SetStatusCode(fasthttp.StatusOK)
		ctx.Response.SetBodyString("ok")
	}
}

func (srv *Server) requestResponseParserMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	return func(ctx *fasthttp.RequestCtx) {
		isBatched := isBatch(ctx.Request.Body())

		var request []JSONRPCRequest
		if isBatched {
			err := json.Unmarshal(ctx.Request.Body(), &request)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse request")
			}
		} else {
			request = append(request, JSONRPCRequest{})
			err := json.Unmarshal(ctx.Request.Body(), &request[0])
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse request")
			}
		}
		SetToReqCtx(ctx, func(rc *ReqCtx) { rc.Request = request })

		f(ctx)

		var response []JSONRPCResponse
		if isBatched {
			err := json.Unmarshal(ctx.Response.Body(), &response)
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse response")
			}
		} else {
			response = append(response, JSONRPCResponse{})
			err := json.Unmarshal(ctx.Response.Body(), &response[0])
			if err != nil {
				log.Error().Uint64("request_id", ctx.ID()).Err(err).Msg("can not parse response")
			}
		}
		SetToReqCtx(ctx, func(rc *ReqCtx) { rc.Response = response })
	}
}

func isBatch(raw json.RawMessage) bool {
	for _, c := range raw {
		// skip insignificant whitespace (http://www.ietf.org/rfc/rfc4627.txt)
		if c == 0x20 || c == 0x09 || c == 0x0a || c == 0x0d {
			continue
		}
		return c == '['
	}
	return false
}

func (srv *Server) loadBalancerMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	nameToLBAlgo := make(map[string]string)
	for _, rpc := range srv.rpcs {
		nameToLBAlgo["/"+rpc.Name] = rpc.BalancerType
	}

	return func(ctx *fasthttp.RequestCtx) {
		balancerType := nameToLBAlgo[string(ctx.Path())]

		var lb Balancer
		switch balancerType {
		case "p2cewma":
			lb = srv.chainToP2CEWMA[string(ctx.Path())]
		case "round-robin":
			lb = srv.chainToRR[string(ctx.Path())]
		case "least-connection":
			lb = srv.chainToLC[string(ctx.Path())]
		}
		if lb == nil {
			log.Error().
				Uint64("request_id", ctx.ID()).
				Str("path", string(ctx.Path())).
				Str("balancer", balancerType).
				Msg("no balancer configured for rpc")
			ctx.Error("internal server error", fasthttp.StatusInternalServerError)
			return
		}

		provider, release := lb.Borrow()

		SetToReqCtx(ctx, func(rc *ReqCtx) {
			rc.Balancer = balancerType
			rc.Provider = provider.Name
			rc.ConnURL = provider.URL
		})

		start := time.Now()
		f(ctx)
		latency := time.Since(start)

		ok := ctx.Response.StatusCode() == fasthttp.StatusOK
		reqctx := GetReqCtx(ctx)

		if len(reqctx.Response) == 0 {
			ok = false
		}
		for _, resp := range reqctx.Response {
			if !resp.HasError() {
				continue
			}
			if !isUserCallError(resp.Error.Code, resp.Error.Message) {
				ok = false
				break
			}
		}

		SetToReqCtx(ctx, func(rc *ReqCtx) { rc.Latency = latency.Seconds() })

		release(ok, latency)
	}
}

func isUserCallError(code int64, msg string) bool {
	switch code {
	case -32003, -32004, -32006, -32010, -32600, -32700:
		return true
	case -32601:
		// TODO required methods validation
		return true
	case -32602:
		m := strings.ToLower(msg)
		if strings.Contains(m, "block range limit exceeded") {
			return false
		}
		return true
	case -32000:
		m := strings.ToLower(msg)
		if strings.Contains(m, "execution reverted") ||
			strings.Contains(m, "replacement transaction underpriced") {
			return true
		}
	}
	return false
}
