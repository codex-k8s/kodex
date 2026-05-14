package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/app"
)

func main() {
	servicemain.Run("agent-manager", app.LoadConfig, app.Run)
}
