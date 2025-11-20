package proxy_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/BinaryArchaism/rpcgate/internal/proxy"
)

func Test_ReqCtx(t *testing.T) {
	t.Run("SetToCtx", func(t *testing.T) {
		req := &fasthttp.RequestCtx{}
		reqctx := &proxy.ReqCtx{
			ChainID: 123,
		}
		reqctx.SetToCtx(req)
		gotReqCtx, ok := req.UserValue("rpcgate.reqctx").(*proxy.ReqCtx)
		require.True(t, ok)
		require.NotNil(t, gotReqCtx)
		require.Equal(t, *reqctx, *gotReqCtx)
	})
	t.Run("GetReqCtx_Exist", func(t *testing.T) {
		req := &fasthttp.RequestCtx{}
		reqctx := &proxy.ReqCtx{
			ChainID: 123,
		}
		req.SetUserValue("rpcgate.reqctx", reqctx)
		gotReqCtx := proxy.GetReqCtx(req)
		require.Equal(t, *reqctx, *gotReqCtx)
	})
	t.Run("GetReqCtx_Not_Exist", func(t *testing.T) {
		req := &fasthttp.RequestCtx{}
		gotReqCtx := proxy.GetReqCtx(req)
		require.Empty(t, *gotReqCtx)
	})
	t.Run("HasError", func(t *testing.T) {
		var resp proxy.JSONRPCResponse
		require.False(t, resp.HasError())

		resp.Error = proxy.JSONRPCError{
			Code:    123,
			Message: "error",
		}
		require.True(t, resp.HasError())
	})
	t.Run("setter", func(t *testing.T) {
		req := &fasthttp.RequestCtx{}
		proxy.SetToReqCtx(req, func(rc *proxy.ReqCtx) {
			rc.Balancer = "test"
		})
		gotReqCtx := proxy.GetReqCtx(req)
		require.NotEmpty(t, *gotReqCtx)
		require.Equal(t, "test", gotReqCtx.Balancer)
	})
}
