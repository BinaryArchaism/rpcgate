package proxy

import "github.com/valyala/fasthttp"

// userValueKey is the key used to store ReqCtx inside fasthttp.RequestCtx.
const userValueKey = "rpcgate.reqctx"

// ReqCtx carries request-scoped metadata used for metrics and logging.
// It is progressively filled by middlewares during request handling.
type ReqCtx struct {
	Request  []JSONRPCRequest  // json-rpc request from client
	Response []JSONRPCResponse // json-rpc response from node

	ConnURL string // provider connection url choiced by balanacer

	Balancer string // load balancing algorithm for request
	Client   string // login from basic auth
	ChainID  int64  // chainID from path
	RPCName  string // rpc name from config
	Provider string // provider from config

	Latency       float64 // request latency
	IsClientError bool    // true if response contains user user
}

// SetToCtx stores the ReqCtx in the given fasthttp.RequestCtx.
func (r *ReqCtx) SetToCtx(ctx *fasthttp.RequestCtx) {
	ctx.SetUserValue(userValueKey, r)
}

// SetToReqCtx retrieves the ReqCtx from fasthttp.RequestCtx,
// applies the provided setter, and stores it back.
func SetToReqCtx(ctx *fasthttp.RequestCtx, setter func(*ReqCtx)) {
	reqctx := GetReqCtx(ctx)
	defer reqctx.SetToCtx(ctx)

	setter(reqctx)
}

// GetReqCtx returns the ReqCtx from fasthttp.RequestCtx.
// If none exists, a new one is created.
func GetReqCtx(ctx *fasthttp.RequestCtx) *ReqCtx {
	reqctx, ok := ctx.UserValue(userValueKey).(*ReqCtx)
	if !ok {
		return &ReqCtx{}
	}

	return reqctx
}

// JSONRPCRequest json-rpc request spec struct with method field.
type JSONRPCRequest struct {
	Method string `json:"method"`
}

// JSONRPCResponse json-rpc response spec struct with error field.
type JSONRPCResponse struct {
	Error JSONRPCError `json:"error"`
}

// JSONRPCError json-rpc error spec struct.
type JSONRPCError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

// HasError return false if JSONRPCResponse Error field is empty.
func (j *JSONRPCResponse) HasError() bool {
	return j.Error != JSONRPCError{}
}
