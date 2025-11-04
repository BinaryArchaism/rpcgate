lint: 
	golangci-lint run

run:
	go run cmd/rpcgate/rpcgate.go --config ./rpcgate.yaml 