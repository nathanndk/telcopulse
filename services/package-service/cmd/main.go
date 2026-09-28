package main

import (
	"log/slog"
	"os"
	domainservice "telcopulse/services/package-service"
	"telcopulse/services/shared/runtime"
)

func main() {
	if err := runtime.Run("package-service", domainservice.Handler); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
