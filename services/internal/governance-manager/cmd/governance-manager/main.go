package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/app"
)

func main() {
	servicemain.Run("governance-manager", app.LoadConfig, app.Run)
}
