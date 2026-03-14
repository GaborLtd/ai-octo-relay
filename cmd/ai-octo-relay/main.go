package main

import (
	"os"

	"github.com/match/ai-octo-relay/internal/relaycmd"
)

func main() {
	os.Exit(relaycmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
