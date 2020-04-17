// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"os"

	"github.com/ava-labs/coreth/cmd/geth"
)

func checkError(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	geth.App.Run(os.Args)
}
