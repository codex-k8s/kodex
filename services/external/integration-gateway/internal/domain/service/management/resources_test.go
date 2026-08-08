package management

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	managementrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/management"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

func TestConnectionEligibilityRequiresFreshExactCapacityReadback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	digest := strings.Repeat("a", 64)
	observedAt := now.Add(-time.Minute)
	connection := entity.ManagedProviderConnection{
		ID: "11111111-1111-4111-8111-111111111111", Status: "VALID", Version: 2, Generation: 3,
		ActiveCredential: 3, CredentialBindingID: "22222222-2222-4222-8222-222222222222",
		CredentialBindingVersion: 1, CredentialBindingDigest: digest,
		ControlPlaneID: "33333333-3333-4333-8333-333333333333", ControlPlaneVersion: 1,
		ControlPlaneDigest: digest, ObservedAt: &observedAt,
		Capacity: entity.ProviderCapacityObservation{
			Usage: 25, Limit: 100, Revision: 7, ObservedAt: observedAt,
			WindowSeconds: 300 * 60, ResetsAt: now.Add(time.Hour), ExpiresAt: now.Add(time.Minute), Digest: digest,
		},
	}
	if !connectionEligible(connection, now) {
		t.Fatal("fresh version-pinned provider observation was rejected")
	}

	for name, mutate := range map[string]func(*entity.ManagedProviderConnection){
		"missing limit": func(value *entity.ManagedProviderConnection) { value.Capacity.Limit = 0 },
		"expired":       func(value *entity.ManagedProviderConnection) { value.Capacity.ExpiresAt = now },
		"stale": func(value *entity.ManagedProviderConnection) {
			stale := now.Add(-6 * time.Minute)
			value.ObservedAt = &stale
		},
		"generation": func(value *entity.ManagedProviderConnection) { value.ActiveCredential++ },
		"binding":    func(value *entity.ManagedProviderConnection) { value.CredentialBindingDigest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := connection
			mutate(&changed)
			if connectionEligible(changed, now) {
				t.Fatal("unknown or stale capacity remained eligible")
			}
		})
	}
}

type receiptFirstTestRepository struct {
	managementrepo.Repository
	current entity.ManagedProviderPool
	stored  entity.ManagedProviderPool
	order   []string
}

func (repository *receiptFirstTestRepository) GetPool(context.Context, domainrepo.Scope, string) (entity.ManagedProviderPool, error) {
	repository.order = append(repository.order, "resolve")
	return repository.current, nil
}

func (repository *receiptFirstTestRepository) ReplayManagement(context.Context, domainrepo.Scope, string, string, string) ([]byte, bool, error) {
	repository.order = append(repository.order, "receipt")
	payload, _ := json.Marshal(repository.stored)
	return payload, true, nil
}

func TestPoolReplayPrecedesOCCAfterOwnerResolution(t *testing.T) {
	t.Parallel()

	scope := domainrepo.Scope{
		TenantID: "11111111-1111-4111-8111-111111111111", ProjectID: "22222222-2222-4222-8222-222222222222",
		ActorID: "33333333-3333-4333-8333-333333333333",
	}
	current := entity.ManagedProviderPool{
		ID: "44444444-4444-4444-8444-444444444444", StableKey: "primary", DisplayName: "Primary",
		Policy: "LEAST_USED", Version: 2, Status: "ARCHIVED",
	}
	repository := &receiptFirstTestRepository{current: current, stored: current}
	service := &Service{repository: repository, now: time.Now, config: Config{MaximumPageSize: 100}}
	result, err := service.ManagePool(context.Background(), ManagePoolInput{
		Scope: scope, Action: "ARCHIVE", ID: current.ID, ExpectedVersion: 1,
		IdempotencyKey: "55555555-5555-4555-8555-555555555555",
	})
	if err != nil || result.Version != current.Version {
		t.Fatalf("exact replay did not return immutable result after own version bump: %#v %v", result, err)
	}
	if len(repository.order) != 2 || repository.order[0] != "resolve" || repository.order[1] != "receipt" {
		t.Fatalf("owner resolution/receipt order is invalid: %#v", repository.order)
	}
}

var _ managementrepo.Repository = (*receiptFirstTestRepository)(nil)
