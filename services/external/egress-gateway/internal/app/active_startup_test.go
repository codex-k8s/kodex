package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
	internalobservability "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/observability"
)

// TestActiveStartupWithUnavailableOptionalProjections exercises all six listeners
// with the canonical 256-connection budget, local synthetic keys and no external API.
// TestActiveStartupWithUnavailableOptionalProjections запускает шесть listeners
// с каноническим бюджетом256 и синтетическими ключами, без внешнего API.
func TestActiveStartupWithUnavailableOptionalProjections(t *testing.T) {
	directory := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic.example.invalid"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(3 * time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, data, 0o440); err != nil {
			t.Fatal(err)
		}
		return path
	}
	config := Config{TechnicalAddress: "127.0.0.1:0", ConnectAddress: "127.0.0.1:0", STTConnectAddress: "127.0.0.1:0",
		MailConnectAddress: "127.0.0.1:0", IntegrationConnectAddress: "127.0.0.1:0", RuntimeConnectAddress: "127.0.0.1:0",
		MailPolicyFile: filepath.Join(directory, "missing-mail.json"), IntegrationPolicyFile: filepath.Join(directory, "missing-integration.json"),
		NativeConnectAddress: "127.0.0.1:0", NativePolicyFile: filepath.Join(directory, "missing-native.json"),
		RuntimeSigningKeyFile:     write("signing", []byte("synthetic-runtime-signing-material-0123456789")),
		RuntimeProxyCACertificate: write("ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		RuntimeProxyCAPrivateKey:  write("ca.key", pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))}
	active := loadRepositoryPolicy(t)
	if active.Limits().MaximumConnections != 256 {
		t.Fatal("fixture lost the canonical capacity")
	}
	readiness := serviceruntime.NewReadiness()
	metrics := sharedobservability.NewMetrics(metricsSubsystem, "test", map[string]string{})
	business, err := internalobservability.New(metrics.Register)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- runActive(ctx, context.Background(), config, active,
			[]netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:53")}, readiness, metrics, business)
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if ready, _ := readiness.Ready(); ready {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("active startup stopped before readiness: %v", err)
		case <-deadline.C:
			t.Fatal("active startup did not become ready")
		case <-tick.C:
		}
	}
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("active startup did not join cleanly: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active startup did not cancel and join")
	}
	if ready, _ := readiness.Ready(); ready {
		t.Fatal("stopped runtime retained readiness")
	}
}
