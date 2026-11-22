package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesOutputFixture records owned bytes without inventing durable staging or artifact publication.
// kubernetesOutputFixture сохраняет owned bytes без выдуманного durable staging либо artifact publication.
type kubernetesOutputFixture struct {
	*managedKubernetesRepositoryFixture
	calls          int
	borrowed, seen []byte
}

// AcceptManagedKubernetesOutputFrame retains only a test alias to prove service snapshot and zeroing.
// AcceptManagedKubernetesOutputFrame хранит лишь test alias для проверки service snapshot и zeroing.
func (fixture *kubernetesOutputFixture) AcceptManagedKubernetesOutputFrame(_ context.Context, _ value.Principal, _ value.ManagedKubernetesBinding, input command.ManagedKubernetesAcceptOutputFrame) (entity.ManagedKubernetesOutputFrameReceipt, error) {
	fixture.calls++
	fixture.borrowed = input.Payload
	fixture.seen = bytes.Clone(input.Payload)
	return entity.ManagedKubernetesOutputFrameReceipt{}, nil
}

// GetManagedKubernetesOutputFrame records metadata-only forwarding without issuing a receipt or credit.
// GetManagedKubernetesOutputFrame фиксирует metadata-only forwarding без receipt либо credit.
func (fixture *kubernetesOutputFixture) GetManagedKubernetesOutputFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, command.ManagedKubernetesOutputFrameLookup) (entity.ManagedKubernetesOutputFrameReceipt, error) {
	fixture.calls++
	return entity.ManagedKubernetesOutputFrameReceipt{}, nil
}

// TestKubernetesOutputPortNeverUsesGenericArtifactAuthority checks fixed caller/method and private payload ownership.
// TestKubernetesOutputPortNeverUsesGenericArtifactAuthority проверяет fixed caller/method и private payload ownership.
func TestKubernetesOutputPortNeverUsesGenericArtifactAuthority(t *testing.T) {
	service, base, p, binding := managedKubernetesServiceFixture(t, "unused")
	p.CallerWorkload = "integration-gateway"
	p.Permission = "platform.runtime.kubernetes.output.accept"
	payload := []byte("synthetic output")
	sum := sha256.Sum256(payload)
	input := command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: "kst_original", Sequence: 1, DeliveryOrdinal: 1}, Channel: "STDOUT", ByteCount: uint32(len(payload)), PayloadDigest: hex.EncodeToString(sum[:]), Payload: payload}
	if _, err := service.AcceptManagedKubernetesOutputFrame(t.Context(), p, binding, input); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("absent private staging became generic artifact upload", err)
	}
	fixture := &kubernetesOutputFixture{managedKubernetesRepositoryFixture: base}
	service, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.AcceptManagedKubernetesOutputFrame(t.Context(), p, binding, input); err != nil || fixture.calls != 1 || !bytes.Equal(fixture.seen, payload) || !bytes.Equal(payload, []byte("synthetic output")) {
		t.Fatal("private payload ownership changed", err)
	}
	for _, value := range fixture.borrowed {
		if value != 0 {
			t.Fatal("owned output bytes retained after service return")
		}
	}
	changed := input
	changed.DeliveryOrdinal = 0
	if _, err = service.AcceptManagedKubernetesOutputFrame(t.Context(), p, binding, changed); !errors.Is(err, errs.ErrInvalid) || fixture.calls != 1 {
		t.Fatal("invalid ordinal reached owner", err)
	}
	p.CallerWorkload = "secret-broker"
	if _, err = service.AcceptManagedKubernetesOutputFrame(t.Context(), p, binding, input); !errors.Is(err, errs.ErrForbidden) || fixture.calls != 1 {
		t.Fatal("broker transport became gateway stage authority", err)
	}
}
