package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nkeys"
)

// natsDiagnosticFixtureImage pins the existing canonical broker image; the
// component rejects drift against repository deployment source before Docker.
// natsDiagnosticFixtureImage закрепляет прежний канонический образ broker;
// component отклоняет расхождение с deploy source до обращения к Docker.
const natsDiagnosticFixtureImage = "docker.io/library/nats:2.14.4-alpine@sha256:f2123f533c2b0cada0a5c5ec434fb2b8cfe1cf220215ef9d7517e1372917ad66"

// natsFixtureMaterial holds only paths to generated disposable test material.
// It carries no real runtime credentials or cluster identity.
// natsFixtureMaterial содержит лишь пути к одноразовым материалам проверки.
// Реальных runtime-реквизитов либо кластерной identity в ней нет.
type natsFixtureMaterial struct {
	ca, wrongCA, certificate, key, credentials, rejectedCredentials string
}

// writeNATSFixtureFile stores generated fixture bytes privately and fails with
// fixed diagnostics, never including certificate, JWT or key contents.
// writeNATSFixtureFile сохраняет одноразовые байты приватно и выдаёт лишь
// фиксированную ошибку без содержимого сертификата, JWT либо ключа.
func writeNATSFixtureFile(t *testing.T, directory, name string, value []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal("write disposable NATS fixture material")
	}
	return path
}

// createNATSFixtureMaterial uses standard X.509 and official NATS claim signing
// APIs to generate a one-hour operator/account/user chain and mutual TLS files.
// Nothing is projected from the real runtime or persisted outside the fixture.
// createNATSFixtureMaterial создаёт часовую operator/account/user цепочку и
// mutual TLS штатными X.509 и NATS APIs. Реальный runtime не читается; данные
// не сохраняются за пределами одноразового каталога.
func createNATSFixtureMaterial(t *testing.T, directory string) natsFixtureMaterial {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate fixture CA")
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "disposable API NATS CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("sign fixture CA")
	}
	foreignKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate unrelated fixture CA")
	}
	foreignCA := *ca
	foreignCA.SerialNumber = big.NewInt(3)
	foreignDER, err := x509.CreateCertificate(rand.Reader, &foreignCA, &foreignCA, &foreignKey.PublicKey, foreignKey)
	if err != nil {
		t.Fatal("sign unrelated fixture CA")
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal("generate fixture TLS key")
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "nats.fixture.invalid"}, DNSNames: []string{"nats.fixture.invalid"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal("sign fixture TLS certificate")
	}
	leafPrivate, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatal("encode fixture TLS key")
	}
	material := natsFixtureMaterial{
		ca:          writeNATSFixtureFile(t, directory, "ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
		wrongCA:     writeNATSFixtureFile(t, directory, "unrelated-ca.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: foreignDER})),
		certificate: writeNATSFixtureFile(t, directory, "client-server.pem", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})),
		key:         writeNATSFixtureFile(t, directory, "client-server.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafPrivate})),
	}
	operatorKey, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal("generate fixture operator")
	}
	defer operatorKey.Wipe()
	operatorPublic, err := operatorKey.PublicKey()
	if err != nil {
		t.Fatal("read fixture operator public key")
	}
	operatorClaims := jwt.NewOperatorClaims(operatorPublic)
	operatorClaims.Expires = now.Add(time.Hour).Unix()
	operatorJWT, err := operatorClaims.Encode(operatorKey)
	if err != nil {
		t.Fatal("sign fixture operator")
	}
	accountKey, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal("generate fixture account")
	}
	defer accountKey.Wipe()
	accountPublic, err := accountKey.PublicKey()
	if err != nil {
		t.Fatal("read fixture account public key")
	}
	accountClaims := jwt.NewAccountClaims(accountPublic)
	accountClaims.Expires = now.Add(time.Hour).Unix()
	accountJWT, err := accountClaims.Encode(operatorKey)
	if err != nil {
		t.Fatal("sign fixture account")
	}
	for _, rejected := range []bool{false, true} {
		userKey, err := nkeys.CreateUser()
		if err != nil {
			t.Fatal("generate fixture user")
		}
		defer userKey.Wipe()
		userPublic, err := userKey.PublicKey()
		if err != nil {
			t.Fatal("read fixture user public key")
		}
		claims := jwt.NewUserClaims(userPublic)
		claims.Expires = now.Add(time.Hour).Unix()
		signer := accountKey
		if rejected {
			signer, err = nkeys.CreateAccount()
			if err != nil {
				t.Fatal("generate untrusted fixture account")
			}
			defer signer.Wipe()
		}
		userJWT, err := claims.Encode(signer)
		if err != nil {
			t.Fatal("sign fixture user")
		}
		seed, err := userKey.Seed()
		if err != nil {
			t.Fatal("read fixture user seed")
		}
		credentials, err := jwt.FormatUserConfig(userJWT, seed)
		clear(seed)
		if err != nil {
			t.Fatal("encode fixture user credentials")
		}
		path := writeNATSFixtureFile(t, directory, fmt.Sprintf("user-%t.creds", rejected), credentials)
		clear(credentials)
		if rejected {
			material.rejectedCredentials = path
		} else {
			material.credentials = path
		}
	}
	// Only this disposable broker sees these generated claims and TLS paths.
	// Лишь одноразовый broker видит сгенерированные claims и TLS-пути.
	configuration := fmt.Sprintf("port: 4222\noperator: %q\nresolver: MEMORY\nresolver_preload: { %s: %q }\ntls { cert_file: %q, key_file: %q, ca_file: %q, verify: true, timeout: 2 }\n", operatorJWT, accountPublic, accountJWT, "/fixture/client-server.pem", "/fixture/client-server.key", "/fixture/ca.pem")
	writeNATSFixtureFile(t, directory, "nats.conf", []byte(configuration))
	return material
}

// natsFixtureDocker runs only the component's fixed local Docker operations with
// bounded caller context and keeps raw daemon output away from test diagnostics.
// natsFixtureDocker выполняет лишь фиксированные локальные Docker-операции
// component с ограниченным контекстом, не выводя сырой ответ daemon в диагностику.
func natsFixtureDocker(t *testing.T, ctx context.Context, args ...string) []byte {
	t.Helper()
	output, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		t.Fatal("disposable NATS Docker operation failed")
	}
	return output
}

// TestNATSStartupDiagnosticComponent exercises the unchanged API connect helper
// against a real pinned NATS server with generated JWT and mutual TLS. Ordinary
// Go tests skip this external fixture; the public repository script owns budget,
// exact container cleanup and private failure evidence, without cluster access.
// TestNATSStartupDiagnosticComponent проверяет прежний connect helper API на
// настоящем закреплённом NATS с одноразовыми JWT и mutual TLS. Обычные Go-тесты
// пропускают внешний fixture; публичный script владеет бюджетом, exact cleanup
// контейнера и приватными данными отказа, без доступа к кластеру.
func TestNATSStartupDiagnosticComponent(t *testing.T) {
	directory, image, name := os.Getenv("KODEX_API_NATS_TEST_DIRECTORY"), os.Getenv("KODEX_API_NATS_TEST_IMAGE"), os.Getenv("KODEX_API_NATS_TEST_CONTAINER")
	if directory == "" && image == "" && name == "" {
		t.Skip("run make test-control-api-nats-startup")
	}
	canonical, err := os.ReadFile("../../../../../deploy/k8s/base/platform-state/nats.yaml")
	if err != nil || image != natsDiagnosticFixtureImage || !strings.Contains(string(canonical), "image: "+image) || !regexp.MustCompile(`^kodex-api-nats-[A-Za-z0-9.-]+$`).MatchString(name) {
		t.Fatal("component image or identity does not match canonical fixture")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("component directory is not private")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	material := createNATSFixtureMaterial(t, directory)
	id := strings.TrimSpace(string(natsFixtureDocker(t, ctx, "create", "--pull", "never", "--name", name, "--label", "kodex.qa.fixture=control-api-nats-startup", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "--cpus", "1", "--memory", "128m", "--pids-limit", "64", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--publish", "127.0.0.1::4222", "--mount", "type=bind,src="+directory+",dst=/fixture,readonly", image, "-c", "/fixture/nats.conf")))
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(id) {
		t.Fatal("component container ID is invalid")
	}
	state, _ := json.Marshal(map[string]string{"id": id, "name": name})
	writeNATSFixtureFile(t, directory, "container.json", state)
	natsFixtureDocker(t, ctx, "start", id)
	address := strings.TrimSpace(string(natsFixtureDocker(t, ctx, "port", id, "4222/tcp")))
	if !regexp.MustCompile(`^127\.0\.0\.1:[0-9]+$`).MatchString(address) {
		t.Fatal("component endpoint is not disposable loopback")
	}
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			ready = true
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("component startup budget exhausted")
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("disposable NATS listener did not start")
	}
	config := Config{NATSURL: "tls://" + address, NATSTLSServerName: "nats.fixture.invalid", NATSCAFile: material.ca, NATSCertificateFile: material.certificate, NATSPrivateKeyFile: material.key, NATSCredentialsFile: material.credentials}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, scenario := range []string{"valid JWT and mTLS", "wrong CA", "wrong SNI", "rejected JWT", "missing credentials", "malformed credentials", "malformed CA"} {
		t.Run(scenario, func(t *testing.T) {
			input := config
			wantClass := ""
			switch scenario {
			case "wrong CA":
				input.NATSCAFile = material.wrongCA
				wantClass = "tls_verification"
			case "wrong SNI":
				input.NATSTLSServerName = "foreign.fixture.invalid"
				wantClass = "tls_verification"
			case "rejected JWT":
				input.NATSCredentialsFile = material.rejectedCredentials
				wantClass = "authorization"
			case "missing credentials":
				input.NATSCredentialsFile = filepath.Join(directory, "missing.creds")
				wantClass = "filesystem"
			case "malformed credentials":
				input.NATSCredentialsFile = writeNATSFixtureFile(t, directory, "malformed.creds", []byte("PRIVATE_JWT"))
				wantClass = "credentials_configuration"
			case "malformed CA":
				input.NATSCAFile = writeNATSFixtureFile(t, directory, "malformed-ca.pem", []byte("PRIVATE_CA"))
				wantClass = "tls_configuration"
			}
			connection, err := connectNATS(input, logger)
			if wantClass == "" {
				if err != nil || connection == nil {
					t.Fatal("actual JWT/mTLS NATS connect did not pass")
				}
				connection.Close()
				if !connection.IsClosed() {
					t.Fatal("native connection did not close")
				}
				return
			}
			if connection != nil {
				connection.Close()
				t.Fatal("negative native case returned a connection")
			}
			var failure *natsStartupError
			if !errors.As(err, &failure) {
				t.Fatal("actual native failure lost diagnostic marker")
			}
			class := natsFailureClass(failure.stage, failure.cause)
			t.Logf("native diagnostic stage=%q error_class=%s", natsStageLabel(failure.stage), class)
			if class != wantClass {
				t.Fatalf("actual closed class=%s expected=%s; do not infer cause from raw text", class, wantClass)
			}
			for _, forbidden := range []string{directory, address, "PRIVATE_", "tls://", "BEGIN NATS"} {
				if strings.Contains(err.Error(), forbidden) {
					t.Fatal("native diagnostic exposed private fixture metadata")
				}
			}
		})
	}
}
