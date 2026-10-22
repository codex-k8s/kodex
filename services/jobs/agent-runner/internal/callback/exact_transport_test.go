package callback

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// exactTransportBindingFixture creates a disposable self-signed client identity and CA for constructor tests only.
// No live credentials, endpoint or listener is used; the generated private key remains in the temporary test directory.
// exactTransportBindingFixture создаёт одноразовую самоподписанную клиентскую идентичность и CA только для тестов конструктора.
// Живые учётные данные, endpoint и слушатель не используются; сгенерированный закрытый ключ остаётся во временном тестовом каталоге.
func exactTransportBindingFixture(t *testing.T) (model.TLSBinding, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1),
		Subject: pkix.Name{CommonName: "synthetic callback identity"}, DNSNames: []string{"controller.fixture.invalid"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	root := t.TempDir()
	binding := model.TLSBinding{ServerName: "controller.fixture.invalid",
		CAFile: filepath.Join(root, "ca.pem"), CertificateFile: filepath.Join(root, "client.pem"), PrivateKeyFile: filepath.Join(root, "client.key")}
	for path, raw := range map[string][]byte{binding.CAFile: ca, binding.CertificateFile: ca, binding.PrivateKeyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})} {
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return binding, ca
}

// TestExactTransportsKeepTLSIdentityAndBudgets checks both wrappers preserve TLS/SNI/client identity and distinct timeout budgets with independent state.
// This inspects constructed transports without sending network traffic or claiming owner authorization.
// TestExactTransportsKeepTLSIdentityAndBudgets проверяет TLS, SNI, клиентскую идентичность и разные бюджеты таймаута обеих обёрток с независимым состоянием.
// Проверяются созданные транспорты без сетевого обмена и заявления полномочий владельца.
func TestExactTransportsKeepTLSIdentityAndBudgets(t *testing.T) {
	binding, ca := exactTransportBindingFixture(t)
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("synthetic CA is invalid")
	}
	var previous *http.Transport
	for _, test := range []struct {
		name    string
		create  func(model.TLSBinding) (*http.Transport, error)
		timeout time.Duration
	}{
		{"callback", exactTransport, 30 * time.Second},
		{"mcp", MCPTransport, time.Duration(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds+5) * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			transport, err := test.create(binding)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.CloseIdleConnections()
			config := transport.TLSClientConfig
			if config == nil || config.MinVersion != tls.VersionTLS13 || config.MaxVersion != tls.VersionTLS13 || config.ServerName != binding.ServerName ||
				config.InsecureSkipVerify || !config.RootCAs.Equal(roots) || len(config.Certificates) != 1 || len(config.Certificates[0].Certificate) != 1 ||
				config.Certificates[0].PrivateKey == nil || !transport.DisableCompression || transport.Proxy != nil ||
				transport.MaxIdleConns != 2 || transport.MaxIdleConnsPerHost != 2 || transport.TLSHandshakeTimeout != 5*time.Second ||
				transport.ResponseHeaderTimeout != test.timeout || transport.MaxResponseHeaderBytes != 16<<10 {
				t.Fatal("exact transport fields changed")
			}
			if previous != nil && (previous == transport || previous.TLSClientConfig == config || previous.TLSClientConfig.RootCAs == config.RootCAs) {
				t.Fatal("exact transports share mutable state")
			}
			previous = transport
		})
	}
}

// TestExactTransportsKeepFailureMessages checks invalid CA/keypair files return no transport and the original caller-specific safe diagnostic text.
// The cases use only disposable generated identity files; parser and filesystem errors must not reveal their paths.
// TestExactTransportsKeepFailureMessages проверяет отсутствие транспорта и прежнюю безопасную диагностику конкретной обёртки для неверных файлов CA и пары ключа с сертификатом.
// Используются только одноразовые сгенерированные файлы; ошибки разбора и файловой системы не должны раскрывать их пути.
func TestExactTransportsKeepFailureMessages(t *testing.T) {
	for _, factory := range []struct {
		name   string
		create func(model.TLSBinding) (*http.Transport, error)
	}{
		{"runtime callback", exactTransport}, {"MCP", MCPTransport},
	} {
		for _, test := range []struct{ name, failure string }{
			{"missing CA", "read"}, {"empty CA", "read"}, {"oversized CA", "read"}, {"invalid CA", "parse"},
			{"missing certificate", "load"}, {"invalid key", "load"}, {"mismatched key", "load"},
		} {
			t.Run(factory.name+"/"+test.name, func(t *testing.T) {
				binding, _ := exactTransportBindingFixture(t)
				var path string
				var raw []byte
				switch test.name {
				case "missing CA":
					binding.CAFile += ".missing"
				case "empty CA":
					path = binding.CAFile
				case "oversized CA":
					path, raw = binding.CAFile, make([]byte, (1<<20)+1)
				case "invalid CA":
					path, raw = binding.CAFile, []byte("not a certificate")
				case "missing certificate":
					binding.CertificateFile += ".missing"
				case "invalid key":
					path, raw = binding.PrivateKeyFile, []byte("not a key")
				case "mismatched key":
					other, _ := exactTransportBindingFixture(t)
					binding.PrivateKeyFile = other.PrivateKeyFile
				}
				if path != "" {
					if err := os.WriteFile(path, raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				want := test.failure + " " + factory.name + " CA"
				if test.failure == "load" {
					want = "load " + factory.name + " client identity"
				}
				transport, err := factory.create(binding)
				if err == nil || err.Error() != want || transport != nil {
					t.Fatal("exact transport rejection changed")
				}
			})
		}
	}
}
