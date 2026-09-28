package main

import (
	"log/slog"
	"os"
	deployment "telcopulse/services/deployment-service"
	"telcopulse/services/shared/runtime"
)

func main() {
	if os.Getenv("APP_MODE") != "local" {
		slog.Error("APP_MODE=local required until authenticated deployment intake is implemented")
		os.Exit(1)
	}
	if err := runtime.Run("deployment-service", deployment.Handler); err != nil {
		slog.Error("deployment service stopped", "error", err)
		os.Exit(1)
	}
}
