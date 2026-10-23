package providercredential

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// observedAppServerWriter captures independent copies of synthetic outbound stdin messages on a channel
// so tests can assert protocol ordering without a process.
// observedAppServerWriter передаёт независимые копии synthetic исходящих stdin сообщений в channel для
// проверки protocol порядка без процесса.
type observedAppServerWriter struct {
	writes chan []byte
}

// Write copies the request bytes before channel delivery so production buffer clearing cannot erase the
// fixture's observed request; delivery is intentionally synchronous.
// Write копирует request байты до передачи в channel, чтобы production очистка буфера не стирала
// наблюдённый fixture запрос; передача намеренно синхронная.
func (writer *observedAppServerWriter) Write(value []byte) (int, error) {
	writer.writes <- append([]byte(nil), value...)
	return len(value), nil
}

// Close is a no-op fixture implementation because this writer owns no file descriptor or external
// process.
// Close ничего не делает в fixture, поскольку writer не владеет file descriptor и внешним процессом.
func (*observedAppServerWriter) Close() error { return nil }

// TestDeviceSessionWaitsForAccountUpdateBeforeReadingAccount injects synthetic login-completed and
// account-updated events separately, verifies no early account/read and checks the later masked
// account/auth-file result.
// TestDeviceSessionWaitsForAccountUpdateBeforeReadingAccount отдельно внедряет synthetic login-completed
// и account-updated events, проверяет отсутствие преждевременного account/read и последующий masked
// account/auth-file результат.
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

// TestMaskedAccountAllowsChatGPTWithoutEmail accepts provider metadata with a null email and requires
// the generic ChatGPT label, not a fabricated address.
// TestMaskedAccountAllowsChatGPTWithoutEmail допускает provider metadata с null email и требует общую
// ChatGPT метку, не выдуманный адрес.
func TestMaskedAccountAllowsChatGPTWithoutEmail(t *testing.T) {
	t.Parallel()
	masked, err := maskedAccount([]byte(`{"account":{"type":"chatgpt","email":null}}`))
	if err != nil || masked != "ChatGPT" {
		t.Fatalf("email-less ChatGPT account rejected: masked=%q err=%v", masked, err)
	}
}

// TestMaskedAccountMasksEmailAndRejectsInvalidMetadata verifies masking on a synthetic address and
// rejects another account type, missing separator and an empty email.
// TestMaskedAccountMasksEmailAndRejectsInvalidMetadata проверяет маскирование synthetic адреса и
// отклоняет другой account type, отсутствие разделителя и пустой email.
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

// TestReadAuthJSONRejectsUnsafeFileIdentity accepts one private regular synthetic JSON file and rejects
// symlink, hardlink, FIFO and public mode through real disposable filesystem operations.
// TestReadAuthJSONRejectsUnsafeFileIdentity допускает один private regular synthetic JSON файл и
// отклоняет symlink, hardlink, FIFO и public mode через реальные disposable filesystem операции.
func TestReadAuthJSONRejectsUnsafeFileIdentity(t *testing.T) {
	for _, mode := range []string{"regular", "symlink", "hardlink", "fifo", "public"} {
		t.Run(mode, func(t *testing.T) {
			home := t.TempDir()
			path := filepath.Join(home, "auth.json")
			if mode == "fifo" {
				createCatalogFIFO(t, path)
			} else {
				if err := os.WriteFile(path, []byte(`{"auth_mode":"chatgpt"}`), 0o600); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "symlink":
					if err := os.Rename(path, filepath.Join(home, "target")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink("target", path); err != nil {
						t.Fatal(err)
					}
				case "hardlink":
					if err := os.Link(path, filepath.Join(home, "alias")); err != nil {
						t.Fatal(err)
					}
				case "public":
					if err := os.Chmod(path, 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			raw, err := readAuthJSON(home)
			defer clear(raw)
			if mode == "regular" && (err != nil || len(raw) == 0) || mode != "regular" && (err == nil || len(raw) != 0) {
				t.Fatal("unsafe auth file accepted or regular file rejected")
			}
		})
	}
}
