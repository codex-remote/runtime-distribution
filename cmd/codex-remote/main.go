package main

import (
	"os"

	"github.com/codex-remote/runtime-distribution/internal/runtime"
)

var version = "dev"

func main() {
	os.Exit(runtime.Run(os.Args[1:], version, os.Stdout, os.Stderr))
}
