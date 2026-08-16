package main

import (
	"fmt"
	"os"

	"github.com/dayuer/plinth/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "plinth:", err)
		os.Exit(cli.ExitCode(err))
	}
}
