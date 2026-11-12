package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestProviderBrokerFailurePreservesSafeClass verifies each supported error class round-trips to
// its stable sentinel; an arbitrary transport error remains a nonnil generic provider failure.
// TestProviderBrokerFailurePreservesSafeClass проверяет восстановление устойчивого маркера каждого
// поддержанного класса; произвольная транспортная ошибка остаётся ненулевым отказом провайдера.
func TestProviderBrokerFailurePreservesSafeClass(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		failure providerBrokerFailure
		want    error
	}{
		{name: "authentication", err: ErrProviderAuthentication, failure: providerBrokerFailureAuthentication, want: ErrProviderAuthentication},
		{name: "authority", err: ErrAuthorityRequestUnsupported, failure: providerBrokerFailureAuthority, want: ErrAuthorityRequestUnsupported},
		{name: "mcp", err: ErrRequiredMCPUnavailable, failure: providerBrokerFailureMCP, want: ErrRequiredMCPUnavailable},
		{name: "configuration", err: ErrRuntimeProfile, failure: providerBrokerFailureConfiguration, want: ErrRuntimeProfile},
		{name: "provider", err: errors.New("transport"), failure: providerBrokerFailureProvider},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyProviderBrokerFailure(test.err); got != test.failure {
				t.Fatalf("classifyProviderBrokerFailure() = %q, want %q", got, test.failure)
			}
			err := providerBrokerError(test.failure)
			if test.want != nil && !errors.Is(err, test.want) {
				t.Fatalf("providerBrokerError() = %v, want %v", err, test.want)
			}
			if err == nil {
				t.Fatal("providerBrokerError() returned nil")
			}
		})
	}
}

// TestProviderSafeFailureClassDoesNotExposeDiagnostics distinguishes schema/auth classes without
// incorporating an arbitrary private diagnostic into the returned coarse category.
// TestProviderSafeFailureClassDoesNotExposeDiagnostics различает schema/auth классы без включения
// произвольной приватной диагностики в возвращаемую укрупнённую категорию.
func TestProviderSafeFailureClassDoesNotExposeDiagnostics(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "authentication", err: atProviderStage(providerStageAuthRead, ErrProviderAuthentication), want: "AUTHENTICATION"},
		{name: "external diagnostic", err: errors.New("fixture secret and account details must stay private"), want: "PROVIDER"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := providerSafeFailureClass(test.err); got != test.want {
				t.Fatalf("safe provider failure class = %q, want %q", got, test.want)
			}
		})
	}
}

// captureProviderDiagnostic redirects the standard logger to a private buffer and restores its previous
// writer at test cleanup. Tests using it must not run in parallel with other global logger mutations.
// captureProviderDiagnostic направляет стандартный logger в приватный буфер и восстанавливает прежний
// writer при очистке теста. Такие тесты не запускаются параллельно с другими изменениями общего logger.
func captureProviderDiagnostic(t *testing.T) *bytes.Buffer {
	t.Helper()
	var diagnostic bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&diagnostic)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &diagnostic
}

// TestProviderDiagnosticCaptureRestoresWriter checks that the local fixture actually captures a
// synthetic log message and restores the outer writer after the nested test's cleanup.
// TestProviderDiagnosticCaptureRestoresWriter проверяет захват синтетического сообщения локальной
// fixture и восстановление внешнего writer после очистки вложенного теста.
func TestProviderDiagnosticCaptureRestoresWriter(t *testing.T) {
	previous := log.Writer()
	t.Run("capture", func(t *testing.T) {
		diagnostic := captureProviderDiagnostic(t)
		log.Print("synthetic diagnostic capture")
		if !strings.Contains(diagnostic.String(), "synthetic diagnostic capture") {
			t.Fatal("diagnostic fixture did not capture the message")
		}
	})
	if log.Writer() != previous {
		t.Fatal("diagnostic fixture did not restore the logger")
	}
}

// TestProviderBrokerEarlyFailureLogsOnlySafeStage checks both captured log and actual terminal JSON
// omit the private cause while retaining the expected stage and coarse failed-envelope class.
// TestProviderBrokerEarlyFailureLogsOnlySafeStage проверяет отсутствие приватной причины в журнале
// и настоящем terminal JSON при сохранении ожидаемого этапа и укрупнённого класса отказа.
func TestProviderBrokerEarlyFailureLogsOnlySafeStage(t *testing.T) {
	var response bytes.Buffer
	diagnostic := captureProviderDiagnostic(t)

	secret := errors.New("sensitive-provider-detail")
	if err := writeProviderBrokerFailureAtStage(&response, providerStageHomePrepare, secret); err != nil {
		t.Fatalf("writeProviderBrokerFailureAtStage() error = %v", err)
	}
	if strings.Contains(diagnostic.String(), secret.Error()) || strings.Contains(response.String(), secret.Error()) {
		t.Fatal("provider failure exposed the original error")
	}
	if !strings.Contains(diagnostic.String(), "HOME_PREPARE") {
		t.Fatalf("safe diagnostic = %q", diagnostic.String())
	}
	var frame brokerFrame
	if err := json.Unmarshal(response.Bytes(), &frame); err != nil || frame.Version != providerBrokerVersion || frame.Kind != brokerFrameTerminal || frame.Terminal == nil || frame.Terminal.OK || frame.Terminal.Failure != providerBrokerFailureProvider {
		t.Fatalf("broker response = %#v, error = %v", frame, err)
	}
}

// providerTurnFixture writes synthetic auth beneath a private temporary home and returns its content
// pin with minimal execution metadata; it is not a fully admitted runtime input or a live account.
// providerTurnFixture записывает синтетический auth во временный приватный home и возвращает digest
// с минимальными данными исполнения; это не полностью допущенный runtime input и не живой аккаунт.
func providerTurnFixture(t *testing.T, authentication []byte) (model.Input, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, ".kodex", "state", "opencode")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	authPath := filepath.Join(home, "auth.json")
	if err := os.WriteFile(authPath, authentication, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(authentication)
	return model.Input{
		LeaseRef:                 "lease_abcdefgh",
		RuntimeRevisionDigest:    strings.Repeat("a", 64),
		ProviderCredentialRef:    "pcr_abcdefgh",
		ProviderCredentialSHA256: hex.EncodeToString(digest[:]),
		WorkspaceRoot:            root,
		NativeStateRoot:          home,
	}, authPath
}

// assertRemoved requires the path to be absent rather than accepting permission or other Stat errors
// as proof of successful credential cleanup.
// assertRemoved требует отсутствия пути, не принимая ошибку доступа или другую ошибку Stat
// за доказательство успешной очистки реквизитов.
func assertRemoved(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("authentication snapshot still exists: %v", err)
	}
}
