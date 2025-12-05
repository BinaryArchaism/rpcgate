package proxy

import "github.com/fasthttp/websocket"

type WSContext struct {
	conn *websocket.Conn

	requestID     uint64
	client        string
	providerURL   string
	providerName  string
	loadBalanacer string
	requestPath   string
	chainID       string
	rpcName       string
	method        string
}

type WSHandler func(ctx *WSContext)
