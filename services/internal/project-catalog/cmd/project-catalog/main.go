package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/app"
)

func main() {
	servicemain.Run("project-catalog", app.LoadConfig, app.Run)
}
