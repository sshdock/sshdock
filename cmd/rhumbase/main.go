package main

import (
	"io"
	"os"

	"github.com/iketiunn/rumbase/internal/cli"
	"github.com/iketiunn/rumbase/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	runner := cli.NewRunner(cli.NewMemoryBackend("server"), version.String())
	return runner.Run(args, stdout, stderr)
}
