package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
	gatewayruntime "github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/runtime"
)

var version = "dev"

func main() {
	log.SetPrefix("egress-gateway " + version + ": ")
	base := context.Background()
	lifecycle, stop := signal.NotifyContext(base, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	config, err := gatewayruntime.ConfigFromEnv()
	if err != nil {
		log.Print("egress gateway runtime configuration rejected")
		os.Exit(1)
	}
	activePolicy, err := policy.LoadFile(config.PolicyFile, config.ExpectedRevision, config.ExpectedDigest)
	if err != nil {
		log.Print("egress gateway policy rejected")
		if invalidErr := gatewayruntime.RunInvalidPolicy(lifecycle, base, config); invalidErr != nil {
			log.Print("egress gateway invalid-policy runtime stopped with failure")
			os.Exit(1)
		}
		return
	}
	if err := gatewayruntime.Run(lifecycle, base, config, activePolicy); err != nil {
		log.Print("egress gateway runtime stopped with failure")
		os.Exit(1)
	}
}
