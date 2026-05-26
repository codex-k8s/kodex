package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/app"
)

func main() {
	servicemain.Run("interaction-hub", app.LoadConfig, app.Run)
}
