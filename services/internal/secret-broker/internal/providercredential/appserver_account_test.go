package providercredential

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type observedAppServerWriter struct {
	writes chan []byte
}

func (writer *observedAppServerWriter) Write(value []byte) (int, error) {
	writer.writes <- append([]byte(nil), value...)
	return len(value), nil
}

func (*observedAppServerWriter) Close() error { return nil }

func TestDeviceSessionWaitsForAccountUpdateBeforeReadingAccount(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	authJSON := []byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"synthetic-only"}}`)
	if err := os.WriteFile(filepath.Join(home, "auth.json"), authJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	messages := make(chan streamEvent, 4)
	writes := make(chan []byte, 1)
	session := &deviceSession{
		server:  &appServer{stdin: &observedAppServerWriter{writes: writes}, messages: messages},
		home:    home,
		loginID: "synthetic-login",
	}
	result := make(chan struct {
		auth   []byte
		masked string
		err    error
	}, 1)
	go func() {
		auth, masked, err := session.Wait(context.Background())
		result <- struct {
			auth   []byte
			masked string
			err    error
		}{auth: auth, masked: masked, err: err}
	}()
	messages <- streamEvent{message: wireMessage{Method: "account/login/completed", Params: json.RawMessage(`{"loginId":"synthetic-login","success":true,"error":null}`)}}
	select {
	case request := <-writes:
		t.Fatalf("account/read sent before account/updated: %s", request)
	case <-time.After(25 * time.Millisecond):
	}
	messages <- streamEvent{message: wireMessage{Method: "account/updated", Params: json.RawMessage(`{"authMode":"chatgpt","planType":"plus"}`)}}
	select {
	case request := <-writes:
		var parsed struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(request, &parsed) != nil || parsed.ID != 1 || parsed.Method != "account/read" {
			t.Fatalf("unexpected account request: %s", request)
		}
	case <-time.After(time.Second):
		t.Fatal("account/read was not sent after account/updated")
	}
	messages <- streamEvent{message: wireMessage{ID: json.RawMessage(`1`), Result: json.RawMessage(`{"account":{"type":"chatgpt","email":null}}`)}}
	select {
	case completed := <-result:
		if completed.err != nil || completed.masked != "ChatGPT" || string(completed.auth) != string(authJSON) {
			t.Fatalf("unexpected materialization: masked=%q auth=%q err=%v", completed.masked, completed.auth, completed.err)
		}
	case <-time.After(time.Second):
		t.Fatal("device authorization did not complete")
	}
}

func TestMaskedAccountAllowsChatGPTWithoutEmail(t *testing.T) {
	t.Parallel()
	masked, err := maskedAccount([]byte(`{"account":{"type":"chatgpt","email":null}}`))
	if err != nil || masked != "ChatGPT" {
		t.Fatalf("email-less ChatGPT account rejected: masked=%q err=%v", masked, err)
	}
}

func TestMaskedAccountMasksEmailAndRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()
	masked, err := maskedAccount([]byte(`{"account":{"type":"chatgpt","email":"owner@example.test"}}`))
	if err != nil || masked != "ow***@example.test" {
		t.Fatalf("ChatGPT account was not masked: masked=%q err=%v", masked, err)
	}
	for _, raw := range [][]byte{
		[]byte(`{"account":{"type":"apiKey","email":"owner@example.test"}}`),
		[]byte(`{"account":{"type":"chatgpt","email":"invalid"}}`),
		[]byte(`{"account":{"type":"chatgpt","email":""}}`),
	} {
		if _, err := maskedAccount(raw); err == nil {
			t.Fatalf("invalid account metadata accepted: %s", raw)
		}
	}
}
