package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// kubernetesLargeResourcePolicyFixture builds only synthetic operator pins, never provider proof.
// Its full32-volume shape exceeds the former16KiB target row without inflating the registry budget.
// kubernetesLargeResourcePolicyFixture создаёт только synthetic operator pins, не provider proof.
// Полная32-volume shape превышает прежнюю16KiB target row, не увеличивая registry budget.
func kubernetesLargeResourcePolicyFixture() *managedkubernetes.ResourcePolicy {
	policy := &managedkubernetes.ResourcePolicy{}
	for index := 1; index <= 32; index++ {
		policy.Storage = append(policy.Storage, managedkubernetes.StorageQualification{Provider: managedkubernetes.StorageProviderCSIAppDataV1,
			PVCNamespace: "apps", PVCName: fmt.Sprintf("claim-%02d", index), PVCUID: fmt.Sprintf("20000000-0000-4000-8000-%012d", index), PVName: fmt.Sprintf("volume-%02d", index), PVUID: fmt.Sprintf("30000000-0000-4000-8000-%012d", index),
			StorageClassName: "qualified-csi", StorageClassUID: "40000000-0000-4000-8000-000000000001", Provisioner: "csi.synthetic.invalid", CSIDriver: "csi.synthetic.invalid", CSIDriverUID: "50000000-0000-4000-8000-000000000001",
			CSIDriverSpecSHA256: strings.Repeat("a", 64), VolumeSourceSHA256: strings.Repeat("b", 64), MountOptionsSHA256: strings.Repeat("c", 64), VolumeMode: "Filesystem", MountReadOnly: false})
	}
	return policy
}

// TestKubernetesResourcePolicyOwnerComponent verifies the real bounded target storage path and
// immutable target revision rule. Synthetic qualifications prove no actual CSI driver or mount.
// TestKubernetesResourcePolicyOwnerComponent проверяет real bounded target storage path и
// immutable target revision rule. Synthetic qualifications не доказывают actual CSI driver либо mount.
func TestKubernetesResourcePolicyOwnerComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool := newIsolatedAssistantComponentPool(t, ctx)
	t.Cleanup(pool.Close)
	candidate := kubernetesOperatorComponentFixture(t)
	candidate.Targets[0].ResourcePolicy = kubernetesLargeResourcePolicyFixture()
	raw, err := json.Marshal(candidate.Targets[0])
	if err != nil || len(raw) <= 16384 || len(raw) > value.MaximumKubernetesTargetBytes {
		t.Fatal("large valid target fixture missed the former bound")
	}
	repository := newSyntheticRuntimeComponentRepository(t, ctx, pool, func(repository *Repository) {
		if repository.ConfigureKubernetesOperator(candidate) != nil {
			t.Fatal("configure bounded inline resource policy")
		}
	})
	if err := repository.RegisterKubernetesOperator(ctx); err != nil {
		t.Fatal("register large qualified operator target", err)
	}
	var stored int
	if pool.QueryRow(ctx, "SELECT octet_length(payload) FROM control_plane.kubernetes_target_revisions WHERE ref=$1 AND revision=1", candidate.Targets[0].Target.Ref).Scan(&stored) != nil || stored != len(raw) {
		t.Fatal("target row lost or truncated inline qualifications")
	}
	// Keep a matching JSON identity/checksum while crossing only the database payload byte boundary.
	// Сохраняем matching JSON identity/checksum, превышая только database payload byte boundary.
	changed := candidate.Targets[0]
	changed.Target.Revision = 2
	future, err := json.Marshal(changed)
	if err != nil {
		t.Fatal("encode oversized target fixture")
	}
	oversized := strings.Repeat(" ", value.MaximumKubernetesTargetBytes) + string(future)
	digest := sha256.Sum256([]byte(oversized))
	_, err = pool.Exec(ctx, queryKubernetesOperatorTargetInsert, pgx.StrictNamedArgs{"ref": changed.Target.Ref, "revision": int64(2), "registry_revision": int64(1), "cluster_uid": changed.Target.ClusterUID, "payload": oversized, "content_sha256": hex.EncodeToString(digest[:])})
	var databaseError *pgconn.PgError
	if !errors.As(err, &databaseError) || databaseError.Code != "23514" {
		t.Fatal("oversized target payload did not hit the database bound")
	}
	next := candidate
	next.Revision = 2
	next.Targets = append([]value.KubernetesOperatorTarget(nil), candidate.Targets...)
	bytes, err := json.Marshal(next.Targets[0].ResourcePolicy)
	if err != nil {
		t.Fatal("detach inline policy fixture")
	}
	detached, _, err := managedkubernetes.DecodeResourcePolicy(bytes)
	if err != nil {
		t.Fatal("decode inline policy fixture")
	}
	detached.Storage[0].MountReadOnly = true
	next.Targets[0].ResourcePolicy = &detached
	replacement := *repository
	replacement.kubernetesOperator = nil
	replacement.kubernetesOperatorDigest = ""
	if replacement.ConfigureKubernetesOperator(next) != nil {
		t.Fatal("configure next synthetic source revision")
	}
	if err := replacement.RegisterKubernetesOperator(ctx); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("resource qualification changed without target revision", err)
	}
	var sources, targets int
	if pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM control_plane.kubernetes_operator_registry_revisions),(SELECT count(*) FROM control_plane.kubernetes_target_revisions)").Scan(&sources, &targets) != nil || sources != 1 || targets != 1 {
		t.Fatal("rejected target change left partial source history")
	}
}
