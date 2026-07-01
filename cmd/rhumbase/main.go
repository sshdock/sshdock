package main

import (
	"fmt"
	"io"
	"os"

	"github.com/iketiunn/rumbase/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintf(stdout, "rhumbase %s\n", version.String())
		return 0
	}

	fmt.Fprintln(stderr, "usage: rhumbase version")
	return 2
}
