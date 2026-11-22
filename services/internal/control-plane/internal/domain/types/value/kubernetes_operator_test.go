package value

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// kubernetesOperatorFixture creates public metadata with an ephemeral local CA; it performs no I/O.
// kubernetesOperatorFixture создаёт public metadata с ephemeral local CA без внешних I/O.
func kubernetesOperatorFixture(t *testing.T) (KubernetesOperatorRegistry, time.Time) {
	t.Helper()
	now := time.Date(2026, time.November, 14, 12, 0, 0, 0, time.UTC)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("create synthetic CA key")
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(48 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal("create synthetic public CA")
	}
	public := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	digest := sha256.Sum256(public)
	hash := hex.EncodeToString(digest[:])
	target := ManagedKubernetesTarget{Ref: "kbt_synthetic_target", Revision: 1, ServerOrigin: "https://api.synthetic.invalid:6443",
		TLSServerName: "api.synthetic.invalid", CertificateAuthoritySHA256: hash, EmbeddedContext: "synthetic-single-context",
		ClusterUID: "63d210fe-3f36-431b-a79b-8398a23cbd8f", AddressIPs: []string{"192.0.2.10"},
		KubeconfigSourceOrigins: []string{"https://api.synthetic.invalid:6443", "https://127.0.0.1:40123"}}
	return KubernetesOperatorRegistry{Schema: "managed-kubernetes-operator/v1", ManagedBy: "git", Source: "repo:kubernetes-operator", InstallationRef: "install_synthetic", Revision: 1, ProducedAt: now,
		IdentityNamespace: "synthetic-broker-identities",
		UploadEndpoint: KubernetesIdentityUploadEndpoint{ServerOrigin: "https://setup.synthetic.invalid", TLSServerName: "setup.synthetic.invalid",
			CertificateAuthorityPEM: public, CertificateAuthoritySHA256: hash, AddressIPs: []string{"192.0.2.11"}},
		Targets: []KubernetesOperatorTarget{{Target: target, ProtectedNamespaces: []KubernetesProtectedNamespace{{Name: "synthetic-platform",
			UID: "118d9d8c-ccaf-4c51-a601-8986069ab4fd"}}}},
		Nodes: []KubernetesOperatorNode{{Transport: ManagedKubernetesNodeTransport{Ref: "knp_synthetic_node", Generation: 1,
			TargetRef: target.Ref, TargetRevision: target.Revision, NodeName: "synthetic-node", NodeUID: "0ca05615-6168-4900-aa0c-b1602c85e8a0",
			ServerOrigin: "https://node.synthetic.invalid:10250", TLSServerName: "node.synthetic.invalid", AddressIPs: []string{"192.0.2.12"},
			CertificateAuthorityPEM: public, CertificateAuthoritySHA256: hash, ServerPublicKeySHA256: strings.Repeat("a", 64),
			NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Protocol: managedkubernetes.WebSocketV5}}}}, now
}

// TestKubernetesOperatorCanonicalMetadata rejects ambiguous encodings before using any target or pin.
// TestKubernetesOperatorCanonicalMetadata отклоняет ambiguous encoding до использования target либо pin.
func TestKubernetesOperatorCanonicalMetadata(t *testing.T) {
	registry, now := kubernetesOperatorFixture(t)
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal("encode public operator fixture")
	}
	for _, input := range [][]byte{raw, append(append([]byte(nil), raw...), '\n')} {
		decoded, err := DecodeKubernetesOperatorRegistry(input, now)
		if err != nil || decoded.Targets[0].Target.Ref != registry.Targets[0].Target.Ref || len(decoded.Nodes) != 1 {
			t.Fatal("valid canonical public operator artifact was rejected")
		}
	}
	pretty, _ := json.MarshalIndent(registry, "", "  ")
	for _, input := range [][]byte{
		[]byte("null"), append(append([]byte(nil), raw...), []byte("{}")...), pretty,
		[]byte(strings.Replace(string(raw), `"revision":1`, `"revision":1,"revision":1`, 1)),
		[]byte(strings.Replace(string(raw), `"revision":1`, `"Revision":1`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":`, `"unknown":true,"schema":`, 1)),
		append(append([]byte(nil), raw...), '\n', '\n'), make([]byte, MaximumKubernetesOperatorBytes+1),
	} {
		if _, err := DecodeKubernetesOperatorRegistry(input, now); err == nil {
			t.Fatal("ambiguous or noncanonical operator artifact was accepted")
		}
	}
}

// TestKubernetesOperatorTargetAndServingKeyBoundary checks finite origins and distinct incarnations.
// It proves metadata validation only, not live TLS, RBAC, NodeUID atomicity or persistent retirement.
// TestKubernetesOperatorTargetAndServingKeyBoundary проверяет finite origins и distinct incarnations.
// Доказывает metadata validation, не live TLS, RBAC, NodeUID atomicity либо persistent retirement.
func TestKubernetesOperatorTargetAndServingKeyBoundary(t *testing.T) {
	mutations := []func(*KubernetesOperatorRegistry){
		func(r *KubernetesOperatorRegistry) { r.Targets = nil },
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.KubeconfigSourceOrigins = nil },
		func(r *KubernetesOperatorRegistry) {
			r.Targets[0].Target.KubeconfigSourceOrigins = []string{"https://127.0.0.1:40123/arbitrary"}
		},
		func(r *KubernetesOperatorRegistry) { r.Revision = 0 },
		func(r *KubernetesOperatorRegistry) { r.ManagedBy = "ui" },
		func(r *KubernetesOperatorRegistry) { r.ProducedAt = r.ProducedAt.Add(time.Minute) },
		func(r *KubernetesOperatorRegistry) { r.Revision = 9007199254740992 },
		func(r *KubernetesOperatorRegistry) { r.Targets = append(r.Targets, r.Targets[0]) },
		func(r *KubernetesOperatorRegistry) {
			r.Targets[0].Target.ClusterUID = "00000000-0000-0000-0000-000000000000"
		},
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.ServerOrigin = "http://api.synthetic.invalid" },
		func(r *KubernetesOperatorRegistry) {
			r.Targets[0].Target.ServerOrigin = "https://user:private@api.synthetic.invalid"
		},
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.ServerOrigin += "/arbitrary/path" },
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.ServerOrigin = "https://bad_host.invalid" },
		func(r *KubernetesOperatorRegistry) {
			r.Targets[0].Target.ServerOrigin = "https://" + strings.Repeat("a", 64) + ".invalid"
		},
		func(r *KubernetesOperatorRegistry) {
			r.UploadEndpoint.ServerOrigin = "https://" + strings.Repeat("a.", 512) + "invalid"
		},
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.AddressIPs = []string{"0.0.0.0"} },
		func(r *KubernetesOperatorRegistry) {
			r.Targets[0].Target.AddressIPs = []string{"192.0.2.10", "192.0.2.10"}
		},
		func(r *KubernetesOperatorRegistry) { r.Targets[0].Target.AddressIPs = []string{"::ffff:192.0.2.10"} },
		func(r *KubernetesOperatorRegistry) { r.Targets[0].ProtectedNamespaces[0].UID = "foreign-namespace" },
		func(r *KubernetesOperatorRegistry) { r.UploadEndpoint.TLSServerName = "*.synthetic.invalid" },
		func(r *KubernetesOperatorRegistry) {
			r.UploadEndpoint.CertificateAuthoritySHA256 = strings.Repeat("b", 64)
		},
		func(r *KubernetesOperatorRegistry) { r.UploadEndpoint.ServerOrigin += "?proxy=foreign" },
		func(r *KubernetesOperatorRegistry) { r.Nodes[0].Transport.TargetRevision = 2 },
		func(r *KubernetesOperatorRegistry) {
			r.Nodes[0].Transport.ExpiresAt = r.Nodes[0].Transport.NotBefore.Add(25 * time.Hour)
		},
		func(r *KubernetesOperatorRegistry) { r.Nodes[0].Transport.Protocol = "UNREGISTERED" },
		func(r *KubernetesOperatorRegistry) {
			duplicate := r.Nodes[0]
			duplicate.Transport.Ref = "knp_duplicate_node"
			duplicate.Transport.Generation++
			r.Nodes = append(r.Nodes, duplicate)
		},
		func(r *KubernetesOperatorRegistry) {
			foreign := r.Nodes[0]
			foreign.Transport.Ref = "knp_foreign_node"
			foreign.Transport.NodeUID = "6961be45-a1a1-4ffa-b721-ece898188e37"
			r.Nodes = append(r.Nodes, foreign)
		},
	}
	for index, mutate := range mutations {
		registry, now := kubernetesOperatorFixture(t)
		mutate(&registry)
		if registry.Validate(now) == nil {
			t.Fatalf("invalid operator metadata crossed its boundary (case %d)", index)
		}
	}
}

// TestKubernetesOperatorRetainsHistoricalPinMetadata preserves expired/retired evidence as metadata.
// Execution must separately require a current active owner pin; syntax acceptance grants no transport.
// TestKubernetesOperatorRetainsHistoricalPinMetadata сохраняет expired/retired evidence как metadata.
// Execution требует current active owner pin отдельно; syntax acceptance не разрешает transport.
func TestKubernetesOperatorRetainsHistoricalPinMetadata(t *testing.T) {
	registry, now := kubernetesOperatorFixture(t)
	registry.Nodes[0].Transport.NotBefore = now.Add(-30 * time.Minute)
	registry.Nodes[0].Transport.ExpiresAt = now.Add(-time.Minute)
	registry.Nodes[0].Retired = true
	registry.Targets[0].Target.Revision = 2
	if registry.Validate(now) != nil {
		t.Fatal("historical pin was silently discarded instead of retained for owner reconciliation")
	}
	registry.Targets = []KubernetesOperatorTarget{}
	if registry.Validate(now) != nil {
		t.Fatal("explicit empty target set could not retain retired transport metadata")
	}
	if registry.Validate(now.Add(49*time.Hour)) != nil || registry.UploadEndpoint.Validate(now.Add(49*time.Hour)) == nil {
		t.Fatal("historical metadata validation was confused with current endpoint trust")
	}
}
