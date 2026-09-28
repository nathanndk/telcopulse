package main

import (
	"log/slog"
	"os"
	"telcopulse/services/shared/runtime"
	domainservice "telcopulse/services/subscriber-service"
)

func main() {
	if err := runtime.Run("subscriber-service", domainservice.Handler); err != nil {
		slog.Error("service stopped", "error", err)
		os.Exit(1)
	}
}
