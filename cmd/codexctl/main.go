package main

import (
	"os"

	"github.com/codex-k8s/codexctl/internal/cli"
	"github.com/codex-k8s/codexctl/internal/logging"
)

// main is the entry point for the codexctl CLI binary.
func main() {
	logger := logging.NewLogger(os.Stderr, logging.LevelInfo)
	if err := cli.Execute(os.Args[1:], logger); err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}
