package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/app"
)

func main() {
	servicemain.Run("package-hub", app.LoadConfig, app.Run)
}
