package snapshot

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestDeliveryВыполняетResourceVersionCASИReadback(t *testing.T) {
	t.Parallel()
	delivery := newTestDelivery(t)
	server := &snapshotRoundTripper{
		envelope:  testSecretEnvelope("10", 0, "", ""),
		conflicts: 1,
	}
	delivery.client = &http.Client{Transport: server}
	expected := model.AuthoritySnapshotPublication{
		SourceRevision: 1, SourceDigestSHA256: strings.Repeat("a", 64),
		SignerGeneration: 1, SnapshotCompactJWS: "header.payload.signature",
	}
	served, err := delivery.Publish(context.Background(), expected)
	if err != nil {
		t.Fatalf("publish snapshot: %v", err)
	}
	if served.SnapshotResourceVersion != "11" || server.putCount != 2 {
		t.Fatalf("CAS retry/readback mismatch: %#v put=%d", served, server.putCount)
	}
	readback, err := delivery.Read(context.Background())
	if err != nil {
		t.Fatalf("read served snapshot: %v", err)
	}
	if readback.SourceRevision != expected.SourceRevision ||
		readback.SourceDigestSHA256 != expected.SourceDigestSHA256 ||
		readback.SnapshotCompactJWS != expected.SnapshotCompactJWS ||
		readback.SnapshotResourceVersion != "11" {
		t.Fatalf("served snapshot mismatch: %#v", readback)
	}
}

func TestDeliveryОтклоняетRollbackИSameRevisionMutation(t *testing.T) {
	t.Parallel()
	for name, publication := range map[string]model.AuthoritySnapshotPublication{
		"rollback": {
			SourceRevision: 1, SourceDigestSHA256: strings.Repeat("a", 64),
			SignerGeneration: 1, SnapshotCompactJWS: "header.payload.signature",
		},
		"same-revision-mutation": {
			SourceRevision: 2, SourceDigestSHA256: strings.Repeat("b", 64),
			SignerGeneration: 1, SnapshotCompactJWS: "header.payload.signature",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			delivery := newTestDelivery(t)
			server := &snapshotRoundTripper{
				envelope: testSecretEnvelope(
					"10",
					2,
					strings.Repeat("a", 64),
					"served.snapshot.signature",
				),
			}
			delivery.client = &http.Client{Transport: server}
			if _, err := delivery.Publish(
				context.Background(),
				publication,
			); err == nil {
				t.Fatal("rollback or same-revision mutation was accepted")
			}
			if server.putCount != 0 {
				t.Fatal("rejected publication reached the CAS write")
			}
		})
	}
}

func newTestDelivery(t *testing.T) *Delivery {
	t.Helper()
	directory := t.TempDir()
	caFile := filepath.Join(directory, "ca.pem")
	tokenFile := filepath.Join(directory, "token")
	writeTestCA(t, caFile)
	if err := os.WriteFile(tokenFile, []byte("bounded-test-token"), 0o400); err != nil {
		t.Fatal(err)
	}
	delivery, err := New(Config{
		Address:       "https://kubernetes.default.svc:443",
		TLSServerName: "kubernetes.default.svc",
		CAFile:        caFile, TokenFile: tokenFile,
		Namespace:  "mattercodex-system",
		SecretName: "internal-rpc-authority-snapshot",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return delivery
}

func writeTestCA(t *testing.T, path string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "authority snapshot test CA"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	pemValue := "-----BEGIN CERTIFICATE-----\n" +
		base64.StdEncoding.EncodeToString(raw) +
		"\n-----END CERTIFICATE-----\n"
	if err := os.WriteFile(path, []byte(pemValue), 0o440); err != nil {
		t.Fatal(err)
	}
}

func testSecretEnvelope(
	resourceVersion string,
	revision uint64,
	digest string,
	compact string,
) secretEnvelope {
	value := secretEnvelope{
		APIVersion: "v1", Kind: "Secret", Type: "Opaque",
		Data: map[string]string{
			"snapshot.jws": base64.StdEncoding.EncodeToString([]byte(compact)),
		},
	}
	value.Metadata.Name = "internal-rpc-authority-snapshot"
	value.Metadata.Namespace = "mattercodex-system"
	value.Metadata.UID = "00000000-0000-4000-8000-000000000001"
	value.Metadata.ResourceVersion = resourceVersion
	value.Metadata.Annotations = map[string]string{
		"mattercodex.dev/source-revision":      "0",
		"mattercodex.dev/source-digest-sha256": digest,
		"mattercodex.dev/signer-generation":    "0",
	}
	if revision != 0 {
		value.Metadata.Annotations["mattercodex.dev/source-revision"] =
			big.NewInt(int64(revision)).String()
		value.Metadata.Annotations["mattercodex.dev/signer-generation"] = "1"
	}
	return value
}

type snapshotRoundTripper struct {
	mu        sync.Mutex
	envelope  secretEnvelope
	conflicts int
	putCount  int
}

func (server *snapshotRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if request.Header.Get("Authorization") != "Bearer bounded-test-token" ||
		request.URL.String() !=
			"https://kubernetes.default.svc:443/api/v1/namespaces/"+
				"mattercodex-system/secrets/internal-rpc-authority-snapshot" {
		return testHTTPResponse(http.StatusForbidden, nil), nil
	}
	switch request.Method {
	case http.MethodGet:
		raw, _ := json.Marshal(server.envelope)
		return testHTTPResponse(http.StatusOK, raw), nil
	case http.MethodPut:
		server.putCount++
		if server.conflicts > 0 {
			server.conflicts--
			return testHTTPResponse(http.StatusConflict, []byte(`{}`)), nil
		}
		raw, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		var next secretEnvelope
		if err := json.Unmarshal(raw, &next); err != nil {
			return nil, err
		}
		if next.Metadata.ResourceVersion != server.envelope.Metadata.ResourceVersion {
			return testHTTPResponse(http.StatusConflict, []byte(`{}`)), nil
		}
		next.Metadata.ResourceVersion = "11"
		server.envelope = next
		responseRaw, _ := json.Marshal(next)
		return testHTTPResponse(http.StatusOK, responseRaw), nil
	default:
		return testHTTPResponse(http.StatusMethodNotAllowed, nil), nil
	}
}

func testHTTPResponse(status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(string(body))),
		Header:     make(http.Header),
	}
}
