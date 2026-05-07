package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/app"
)

func main() {
	servicemain.Run("provider-hub", app.LoadConfig, app.Run)
}
