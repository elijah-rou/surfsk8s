package main

import (
	"fmt"
	"os"

	"github.com/elijahrou/surfsk8s/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
