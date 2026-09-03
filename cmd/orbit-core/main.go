// Command orbit-core runs the Orbit Core service.
package main

import (
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.Info("component scaffold ready", "component", "orbit-core")
}
