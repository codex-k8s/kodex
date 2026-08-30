package credentialrelay

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestPeerUIDReadsUnixPeerAndAuthorizationAllowsOnlyProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()
	uid, err := peerUID(server)
	if err != nil {
		t.Fatalf("peerUID() error = %v", err)
	}
	if uid != uint32(os.Geteuid()) {
		t.Fatalf("peerUID() = %d, want %d", uid, os.Geteuid())
	}
	if !authorizedProviderUID(providerUID) || authorizedProviderUID(10001) || authorizedProviderUID(relayUID) {
		t.Fatal("provider relay accepted a non-provider peer UID")
	}
	if !authorizedRelayUID(relayUID) || authorizedRelayUID(providerUID) || authorizedRelayUID(10001) {
		t.Fatal("provider client accepted a non-relay peer UID")
	}
}

func TestWriteFullHandlesPartialWrites(t *testing.T) {
	want := bytes.Repeat([]byte("credential-relay-payload"), 1024)
	writer := &partialWriter{maximum: 7}
	if err := writeFull(writer, want); err != nil {
		t.Fatalf("writeFull() error = %v", err)
	}
	if !bytes.Equal(writer.buffer.Bytes(), want) || writer.calls < 2 {
		t.Fatalf("writeFull() calls = %d, bytes = %d", writer.calls, writer.buffer.Len())
	}
}

func TestWriteFullRejectsZeroLengthWrite(t *testing.T) {
	if err := writeFull(zeroWriter{}, []byte("payload")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("writeFull() error = %v", err)
	}
}

func TestDecodeRequestRejectsPayloadOutsideBound(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), maximumRequestBytes+1)
	if _, err := decodeRequest(bytes.NewReader(oversized)); err == nil {
		t.Fatal("oversized provider credential relay request was accepted")
	}
}

func TestDecodeRequestRejectsUnknownFields(t *testing.T) {
	raw := []byte(`{"lease_ref":"lease_abcdefgh","refresh":{},"authentication":"forbidden"}`)
	if _, err := decodeRequest(bytes.NewReader(raw)); err == nil {
		t.Fatal("provider credential relay request with unknown fields was accepted")
	}
}

func TestValidateRequestRequiresExactExecutionBinding(t *testing.T) {
	input, payload := validRelayFixture()
	if err := validateRequest(input, payload); err != nil {
		t.Fatalf("validateRequest() error = %v", err)
	}
	for name, mutate := range map[string]func(*request){
		"lease":               func(value *request) { value.LeaseRef = "lease_other123" },
		"runtime revision":    func(value *request) { value.Refresh.RuntimeRevisionDigest = strings.Repeat("d", 64) },
		"credential revision": func(value *request) { value.Refresh.PreviousCredentialRevisionRef = "pcr_other123" },
		"credential digest":   func(value *request) { value.Refresh.PreviousContentSHA256 = strings.Repeat("e", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := payload
			changed.Refresh.Authentication = append([]byte(nil), payload.Refresh.Authentication...)
			mutate(&changed)
			if err := validateRequest(input, changed); err == nil {
				t.Fatal("mismatched provider credential relay binding was accepted")
			}
		})
	}
}

func TestRequestEncodingStaysInsideRelayBound(t *testing.T) {
	_, payload := validRelayFixture()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= maximumRequestBytes {
		t.Fatalf("valid request size = %d", len(raw))
	}
}

func validRelayFixture() (model.Input, request) {
	input := model.Input{
		LeaseRef:                 "lease_abcdefgh",
		RuntimeRevisionDigest:    strings.Repeat("a", 64),
		ProviderCredentialRef:    "pcr_abcdefgh",
		ProviderCredentialSHA256: strings.Repeat("b", 64),
	}
	payload := request{LeaseRef: input.LeaseRef, Refresh: runtimecontract.RunnerProviderCredentialRefreshRequest{
		RuntimeRevisionDigest:         input.RuntimeRevisionDigest,
		PreviousCredentialRevisionRef: input.ProviderCredentialRef,
		PreviousContentSHA256:         input.ProviderCredentialSHA256,
		Authentication:                []byte(`{"auth_mode":"chatgpt","tokens":{"refresh_token":"rotated"}}`),
	}}
	return input, payload
}

type partialWriter struct {
	buffer  bytes.Buffer
	maximum int
	calls   int
}

func (writer *partialWriter) Write(payload []byte) (int, error) {
	writer.calls++
	if len(payload) > writer.maximum {
		payload = payload[:writer.maximum]
	}
	return writer.buffer.Write(payload)
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
