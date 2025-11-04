package proxy

import "github.com/valyala/fasthttp"

// userValueKey is the key used to store ReqCtx inside fasthttp.RequestCtx.
const userValueKey = "rpcgate.reqctx"

// ReqCtx carries request-scoped metadata used for metrics and logging.
// It is progressively filled by middlewares during request handling.
type ReqCtx struct {
	Request  JSONRPCRequest  // json-rpc request from client
	Response JSONRPCResponse // json-rpc response from node

	Client    string // login from basic auth
	ChainID   int64  // chainID from path
	ChainName string // chain name from config
	Provider  string // provider from config
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

// SetProviderToReqCtx sets the provider field in the given fasthttp.RequestCtx.
func SetProviderToReqCtx(ctx *fasthttp.RequestCtx, provider string) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.Provider = provider
	})
}

// SetChainIDToReqCtx sets the chain ID field in the given fasthttp.RequestCtx.
func SetChainIDToReqCtx(ctx *fasthttp.RequestCtx, chainID int64) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.ChainID = chainID
	})
}

// SetChainNameToReqCtx sets the chain name field in the given fasthttp.RequestCtx.
func SetChainNameToReqCtx(ctx *fasthttp.RequestCtx, chainName string) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.ChainName = chainName
	})
}

// SetClientToReqCtx sets the client field in the given fasthttp.RequestCtx.
func SetClientToReqCtx(ctx *fasthttp.RequestCtx, client string) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.Client = client
	})
}

// SetJSONRPCResponceToCtx sets the json-rpc response from node to
// Response field in the given fasthttp.RequestCtx.
func SetJSONRPCResponceToCtx(ctx *fasthttp.RequestCtx, response JSONRPCResponse) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.Response = response
	})
}

// SetJSONRPCRequestToCtx sets the json-rpc request from client to
// Request field in the given fasthttp.RequestCtx.
func SetJSONRPCRequestToCtx(ctx *fasthttp.RequestCtx, request JSONRPCRequest) {
	SetToReqCtx(ctx, func(rc *ReqCtx) {
		rc.Request = request
	})
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
	return j.Error != JSONRPCError{
		Code:    0,
		Message: "",
	}
}
