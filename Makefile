lint: 
	golangci-lint run --new-from-rev=origin/master

run:
	go run cmd/rpcgate/rpcgate.go --path ./rpcgate.yaml 