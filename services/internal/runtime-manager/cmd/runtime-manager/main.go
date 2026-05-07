package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/app"
)

func main() {
	servicemain.Run("runtime-manager", app.LoadConfig, app.Run)
}
