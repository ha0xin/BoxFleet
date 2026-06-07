package main

import (
	"fmt"
	"os"

	bfcli "github.com/haoxin/boxfleet/internal/cli/bf"
)

var version = "dev"

func main() {
	bfcli.Version = version
	if err := bfcli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "bf: %v\n", err)
		os.Exit(1)
	}
}
