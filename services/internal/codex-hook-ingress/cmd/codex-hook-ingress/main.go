package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/app"
)

func main() {
	servicemain.Run("codex-hook-ingress", app.LoadConfig, app.Run)
}
