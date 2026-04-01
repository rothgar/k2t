package main

import (
	"os"

	"github.com/rothgar/k2t/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
