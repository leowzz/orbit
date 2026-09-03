// Command orbit-agent runs the trusted-host Orbit Agent.
package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("component scaffold ready", "component", "orbit-agent")
}
