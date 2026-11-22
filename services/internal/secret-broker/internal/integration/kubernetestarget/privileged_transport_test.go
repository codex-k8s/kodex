package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// privilegedPinFixture binds only the disposable local TLS server to an operator-style node pin.
// privilegedPinFixture связывает только disposable local TLS server с operator-style node pin.
func privilegedPinFixture(t *testing.T, credential Credential, now time.Time) PrivilegedTransportPin {
	t.Helper()
	block, _ := pem.Decode(credential.ca)
	if block == nil {
		t.Fatal("decode disposable server trust")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal("parse disposable server trust")
	}
	keyHash := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)
	return PrivilegedTransportPin{
		Ref: "synthetic-node-pin", TargetID: credential.target.ID, TargetRevision: credential.target.Revision,
		NodeName: "synthetic-node", NodeUID: "9a99d4f2-836d-4efb-868e-5de8792ade07", Generation: 3,
		ServerURL: credential.target.ServerURL, TLSServerName: credential.target.TLSServerName,
		CertificateAuthorityPEM: append([]byte(nil), credential.ca...), CertificateAuthorityHash: credential.target.CertificateAuthorityHash,
		ServerPublicKeyHash: hex.EncodeToString(keyHash[:]), AddressPins: []string{"127.0.0.1"},
		NotBefore: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
		Protocol: PrivilegedWebSocketV5,
	}
}

// TestPrivilegedClientPreservesSelectedIdentity proves the private direct transport keeps exact TLS
// key/address pins and the same personal token or certificate. No actual stream or live node is used.
// TestPrivilegedClientPreservesSelectedIdentity доказывает exact TLS key/address pins и ту же
// personal token/certificate в private direct transport. Actual stream либо live node не используются.
func TestPrivilegedClientPreservesSelectedIdentity(t *testing.T) {
	for _, certificateIdentity := range []bool{false, true} {
		t.Run(map[bool]string{false: "token", true: "certificate"}[certificateIdentity], func(t *testing.T) {
			credential, calls := transportFixture(t, certificateIdentity)
			now := time.Now()
			pin := privilegedPinFixture(t, credential, now)
			client, closeIdle, err := credential.newPrivilegedClient(pin, now, 3*time.Second)
			if err != nil {
				t.Fatal("build private pinned node transport")
			}
			defer closeIdle()
			pin.AddressPins[0] = "192.0.2.1"
			pin.CertificateAuthorityPEM[0] ^= 1
			response, err := client.Get(credential.target.ServerURL + "/api")
			if err != nil {
				t.Fatal("pinned node request failed or caller mutation changed transport pins")
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "synthetic Kubernetes response" || calls.Load() != 1 {
				t.Fatal("node transport changed selected identity or response")
			}
		})
	}
}

// TestPrivilegedPinRejectsForeignExpiredAndMissingBindings rejects unavailable capabilities before
// network traffic; a NodeUID read or caller flag cannot synthesize a missing operator pin.
// TestPrivilegedPinRejectsForeignExpiredAndMissingBindings отклоняет unavailable capabilities до
// network traffic; NodeUID read либо caller flag не создаёт отсутствующий operator pin.
func TestPrivilegedPinRejectsForeignExpiredAndMissingBindings(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	for _, mutate := range []func(*PrivilegedTransportPin){
		func(pin *PrivilegedTransportPin) { pin.Ref = "" },
		func(pin *PrivilegedTransportPin) { pin.TargetID = "foreign" },
		func(pin *PrivilegedTransportPin) { pin.TargetRevision++ },
		func(pin *PrivilegedTransportPin) { pin.Generation = 0 },
		func(pin *PrivilegedTransportPin) { pin.Protocol = "AUTO_FALLBACK" },
		func(pin *PrivilegedTransportPin) { pin.NodeUID = "" },
		func(pin *PrivilegedTransportPin) { pin.NodeUID = strings.ToUpper(pin.NodeUID) },
		func(pin *PrivilegedTransportPin) { pin.NodeName = "node/proxy/foreign" },
		func(pin *PrivilegedTransportPin) { pin.ServerPublicKeyHash = "" },
		func(pin *PrivilegedTransportPin) { pin.ServerPublicKeyHash = strings.Repeat("A", 64) },
		func(pin *PrivilegedTransportPin) { pin.CertificateAuthorityHash = strings.Repeat("0", 64) },
		func(pin *PrivilegedTransportPin) { pin.CertificateAuthorityPEM = nil },
		func(pin *PrivilegedTransportPin) { pin.AddressPins = nil },
		func(pin *PrivilegedTransportPin) { pin.AddressPins = []string{"foreign.synthetic.invalid"} },
		func(pin *PrivilegedTransportPin) { pin.ServerURL = "http://api.synthetic.invalid:10250" },
		func(pin *PrivilegedTransportPin) { pin.ServerURL += "/arbitrary/path" },
		func(pin *PrivilegedTransportPin) { pin.TLSServerName = "" },
		func(pin *PrivilegedTransportPin) { pin.ExpiresAt = now },
		func(pin *PrivilegedTransportPin) { pin.NotBefore = now.Add(time.Second) },
		func(pin *PrivilegedTransportPin) { pin.ExpiresAt = now.Add(48 * time.Hour) },
	} {
		pin := privilegedPinFixture(t, credential, now)
		mutate(&pin)
		if client, closeIdle, err := credential.newPrivilegedClient(pin, now, time.Second); err == nil || client != nil || closeIdle != nil {
			t.Fatal("invalid privileged transport pin was accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unproven operator binding reached the node API")
	}
}

// TestPrivilegedClientRejectsChangedServingKey proves a valid CA and hostname cannot bypass the
// operator's selected serving key; the failed TLS handshake never reaches an HTTP handler.
// TestPrivilegedClientRejectsChangedServingKey доказывает, что valid CA/hostname не обходят
// выбранный operator serving key; failed TLS handshake не достигает HTTP handler.
func TestPrivilegedClientRejectsChangedServingKey(t *testing.T) {
	credential, calls := transportFixture(t, false)
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.ServerPublicKeyHash = strings.Repeat("0", 64)
	client, closeIdle, err := credential.newPrivilegedClient(pin, now, 3*time.Second)
	if err != nil {
		t.Fatal("build mismatched-key transport fixture")
	}
	defer closeIdle()
	if response, err := client.Get(credential.target.ServerURL + "/api"); err == nil || response != nil || calls.Load() != 0 {
		t.Fatal("changed node serving key bypassed its operator pin")
	}
}

// TestPrivilegedClientExpiresInFlightAndBeforeReuse proves a pin cannot authorize a long response
// past expiry or a later request on an already-created client. No background request outlives join.
// TestPrivilegedClientExpiresInFlightAndBeforeReuse доказывает, что pin не допускает long response
// после expiry либо later request прежним client. Background request завершается до join.
func TestPrivilegedClientExpiresInFlightAndBeforeReuse(t *testing.T) {
	joined := make(chan struct{})
	credential, calls := transportFixtureWithHandler(t, false, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "first")
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
		close(joined)
	}))
	now := time.Now()
	pin := privilegedPinFixture(t, credential, now)
	pin.ExpiresAt = now.Add(time.Second)
	client, closeIdle, err := credential.newPrivilegedClient(pin, now, 3*time.Second)
	if err != nil {
		t.Fatal("build expiring node transport")
	}
	defer closeIdle()
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, pin.ServerURL+"/api", nil)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("expiring transport failed before response headers")
	}
	_, err = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err == nil {
		t.Fatal("privileged response outlived its operator pin")
	}
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("expired request did not join its target handler")
	}
	if response, err = client.Get(pin.ServerURL + "/api"); err == nil || response != nil || calls.Load() != 1 {
		t.Fatal("expired pin was reused by an existing client")
	}
}
