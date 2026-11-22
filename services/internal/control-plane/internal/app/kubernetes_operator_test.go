package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	platformrepository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/repository/postgres/platform"
)

// TestKubernetesOperatorProjectionHashBoundary uses a real file descriptor and ConfigMap-style symlink.
// It proves exact byte pinning and source configuration only, never owner enrollment or live target access.
// TestKubernetesOperatorProjectionHashBoundary использует real file descriptor и ConfigMap-style symlink.
// Доказывает exact byte pinning и source configuration, не owner enrollment либо live target access.
func TestKubernetesOperatorProjectionHashBoundary(t *testing.T) {
	public, _ := startupPostgresConfigFixture(t)
	ca, err := os.ReadFile(public.PostgresCAFile)
	if err != nil {
		t.Fatal("read synthetic public CA")
	}
	caDigest := sha256.Sum256(ca)
	registry := value.KubernetesOperatorRegistry{Schema: "managed-kubernetes-operator/v1", ManagedBy: "git", Source: "repo:kubernetes-operator",
		InstallationRef: "install_synthetic", Revision: 1, ProducedAt: time.Now().UTC(), IdentityNamespace: "synthetic-identities",
		Targets: []value.KubernetesOperatorTarget{}, Nodes: []value.KubernetesOperatorNode{},
		UploadEndpoint: value.KubernetesIdentityUploadEndpoint{ServerOrigin: "https://setup.synthetic.invalid", TLSServerName: "setup.synthetic.invalid",
			CertificateAuthorityPEM: ca, CertificateAuthoritySHA256: hex.EncodeToString(caDigest[:]), AddressIPs: []string{"192.0.2.10"}}}
	raw, err := json.Marshal(registry)
	if err != nil {
		t.Fatal("encode synthetic public operator metadata")
	}
	raw = append(raw, '\n')
	directory := t.TempDir()
	path := filepath.Join(directory, "operator.json")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write synthetic public operator metadata")
	}
	link := filepath.Join(directory, "projection.json")
	if os.Symlink("operator.json", link) != nil {
		t.Fatal("create synthetic ConfigMap projection")
	}
	hash := sha256.Sum256(raw)
	config := Config{KubernetesOperatorRegistryFile: link, KubernetesOperatorRegistryHash: hex.EncodeToString(hash[:])}
	repository := new(platformrepository.Repository)
	if configureKubernetesOperator(repository, config) != nil || repository.ConfigureKubernetesOperator(registry) != nil {
		t.Fatal("valid pinned projection did not configure its detached metadata")
	}
	for _, invalid := range []Config{
		{KubernetesOperatorRegistryFile: link},
		{KubernetesOperatorRegistryHash: config.KubernetesOperatorRegistryHash},
		{KubernetesOperatorRegistryFile: "relative.json", KubernetesOperatorRegistryHash: config.KubernetesOperatorRegistryHash},
		{KubernetesOperatorRegistryFile: link, KubernetesOperatorRegistryHash: strings.Repeat("f", 64)},
	} {
		if !errors.Is(configureKubernetesOperator(new(platformrepository.Repository), invalid), value.ErrKubernetesOperator) {
			t.Fatal("missing or foreign artifact pins were accepted")
		}
	}
	if os.WriteFile(path, append(raw, ' '), 0600) != nil || !errors.Is(configureKubernetesOperator(new(platformrepository.Repository), config), value.ErrKubernetesOperator) {
		t.Fatal("projection replacement kept the previous byte pin")
	}
	if configureKubernetesOperator(nil, Config{}) != nil {
		t.Fatal("missing optional source selected a fallback")
	}
}
