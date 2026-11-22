package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgtype"
)

// kubernetesOutputStorageFixture counts actual port writes over the existing local object store.
// It proves owner one-write semantics, not remote S3 durability or public artifact publication.
// kubernetesOutputStorageFixture считает actual port writes поверх existing local object store.
// Доказывает owner one-write semantics, не remote S3 durability либо public artifact publication.
type kubernetesOutputStorageFixture struct {
	objectstorage.Store
	puts    int
	unknown bool
}

// Put preserves the underlying versioned test receipt while counting every external write attempt.
// Put сохраняет underlying versioned test receipt, считая каждый external write attempt.
func (store *kubernetesOutputStorageFixture) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	store.puts++
	receipt, err := store.Store.Put(ctx, input)
	if store.unknown && err == nil {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	return receipt, err
}

// assertKubernetesOutputStageDelivery follows actual accepted stages and metadata readback across
// new ordinal credits; no second PUT occurs and an older ordinal never authorizes accepted bytes.
// assertKubernetesOutputStageDelivery проверяет actual accepted stages и metadata readback при
// новых ordinal credits; второго PUT нет, прежний ordinal не допускает accepted bytes.
func assertKubernetesOutputStageDelivery(t *testing.T, ctx context.Context, repository *Repository, storage *kubernetesOutputStorageFixture, binding value.ManagedKubernetesBinding, output command.ManagedKubernetesBeginFrame, admission entity.ManagedKubernetesFrameAdmission) {
	t.Helper()
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	gateway, resolveErr := repository.ResolvePrincipal(ctx, gateway)
	if resolveErr != nil {
		t.Fatal("resolve output gateway fixture", resolveErr)
	}
	gateway.Permission = "platform.runtime.kubernetes.output.accept"
	sum := sha256.Sum256([]byte("body"))
	input := command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: output.StreamRef, Sequence: output.Sequence, DeliveryOrdinal: admission.DeliveryOrdinal}, Channel: output.Channel, PayloadDigest: hex.EncodeToString(sum[:]), ByteCount: 4, Payload: []byte("body")}
	if input.PayloadDigest != output.PayloadDigest {
		t.Fatal("stage fixture does not match admitted bytes")
	}
	receipt, err := repository.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, input)
	if err != nil || receipt.State != enum.KubernetesOutputAccepted || receipt.DeliveryOrdinal != admission.DeliveryOrdinal || !receipt.AuthorizedUntil.Equal(admission.ExpiresAt) || storage.puts != 1 {
		t.Fatal("exact durable stage was not accepted under original ordinal", receipt.State, storage.puts, err)
	}
	// The service-owned buffer may be cleared without changing captured versioned content.
	// Service-owned buffer можно очистить без изменения captured versioned content.
	clear(input.Payload)
	gateway.Permission = "platform.runtime.kubernetes.output.read"
	replay, err := repository.GetManagedKubernetesOutputFrame(ctx, gateway, binding, input.ManagedKubernetesOutputFrameLookup)
	if err != nil || replay != receipt || storage.puts != 1 {
		t.Fatal("lost stage ACK metadata read changed credit or wrote bytes", err)
	}
	if admission.DeliveryOrdinal == 1 {
		broken, fault := kubernetesAuthorityFaultRepository(t, ctx, repository, queryKubernetesFrameStream, pgtype.BoolOID)
		var before, after string
		if err := repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(stage)::text) FROM control_plane.kubernetes_output_stages stage WHERE ref=$1", receipt.Ref).Scan(&before); err != nil {
			t.Fatal("read stage before infrastructure fault", err)
		}
		if _, err := broken.GetManagedKubernetesOutputFrame(ctx, gateway, binding, input.ManagedKubernetesOutputFrameLookup); !errors.Is(err, errs.ErrUnavailable) || fault.fired.Load() != 1 {
			t.Fatal("cached stage read skipped child/current-frame proof or swallowed Scan error", err)
		}
		if err := repository.pool.QueryRow(ctx, "SELECT md5(to_jsonb(stage)::text) FROM control_plane.kubernetes_output_stages stage WHERE ref=$1", receipt.Ref).Scan(&after); err != nil || before != after || storage.puts != 1 {
			t.Fatal("infrastructure read failure changed durable stage", err)
		}
	}
	if admission.DeliveryOrdinal > 1 {
		input.DeliveryOrdinal--
		input.Payload = []byte("body")
		gateway.Permission = "platform.runtime.kubernetes.output.accept"
		if _, err := repository.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, input); !errors.Is(err, errs.ErrForbidden) || storage.puts != 1 {
			t.Fatal("old ordinal was reused after a fresh delivery", err)
		}
	}
}

// assertKubernetesOutputUnknownWrite retains a real successful local write whose transport receipt
// is lost. Neither a repeated Accept nor metadata Get performs another PUT or authorizes an ACK.
// assertKubernetesOutputUnknownWrite сохраняет actual local write с потерянной transport receipt.
// Повторный Accept и metadata Get не выполняют новый PUT и не разрешают ACK.
func assertKubernetesOutputUnknownWrite(t *testing.T, ctx context.Context, repository *Repository, storage *kubernetesOutputStorageFixture, binding value.ManagedKubernetesBinding, output command.ManagedKubernetesBeginFrame) {
	t.Helper()
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	gateway, resolveErr := repository.ResolvePrincipal(ctx, gateway)
	if resolveErr != nil {
		t.Fatal("resolve output gateway fixture", resolveErr)
	}
	gateway.Permission = "platform.runtime.kubernetes.output.accept"
	storage.unknown = true
	defer func() { storage.unknown = false }()
	payload := make([]byte, output.ByteCount)
	for i := range payload {
		payload[i] = 'x'
	}
	input := command.ManagedKubernetesAcceptOutputFrame{ManagedKubernetesOutputFrameLookup: command.ManagedKubernetesOutputFrameLookup{StreamRef: output.StreamRef, Sequence: output.Sequence, DeliveryOrdinal: 1}, Channel: output.Channel, PayloadDigest: output.PayloadDigest, ByteCount: output.ByteCount, Payload: payload}
	for attempt := 0; attempt < 2; attempt++ {
		receipt, err := repository.AcceptManagedKubernetesOutputFrame(ctx, gateway, binding, input)
		if err != nil || receipt.State != enum.KubernetesOutputUnknown || !receipt.AuthorizedUntil.IsZero() || storage.puts != 2 {
			t.Fatal("uncertain stage write was retried or acknowledged", storage.puts, err)
		}
	}
	gateway.Permission = "platform.runtime.kubernetes.output.read"
	receipt, err := repository.GetManagedKubernetesOutputFrame(ctx, gateway, binding, input.ManagedKubernetesOutputFrameLookup)
	if err != nil || receipt.State != enum.KubernetesOutputUnknown || !receipt.AuthorizedUntil.IsZero() || storage.puts != 2 {
		t.Fatal("unknown stage read minted delivery credit", err)
	}
}

// assertKubernetesOutputClosedReadback retains private stage metadata after stop but suppresses credit.
// It cannot publish or retrieve bytes, and the old original binding never authorizes another stream.
// assertKubernetesOutputClosedReadback сохраняет private stage metadata после stop без credit.
// Не публикует/читает bytes; old original binding не допускает другой stream.
func assertKubernetesOutputClosedReadback(t *testing.T, ctx context.Context, repository *Repository, binding value.ManagedKubernetesBinding, streamRef string) {
	t.Helper()
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim"}, "integration-gateway")
	gateway, resolveErr := repository.ResolvePrincipal(ctx, gateway)
	if resolveErr != nil {
		t.Fatal("resolve output gateway fixture", resolveErr)
	}
	gateway.Permission = "platform.runtime.kubernetes.output.read"
	lookup := command.ManagedKubernetesOutputFrameLookup{StreamRef: streamRef, Sequence: 1, DeliveryOrdinal: 4}
	receipt, err := repository.GetManagedKubernetesOutputFrame(ctx, gateway, binding, lookup)
	if err != nil || receipt.State != enum.KubernetesOutputCancelled || !receipt.AuthorizedUntil.IsZero() {
		t.Fatal("closed stream stage retained output admission", err)
	}
	foreign := binding
	foreign.WorkloadInstance += "-foreign"
	if _, err := repository.GetManagedKubernetesOutputFrame(ctx, gateway, foreign, lookup); !errors.Is(err, errs.ErrNotFound) {
		t.Fatal("stage metadata escaped original binding", err)
	}
}

// kubernetesOutputFixtureDigest hashes the actual bounded unknown-write fixture bytes.
// kubernetesOutputFixtureDigest хеширует actual bounded unknown-write fixture bytes.
func kubernetesOutputFixtureDigest() string {
	payload := make([]byte, 32768)
	for i := range payload {
		payload[i] = 'x'
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
