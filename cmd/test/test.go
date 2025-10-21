package main

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

//nolint:all // no need
func main() {
	cli, err := ethclient.Dial("http://localhost:8080")
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

	fmt.Println(err)
	fmt.Println(len(logs))

	fmt.Println(cli.BlockNumber(context.Background()))
	fmt.Println(cli.ChainID(context.Background()))
}
