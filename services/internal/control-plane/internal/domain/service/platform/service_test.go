package platform

import (
	"context"
	"errors"
	"testing"

	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

type readinessRepository struct {
	platformrepo.Repository
	err error
}

func (repository *readinessRepository) Ready(context.Context) error {
	return repository.err
}

type unavailableProviderMaterializer struct {
	checkCalls int
}

func (materializer *unavailableProviderMaterializer) Check(context.Context) error {
	materializer.checkCalls++
	return errors.New("provider materializer is unavailable")
}

func (*unavailableProviderMaterializer) StartDeviceAuthorization(context.Context, string, string) (ProviderDeviceAuthorizationMaterialization, error) {
	return ProviderDeviceAuthorizationMaterialization{}, nil
}

func (*unavailableProviderMaterializer) ObserveDeviceAuthorization(context.Context, string) (ProviderAuthorizationObservation, error) {
	return ProviderAuthorizationObservation{}, nil
}

func (*unavailableProviderMaterializer) MaterializeAPIKey(context.Context, string, string, []byte) (entity.ProviderCredentialDescriptor, string, error) {
	return entity.ProviderCredentialDescriptor{}, "", nil
}

func (*unavailableProviderMaterializer) Discard(context.Context, ProviderMaterializationDiscard) error {
	return nil
}

func TestReadyDoesNotDependOnProviderCredentialMaterializer(t *testing.T) {
	t.Parallel()

	materializer := &unavailableProviderMaterializer{}
	service, err := New(
		&readinessRepository{},
		WithProviderCredentialMaterializer(materializer),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(context.Background()); err != nil {
		t.Fatalf("Ready() error = %v", err)
	}
	if materializer.checkCalls != 0 {
		t.Fatalf("provider materializer readiness calls = %d, want 0", materializer.checkCalls)
	}
}

func TestReadyReturnsOwnedRepositoryFailure(t *testing.T) {
	t.Parallel()

	want := errors.New("platform repository is unavailable")
	service, err := New(&readinessRepository{err: want})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Ready(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Ready() error = %v, want %v", err, want)
	}
}
