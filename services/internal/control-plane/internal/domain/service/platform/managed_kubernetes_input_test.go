package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// privateKubernetesInputFixture observes one private delivery and optional readiness compatibility.
// It does not implement durable claims, target authority or an artifact publication consumer.
// privateKubernetesInputFixture наблюдает private delivery и optional readiness compatibility.
// Не реализует durable claims, target authority либо artifact publication consumer.
type privateKubernetesInputFixture struct {
	*managedKubernetesRepositoryFixture
	result   entity.ManagedKubernetesInputFrameAdmission
	revision uint32
}

// ClaimManagedKubernetesInputFrame transfers only the fixture-owned result for boundary checks.
// ClaimManagedKubernetesInputFrame передаёт fixture-owned result лишь для boundary checks.
func (fixture *privateKubernetesInputFixture) ClaimManagedKubernetesInputFrame(context.Context, value.Principal, value.ManagedKubernetesBinding, string) (entity.ManagedKubernetesInputFrameAdmission, error) {
	fixture.calls++
	return fixture.result, nil
}

// ManagedKubernetesOutputContract returns test compatibility without pretending to probe storage.
// ManagedKubernetesOutputContract возвращает test compatibility без выдуманной storage probe.
func (fixture *privateKubernetesInputFixture) ManagedKubernetesOutputContract(context.Context, value.Principal) (uint32, error) {
	return fixture.revision, nil
}

// TestManagedKubernetesInputClaimNeverUsesGenericSource requires the exact gateway method and original
// invocation variant; valid payload is copied then the exclusive repository buffer is cleared.
// TestManagedKubernetesInputClaimNeverUsesGenericSource требует exact gateway method/original
// invocation variant; valid payload копируется, exclusive repository buffer очищается.
func TestManagedKubernetesInputClaimNeverUsesGenericSource(t *testing.T) {
	service, base, p, binding := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.input.claim")
	p.CallerWorkload = "integration-gateway"
	if _, err := service.ClaimManagedKubernetesInputFrame(t.Context(), p, binding, "kst_original"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("missing private owner became generic artifact read", err)
	}
	payload := []byte("synthetic private input")
	sum := sha256.Sum256(payload)
	digest, _ := managedkubernetes.BindingDigest(binding)
	fixture := &privateKubernetesInputFixture{managedKubernetesRepositoryFixture: base, result: entity.ManagedKubernetesInputFrameAdmission{Disposition: enum.KubernetesInputFrameDeliver, Frame: &entity.ManagedKubernetesClaimedInputFrame{Ref: "kif_original_source", InvocationRef: binding.InvocationRef, StreamRef: "kst_original", Generation: binding.Generation, Sequence: 1, Channel: "STDIN", PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: uint32(len(payload)), Payload: payload, DeliveryExpiresAt: time.Now().Add(5 * time.Second), BindingDigest: digest}}}
	service, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	wrong := p
	wrong.CallerWorkload = "secret-broker"
	if _, err := service.ClaimManagedKubernetesInputFrame(t.Context(), wrong, binding, "kst_original"); !errors.Is(err, errs.ErrForbidden) || fixture.calls != 0 {
		t.Fatal("wrong caller reached private input owner", err)
	}
	result, err := service.ClaimManagedKubernetesInputFrame(t.Context(), p, binding, "kst_original")
	if err != nil || result.Frame == nil || !bytes.Equal(result.Frame.Payload, []byte("synthetic private input")) || !bytes.Equal(payload, make([]byte, len(payload))) || fixture.calls != 1 {
		t.Fatal("private input copy/zeroing failed", err)
	}
	clear(result.Frame.Payload)
	fixture.result = entity.ManagedKubernetesInputFrameAdmission{Disposition: enum.KubernetesInputFrameUnknown, Frame: &entity.ManagedKubernetesClaimedInputFrame{Payload: []byte("hidden")}}
	if _, err := service.ClaimManagedKubernetesInputFrame(t.Context(), p, binding, "kst_original"); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("unknown claim exposed hidden payload", err)
	}
}

// TestManagedKubernetesOutputContractDoesNotUseCapacityRevision keeps absent/future output support
// closed even though the independent common delivery contract may already be revision one.
// TestManagedKubernetesOutputContractDoesNotUseCapacityRevision закрывает absent/future output
// support независимо от common delivery contract revision1.
func TestManagedKubernetesOutputContractDoesNotUseCapacityRevision(t *testing.T) {
	service, base, p, _ := managedKubernetesServiceFixture(t, "platform.runtime.kubernetes.readiness")
	if revision, err := service.CheckManagedKubernetesOutputContract(t.Context(), p); err != nil || revision != 0 {
		t.Fatal("missing output backend invented readiness", err)
	}
	fixture := &privateKubernetesInputFixture{managedKubernetesRepositoryFixture: base, revision: managedkubernetes.OutputContractRevision}
	service, err := New(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if revision, err := service.CheckManagedKubernetesOutputContract(t.Context(), p); err != nil || revision != managedkubernetes.OutputContractRevision {
		t.Fatal("exact output contract rejected", err)
	}
	fixture.revision++
	if _, err := service.CheckManagedKubernetesOutputContract(t.Context(), p); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatal("future output contract downgraded", err)
	}
	p.CallerWorkload = "integration-gateway"
	p.Permission = "platform.runtime.integrations.delivery.check"
	if _, err := service.CheckManagedKubernetesOutputContract(t.Context(), p); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("common capacity probe gained output authority", err)
	}
}
