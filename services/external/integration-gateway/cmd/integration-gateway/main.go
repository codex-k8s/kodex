package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/app"
)

func main() {
	servicemain.Run("integration-gateway", app.LoadConfig, app.Run)
}
