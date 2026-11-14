package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

// errInteractionOwnerReady hides transport and credential details when the exact protected owner read cannot complete.
// errInteractionOwnerReady скрывает transport и credential details, когда точное защищённое owner read не завершается.
var errInteractionOwnerReady = errors.New("protected interaction owner path is not ready")

// checkInteractionOwner verifies the same local authority and protected owner transport used by workers without claiming work or performing a provider effect.
// checkInteractionOwner проверяет те же local authority и protected owner transport, что workers, без аренды работы либо provider effect.
func checkInteractionOwner(ctx context.Context, localAuthority func(context.Context) error, control cp.InteractionWorkServiceClient) error {
	if localAuthority == nil || control == nil {
		return errInteractionOwnerReady
	}
	if err := localAuthority(ctx); err != nil {
		return errInteractionOwnerReady
	}
	response, err := control.ListInteractionSources(ctx, &cp.ListInteractionSourcesRequest{})
	if err != nil || response == nil {
		return errInteractionOwnerReady
	}
	return nil
}

// runInteractionReadiness continuously rechecks the protected owner path under bounded contexts and never treats an absent optional provider as a successful provider probe.
// runInteractionReadiness непрерывно перепроверяет protected owner path под bounded contexts и не считает отсутствие optional provider успешной provider probe.
func runInteractionReadiness(control *controlplaneclient.Client, readiness *serviceruntime.Readiness, metrics *observability.Metrics, config Config) serviceruntime.Worker {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(config.ReadinessInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-ticker.C:
				check, cancel := context.WithTimeout(ctx, config.RequestTimeout)
				err := checkInteractionOwner(check, control.CheckLocalAuthority, control.Interaction)
				cancel()
				state := "ready"
				if err != nil {
					state = "owner_unavailable"
				}
				readiness.Set(err == nil, state)
				metrics.SetReady(err == nil)
			}
		}
	}
}
