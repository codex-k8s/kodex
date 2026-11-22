package platform

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// queryKubernetesOperatorHistoryCount checks committed source cardinality in the disposable fixture.
// queryKubernetesOperatorHistoryCount проверяет cardinality committed sources в disposable fixture.
//
//go:embed testdata/sql/kubernetes_operator_history_count.sql
var queryKubernetesOperatorHistoryCount string

// kubernetesOperatorComponentFixture constructs public synthetic attestations, not live node proof.
// The ephemeral CA key stays local; no target connection, credential value or host file is used.
// kubernetesOperatorComponentFixture создаёт public synthetic attestations, не live node proof.
// Ephemeral CA key остаётся локальным; target connection, credential value и host files не используются.
func kubernetesOperatorComponentFixture(t *testing.T) value.KubernetesOperatorRegistry {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("create synthetic operator CA key")
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create synthetic public operator CA")
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	digest := sha256.Sum256(public)
	hash := hex.EncodeToString(digest[:])
	target := value.ManagedKubernetesTarget{Ref: "kbt_synthetic_operator", Revision: 1, ClusterUID: "2cc8de1d-1091-4f47-aed2-e7e3f39ef687",
		ServerOrigin: "https://api.synthetic.invalid:6443", TLSServerName: "api.synthetic.invalid", CertificateAuthoritySHA256: hash,
		EmbeddedContext: "synthetic-context", AddressIPs: []string{"192.0.2.10"},
		KubeconfigSourceOrigins: []string{"https://api.synthetic.invalid:6443", "https://127.0.0.1:40123"}}
	return value.KubernetesOperatorRegistry{Schema: "managed-kubernetes-operator/v1", ManagedBy: "git", Source: "repo:kubernetes-operator",
		InstallationRef: "install_synthetic_operator", Revision: 1, ProducedAt: now, IdentityNamespace: "synthetic-kubernetes-identities",
		UploadEndpoint: value.KubernetesIdentityUploadEndpoint{ServerOrigin: "https://upload.synthetic.invalid", TLSServerName: "upload.synthetic.invalid",
			CertificateAuthorityPEM: public, CertificateAuthoritySHA256: hash, AddressIPs: []string{"192.0.2.11"}},
		Targets: []value.KubernetesOperatorTarget{{Target: target, ProtectedNamespaces: []value.KubernetesProtectedNamespace{
			{Name: "synthetic-system", UID: "e8230808-e5d5-4c3a-b939-5cfe28e36281"}}}},
		Nodes: []value.KubernetesOperatorNode{{Transport: value.ManagedKubernetesNodeTransport{Ref: "knp_synthetic_initial", Generation: 1,
			TargetRef: target.Ref, TargetRevision: target.Revision, NodeName: "synthetic-node", NodeUID: "ddda8412-9aaf-488e-bb03-14ae147ac85e",
			ServerOrigin: "https://node.synthetic.invalid:10250", TLSServerName: "node.synthetic.invalid", AddressIPs: []string{"192.0.2.12"},
			CertificateAuthorityPEM: public, CertificateAuthoritySHA256: hash, ServerPublicKeySHA256: strings.Repeat("a", 64),
			NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Protocol: managedkubernetes.WebSocketV5}}}}
}

// cloneKubernetesOperatorComponent detaches every slice before a negative candidate is changed.
// cloneKubernetesOperatorComponent отделяет каждый slice до изменения negative candidate.
func cloneKubernetesOperatorComponent(t *testing.T, source value.KubernetesOperatorRegistry) value.KubernetesOperatorRegistry {
	t.Helper()
	raw, err := json.Marshal(source)
	if err != nil {
		t.Fatal("encode synthetic operator candidate")
	}
	cloned, err := value.DecodeKubernetesOperatorRegistry(raw, time.Now().UTC())
	if err != nil {
		t.Fatal("clone synthetic operator candidate")
	}
	return cloned
}

// TestKubernetesOperatorRegistryComponent proves real PostgreSQL forward-only source and pin history.
// It covers alias/key/generation/cluster/protection drift without claiming TLS, RBAC or setup grants.
// TestKubernetesOperatorRegistryComponent проверяет real PostgreSQL forward-only source и pin history.
// Покрывает alias/key/generation/cluster/protection drift, не заявляя TLS, RBAC либо setup grants.
func TestKubernetesOperatorRegistryComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("open isolated operator owner database")
	}
	defer pool.Close()
	makeRepository := func(candidate value.KubernetesOperatorRegistry) *Repository {
		return newSyntheticRuntimeComponentRepository(t, ctx, pool, func(repository *Repository) {
			if repository.ConfigureKubernetesOperator(candidate) != nil {
				t.Fatal("configure synthetic operator candidate")
			}
		})
	}
	readCurrent := func() kubernetesOperatorStored {
		current, err := scanKubernetesOperator(pool.QueryRow(ctx, queryKubernetesOperatorCurrent))
		if err != nil {
			t.Fatal("read accepted operator source")
		}
		return current
	}
	base := kubernetesOperatorComponentFixture(t)
	first := makeRepository(base)
	peer := makeRepository(base)
	start, results := make(chan struct{}), make(chan error, 2)
	for _, repository := range []*Repository{first, peer} {
		go func() {
			<-start
			results <- repository.RegisterKubernetesOperator(ctx)
		}()
	}
	close(start)
	left, right := <-results, <-results
	if left != nil || right != nil || first.RegisterKubernetesOperator(ctx) != nil || readCurrent().registry.Revision != 1 {
		t.Fatal("initial source enrollment or exact restart rejoin failed")
	}
	firstDigest := readCurrent().digest
	reject := func(candidate value.KubernetesOperatorRegistry, expectedRevision int64) {
		repository := makeRepository(candidate)
		if repository.RegisterKubernetesOperator(ctx) == nil || readCurrent().registry.Revision != expectedRevision {
			t.Fatal("foreign candidate changed accepted source or passed its boundary")
		}
	}
	aliasDrift := cloneKubernetesOperatorComponent(t, base)
	aliasDrift.Revision++
	aliasDrift.Targets[0].Target.KubeconfigSourceOrigins = []string{"https://127.0.0.1:40124"}
	reject(aliasDrift, 1)
	drift := cloneKubernetesOperatorComponent(t, base)
	drift.Targets[0].Target.ServerOrigin = "https://different.synthetic.invalid:6443"
	reject(drift, 1)
	if readCurrent().digest != firstDigest {
		t.Fatal("same-revision drift changed immutable accepted bytes")
	}
	retired := cloneKubernetesOperatorComponent(t, base)
	retired.Revision, retired.Nodes = 2, []value.KubernetesOperatorNode{}
	if makeRepository(retired).RegisterKubernetesOperator(ctx) != nil {
		t.Fatal("explicit pin retirement failed")
	}
	for _, alias := range []bool{false, true} {
		revive := cloneKubernetesOperatorComponent(t, base)
		revive.Revision = 3
		revive.Nodes[0].Transport.Ref, revive.Nodes[0].Transport.Generation = "knp_synthetic_revival", 2
		if alias {
			revive.Targets[0].Target.Ref = "kbt_synthetic_alias"
			revive.Nodes[0].Transport.TargetRef = revive.Targets[0].Target.Ref
		}
		reject(revive, 2)
	}
	fresh := cloneKubernetesOperatorComponent(t, base)
	fresh.Revision = 4
	fresh.Nodes[0].Transport.Ref, fresh.Nodes[0].Transport.Generation = "knp_synthetic_fresh", 2
	fresh.Nodes[0].Transport.ServerPublicKeySHA256 = strings.Repeat("b", 64)
	if makeRepository(fresh).RegisterKubernetesOperator(ctx) != nil || readCurrent().registry.Revision != 4 {
		t.Fatal("fresh key or skipped unaccepted source revision was rejected")
	}
	if first.RegisterKubernetesOperator(ctx) == nil || readCurrent().registry.Revision != 4 {
		t.Fatal("stale source restart rolled back accepted state")
	}
	oldGeneration := cloneKubernetesOperatorComponent(t, fresh)
	oldGeneration.Revision, oldGeneration.Targets[0].Target.Ref = 5, "kbt_generation_alias"
	oldGeneration.Nodes[0].Transport.Ref, oldGeneration.Nodes[0].Transport.Generation = "knp_generation_alias", 1
	oldGeneration.Nodes[0].Transport.TargetRef = oldGeneration.Targets[0].Target.Ref
	reject(oldGeneration, 4)
	foreign := cloneKubernetesOperatorComponent(t, fresh)
	foreign.Revision = 5
	foreign.Targets[0].Target.Ref = "kbt_foreign_cluster"
	foreign.Targets[0].Target.ClusterUID = "f32097ba-ae58-4d97-a80b-c2cd2a06fe5f"
	foreign.Nodes[0].Transport.Ref, foreign.Nodes[0].Transport.Generation = "knp_foreign_key_reuse", 1
	foreign.Nodes[0].Transport.NodeUID = "f1b44bd2-6f7c-4734-9061-51dc33d7a774"
	foreign.Nodes[0].Transport.TargetRef = foreign.Targets[0].Target.Ref
	reject(foreign, 4)
	retarget := cloneKubernetesOperatorComponent(t, fresh)
	retarget.Revision, retarget.Targets[0].Target.Revision = 5, 2
	retarget.Targets[0].Target.ClusterUID = foreign.Targets[0].Target.ClusterUID
	retarget.Nodes = []value.KubernetesOperatorNode{}
	reject(retarget, 4)
	aliasShrink := cloneKubernetesOperatorComponent(t, fresh)
	aliasShrink.Revision, aliasShrink.Targets[0].Target.Ref = 5, "kbt_protection_alias"
	aliasShrink.Targets[0].ProtectedNamespaces, aliasShrink.Nodes = []value.KubernetesProtectedNamespace{}, []value.KubernetesOperatorNode{}
	reject(aliasShrink, 4)
	upgrade := cloneKubernetesOperatorComponent(t, fresh)
	upgrade.Revision, upgrade.Targets[0].Target.Revision, upgrade.Nodes = 5, 2, []value.KubernetesOperatorNode{}
	if makeRepository(upgrade).RegisterKubernetesOperator(ctx) != nil {
		t.Fatal("target metadata revision could not advance without changing cluster identity")
	}
	shrink := cloneKubernetesOperatorComponent(t, upgrade)
	shrink.Revision, shrink.Targets[0].Target.Revision = 6, 3
	shrink.Targets[0].ProtectedNamespaces = []value.KubernetesProtectedNamespace{}
	reject(shrink, 5)
	var revisionCount int
	if pool.QueryRow(ctx, queryKubernetesOperatorHistoryCount).Scan(&revisionCount) != nil || revisionCount != 4 {
		t.Fatal("rejected or duplicate candidates left a partial source history")
	}
}
