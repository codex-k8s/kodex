package main

import (
	servicemain "github.com/codex-k8s/kodex/libs/go/servicemain"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/app"
)

func main() {
	servicemain.Run("staff-gateway", app.LoadConfig, app.Run)
}
