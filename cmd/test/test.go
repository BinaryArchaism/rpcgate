package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	makeRequests("http://admin:test@localhost:8080/1")
	makeRequests("http://admin:admin@localhost:8080/8453")
}

//nolint:all // no need
func makeRequests(url string) {
	cli, err := ethclient.Dial(url)
	if err != nil {
		panic(err)
	}

	logs, err := cli.FilterLogs(context.Background(), ethereum.FilterQuery{
		FromBlock: big.NewInt(23624500),
		ToBlock:   big.NewInt(23625000),
		Addresses: []common.Address{
			common.HexToAddress("0x5777d92f208679DB4b9778590Fa3CAB3aC9e2168"),
		},
	})

	fmt.Println(len(logs), err)

	fmt.Println(cli.BlockNumber(context.Background()))
	fmt.Println(cli.BlockNumber(context.Background()))
	fmt.Println(cli.ChainID(context.Background()))
}
