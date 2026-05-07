package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/app"
)

func main() {
	servicemain.Run("access-manager", app.LoadConfig, app.Run)
}
