package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/app"
)

func main() {
	servicemain.Run("fleet-manager", app.LoadConfig, app.Run)
}
