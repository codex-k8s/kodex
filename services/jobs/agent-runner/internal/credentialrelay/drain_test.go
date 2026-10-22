package credentialrelay

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// drainCommitter exposes entry and release channels for a single synthetic callback, making drain timing observable without real credential persistence.
// drainCommitter предоставляет каналы входа и освобождения одного синтетического callback для наблюдения drain без сохранения реальных реквизитов.
type drainCommitter struct {
	called  chan struct{}
	release chan struct{}
}

// CommitProviderCredentialRefresh signals entry, waits for test release or cancellation and returns the resulting context error; it does not persist authentication.
// CommitProviderCredentialRefresh сигнализирует вход, ждёт освобождения тестом или отмены и возвращает ошибку контекста; авторизацию не сохраняет.
func (committer *drainCommitter) CommitProviderCredentialRefresh(ctx context.Context, _ model.Input, _ runtimecontract.RunnerProviderCredentialRefreshRequest) error {
	close(committer.called)
	select {
	case <-committer.release:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestRelayDrainsBoundRefreshArrivingAfterCancellation cancels the listener lifecycle before sending a valid framed refresh and requires callback completion, a positive acknowledgement and bounded joining. UID enforcement is tested separately.
// TestRelayDrainsBoundRefreshArrivingAfterCancellation отменяет lifecycle listener до передачи корректного refresh и требует завершение callback, положительное подтверждение и ограниченное ожидание выхода. Проверка UID тестируется отдельно.
func TestRelayDrainsBoundRefreshArrivingAfterCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	input, payload := validRelayFixture()
	committer := &drainCommitter{called: make(chan struct{}), release: make(chan struct{})}
	lifecycle, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- serveListener(lifecycle, listener, 200*time.Millisecond, func(ctx context.Context, connection net.Conn) error {
			// UID-предикат проверен отдельно; здесь исполняется настоящий bounded payload/commit/ack.
			return serveBoundConnection(ctx, input, connection, committer)
		})
	}()
	cancel()
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(time.Second))
	if err := json.NewEncoder(connection).Encode(payload); err != nil {
		t.Fatal(err)
	}
	if err := connection.(*net.UnixConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-committer.called:
	case <-time.After(time.Second):
		t.Fatal("refresh was not admitted during drain")
	}
	close(committer.release)
	raw, err := io.ReadAll(connection)
	if err != nil || string(raw) != "{\"ok\":true}\n" {
		t.Fatalf("drain acknowledgement missing: err=%v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("relay did not join after drain")
	}
}

// TestRelayDrainClosesSlowPeerAndJoins sends an unfinished JSON body, cancels lifecycle and requires bounded shutdown without invoking the credential committer.
// TestRelayDrainClosesSlowPeerAndJoins передаёт незаконченный JSON, отменяет lifecycle и требует ограниченное завершение без вызова committer реквизитов.
func TestRelayDrainClosesSlowPeerAndJoins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	input, _ := validRelayFixture()
	committer := &drainCommitter{called: make(chan struct{}), release: make(chan struct{})}
	lifecycle, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- serveListener(lifecycle, listener, 30*time.Millisecond, func(ctx context.Context, connection net.Conn) error {
			return serveBoundConnection(ctx, input, connection, committer)
		})
	}()
	connection, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = connection.Write([]byte("{"))
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow peer prevented bounded shutdown")
	}
	select {
	case <-committer.called:
		t.Fatal("incomplete input reached credential callback")
	default:
	}
}
