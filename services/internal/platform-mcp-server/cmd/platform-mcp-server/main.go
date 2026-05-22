package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/platform-mcp-server/internal/app"
)

func main() {
	servicemain.Run("platform-mcp-server", app.LoadConfig, app.Run)
}
