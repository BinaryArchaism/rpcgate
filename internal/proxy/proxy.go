package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime/debug"
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
	srv            *fasthttp.Server
	cli            *fasthttp.Client
	port           string
	rpcs           []config.RPC
	clients        config.Clients
	metricsCfg     config.Metrics
	chainToP2CEWMA map[string]*balancer.P2CEWMA
	chainToRR      map[string]*balancer.RoundRobin
	chainToLC      map[string]*balancer.LeastConnection
	done           chan struct{}
}

func New(cfg config.Config) *Server {
	var srv Server

	var cli fasthttp.Client
	srv.cli = &cli
	srv.rpcs = cfg.RPCs
	srv.port = cfg.Port
	srv.done = make(chan struct{})
	srv.chainToP2CEWMA = make(map[string]*balancer.P2CEWMA)
	srv.chainToRR = map[string]*balancer.RoundRobin{}
	srv.chainToLC = map[string]*balancer.LeastConnection{}
	srv.clients = cfg.Clients
	srv.metricsCfg = cfg.Metrics

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

	p2cewmaGlobalInited := cfg.P2CEWMA != config.P2CEWMAConfig{}
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
			localInited := rpc.P2CEWMA != config.P2CEWMAConfig{}
			if localInited {
				srv.chainToP2CEWMA[key] = balancer.NewP2CEWMA(
					providers,
					rpc.P2CEWMA.Smooth,
					rpc.P2CEWMA.LoadNormalizer,
					rpc.P2CEWMA.PenaltyDecay,
					rpc.P2CEWMA.CooldownTimeout,
				)
				continue
			}
			if p2cewmaGlobalInited {
				srv.chainToP2CEWMA[key] = balancer.NewP2CEWMA(
					providers,
					cfg.P2CEWMA.Smooth,
					cfg.P2CEWMA.LoadNormalizer,
					cfg.P2CEWMA.PenaltyDecay,
					cfg.P2CEWMA.CooldownTimeout,
				)
				continue
			}
			srv.chainToP2CEWMA[key] = balancer.NewP2CEWMADefault(providers)
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
				stack := debug.Stack()
				log.Error().
					Uint64("request_id", ctx.ID()).
					Bytes("stack", stack).
					Any("panic", r).
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
		start := time.Now()
		f(ctx)
		latency := time.Since(start).Seconds()

		reqctx := GetReqCtx(ctx)
		chainID := strconv.FormatInt(reqctx.ChainID, base)

		if len(reqctx.Request) == 1 {
			metrics.RequestLatencySeconds.WithLabelValues(
				chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, reqctx.Request[0].Method, reqctx.Client).
				Observe(latency)
			metrics.RequestTotalCounter.WithLabelValues(
				chainID,
				reqctx.RPCName,
				reqctx.Provider,
				reqctx.Balancer,
				reqctx.Request[0].Method,
				reqctx.Client,
			).Inc()
			if reqctx.Response[0].HasError() {
				metrics.ClientRequestError.WithLabelValues(
					chainID,
					reqctx.RPCName,
					reqctx.Provider,
					reqctx.Balancer,
					reqctx.Request[0].Method,
					reqctx.Client,
				).Inc()
			}
			if ctx.Response.StatusCode() != fasthttp.StatusOK {
				metrics.RequestError.WithLabelValues(
					chainID,
					reqctx.RPCName,
					reqctx.Provider,
					reqctx.Balancer,
					reqctx.Request[0].Method,
					reqctx.Client,
				).Inc()
			}
			metrics.ResponseSizeBytes.WithLabelValues(
				chainID,
				reqctx.RPCName,
				reqctx.Provider,
				reqctx.Balancer,
				reqctx.Request[0].Method,
				reqctx.Client,
			).Observe(float64(len(ctx.Response.Body())))
			return
		}

		metrics.RequestLatencySeconds.WithLabelValues(
			chainID, reqctx.RPCName, reqctx.Provider, reqctx.Balancer, "batch", reqctx.Client).
			Observe(latency)
		if ctx.Response.StatusCode() != fasthttp.StatusOK {
			metrics.RequestError.WithLabelValues(
				chainID, reqctx.RPCName, reqctx.Balancer, reqctx.Provider, "batch", reqctx.Client).Inc()
		}
		metrics.ResponseSizeBytes.WithLabelValues(
			chainID,
			reqctx.RPCName,
			reqctx.Provider,
			reqctx.Balancer,
			"batch",
			reqctx.Client,
		).Observe(float64(len(ctx.Response.Body())))
		for i := range len(reqctx.Request) {
			metrics.RequestTotalCounter.WithLabelValues(
				chainID,
				reqctx.RPCName,
				reqctx.Provider,
				reqctx.Balancer,
				reqctx.Request[i].Method,
				reqctx.Client,
			).Inc()
			if reqctx.Response[i].HasError() {
				metrics.ClientRequestError.WithLabelValues(
					chainID,
					reqctx.RPCName,
					reqctx.Provider,
					reqctx.Balancer,
					reqctx.Request[i].Method,
					reqctx.Client,
				).Inc()
			}
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
		SetChainIDToReqCtx(ctx, chainID)
		SetRPCNameToReqCtx(ctx, strings.TrimPrefix(string(ctx.Path()), "/"))

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
			SetClientToReqCtx(ctx, c)
			f(ctx)
		}
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
			} else {
				request = append(request, singleReq)
			}
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
			} else {
				response = append(response, singleResp)
			}
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

func (srv *Server) loadBalancerMiddleware(f fasthttp.RequestHandler) fasthttp.RequestHandler {
	nameToLBAlgo := make(map[string]string)
	for _, rpc := range srv.rpcs {
		nameToLBAlgo["/"+rpc.Name] = rpc.BalancerType
	}

	return func(ctx *fasthttp.RequestCtx) {
		switch nameToLBAlgo[string(ctx.Path())] {
		case "p2cewma":
			SetBalancerToCtx(ctx, "p2cewma")
			srv.proccessP2CEWMA(ctx, f)
		case "round-robin":
			SetBalancerToCtx(ctx, "round-robin")
			srv.proccessRoundRobin(ctx, f)
		case "least-connection":
			SetBalancerToCtx(ctx, "least-connection")
			srv.proccessLeastConnection(ctx, f)
		}
	}
}

func (srv *Server) proccessP2CEWMA(ctx *fasthttp.RequestCtx, next fasthttp.RequestHandler) {
	lb := srv.chainToP2CEWMA[string(ctx.Path())]
	provider, release := lb.Borrow()
	SetProviderToReqCtx(ctx, provider.Name)
	SetConnURLToCtx(ctx, provider.URL)

	start := time.Now()

	next(ctx)

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

	release(ok, time.Since(start))
}

func (srv *Server) proccessLeastConnection(ctx *fasthttp.RequestCtx, next fasthttp.RequestHandler) {
	lb := srv.chainToLC[string(ctx.Path())]
	provider, release := lb.Borrow()
	defer release(true, 0)

	SetProviderToReqCtx(ctx, provider.Name)
	SetConnURLToCtx(ctx, provider.URL)
	next(ctx)
}

func (srv *Server) proccessRoundRobin(ctx *fasthttp.RequestCtx, next fasthttp.RequestHandler) {
	lb := srv.chainToRR[string(ctx.Path())]
	provider, _ := lb.Borrow()
	SetProviderToReqCtx(ctx, provider.Name)
	SetConnURLToCtx(ctx, provider.URL)
	next(ctx)
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
