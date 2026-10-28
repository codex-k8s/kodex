package app

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
)

// unreadableDiagnosticCause panics if code attempts to inspect raw error text.
// unreadableDiagnosticCause паникует при попытке прочитать сырой текст ошибки.
type unreadableDiagnosticCause struct{}

// Error deliberately rejects formatting as proof that closed diagnostics use
// only typed identities and never parse or include private SDK messages.
// Error намеренно запрещает форматирование, доказывая использование лишь типов,
// а не разбора либо вывода приватных сообщений SDK.
func (unreadableDiagnosticCause) Error() string { panic("private error formatted") }

// TestNATSStartupDiagnosticClosesFormattingAndPreservesCause verifies the actual
// process formatting shape, unknown registry closure and private errors.Is/As.
// TestNATSStartupDiagnosticClosesFormattingAndPreservesCause проверяет фактическую
// форму форматирования процесса, закрытие unknown-этапа и приватные errors.Is/As.
func TestNATSStartupDiagnosticClosesFormattingAndPreservesCause(t *testing.T) {
	cause := &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	failure := natsStartupFailure(natsStageConnect, cause)
	want := "connect realtime NATS consumer error_class=connection_refused"
	if failure.Error() != want || !errors.Is(failure, syscall.ECONNREFUSED) {
		t.Fatal("private typed cause or closed class lost")
	}
	var typed *net.OpError
	if !errors.As(failure, &typed) || typed != cause {
		t.Fatal("cause identity changed")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if got := fmt.Sprintf(format, failure); got != want {
			t.Fatal("debug formatting exposed private fields")
		}
	}
	if got := fmt.Sprintf("control-api-gateway failed: %v\n", failure); got != "control-api-gateway failed: "+want+"\n" {
		t.Fatal("existing main boundary lost diagnostic")
	}
	wrapped := natsStartupFailure(natsStageVerify, fmt.Errorf("PRIVATE_OUTER_TEXT: %w", failure))
	if wrapped.Error() != want || !errors.Is(wrapped, syscall.ECONNREFUSED) {
		t.Fatal("outer SDK wrapper exposed private text or hid the first stage")
	}
	unknown := natsStartupFailure(natsStartupStage(255), unreadableDiagnosticCause{})
	if got := unknown.Error(); got != "realtime NATS startup error_class=unknown" {
		t.Fatal("unknown diagnostic escaped its closed registry")
	}
}

// TestNATSFailureClassRetainsPermanentPrecedence checks typed auth/TLS and file
// failures ahead of network wrappers. SDK no_servers never invents refused.
// TestNATSFailureClassRetainsPermanentPrecedence проверяет приоритет typed auth/TLS
// и файловых отказов перед сетью. SDK no_servers не выдумывает refused.
func TestNATSFailureClassRetainsPermanentPrecedence(t *testing.T) {
	for _, test := range []struct {
		cause error
		stage natsStartupStage
		want  string
	}{
		{errors.Join(syscall.ECONNREFUSED, nats.ErrAuthorization), natsStageConnect, "authorization"},
		{errors.Join(syscall.ECONNREFUSED, x509.HostnameError{Host: "PRIVATE_HOST"}), natsStageConnect, "tls_verification"},
		{&os.PathError{Op: "read", Path: "PRIVATE_CREDENTIAL_PATH", Err: syscall.ENOENT}, natsStageLoadCredentials, "filesystem"},
		{unreadableDiagnosticCause{}, natsStageLoadCredentials, "credentials_configuration"},
		{unreadableDiagnosticCause{}, natsStageLoadCA, "tls_configuration"},
		{errors.Join(syscall.ECONNREFUSED, nkeys.ErrNoSeedFound), natsStageConnect, "credentials_configuration"},
		{errors.Join(nats.ErrTLS, syscall.ECONNRESET), natsStageConnect, "tls_handshake"},
		{nats.ErrNoServers, natsStageConnect, "no_servers"},
		{nats.ErrTimeout, natsStageVerify, "timeout"},
		{nats.ErrSecureConnWanted, natsStageConnect, "tls_configuration"},
		{nats.ErrBadTimeout, natsStageConnect, "configuration"},
		{syscall.ECONNREFUSED, natsStageConnect, "connection_refused"},
		{unreadableDiagnosticCause{}, natsStageConnect, "unknown"},
	} {
		if got := natsFailureClass(test.stage, test.cause); got != test.want {
			t.Fatalf("closed class=%s want=%s", got, test.want)
		}
	}
	for _, cause := range []error{nats.ErrAuthExpired, nats.ErrAuthRevoked, nats.ErrAccountAuthExpired, nats.ErrPermissionViolation} {
		if natsFailureClass(natsStageConnect, cause) != "authorization" {
			t.Fatal("authorization sentinel escaped its closed class")
		}
	}
}

// TestNATSDiagnosticOptionsPreserveNativeApplication verifies real SDK option
// parsing before any socket connection: missing and malformed files get closed
// stages without disclosing paths or changing the option application count.
// TestNATSDiagnosticOptionsPreserveNativeApplication проверяет настоящий разбор
// опций SDK до подключения: отсутствующий либо неверный файл даёт закрытый этап,
// не раскрывая путь и не меняя число применений опции.
func TestNATSDiagnosticOptionsPreserveNativeApplication(t *testing.T) {
	root := t.TempDir()
	malformed := filepath.Join(root, "PRIVATE_MALFORMED_FILE")
	if err := os.WriteFile(malformed, []byte("PRIVATE_CONTENT"), 0o600); err != nil {
		t.Fatal("write fixture file")
	}
	for _, test := range []struct {
		stage  natsStartupStage
		option nats.Option
		want   string
	}{
		{natsStageLoadCA, nats.RootCAs(filepath.Join(root, "PRIVATE_MISSING_CA")), "filesystem"},
		{natsStageLoadCA, nats.RootCAs(malformed), "tls_configuration"},
		{natsStageLoadClientCertificate, nats.ClientCert(malformed, malformed), "tls_configuration"},
		{natsStageLoadCredentials, nats.UserCredentials(filepath.Join(root, "PRIVATE_MISSING_JWT")), "filesystem"},
	} {
		_, err := nats.Connect("tls://127.0.0.1:1", diagnosticNATSOption(test.stage, test.option))
		var marked *natsStartupError
		if !errors.As(err, &marked) || marked.stage != test.stage || natsFailureClass(marked.stage, marked.cause) != test.want {
			t.Fatal("native option failure lost closed stage/class")
		}
		if natsStartupFailure(natsStageConnect, err) != err {
			t.Fatal("outer connect erased the precise option marker")
		}
		for _, forbidden := range []string{root, "PRIVATE_", "127.0.0.1", "tls://"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatal("native option diagnostic exposed private metadata")
			}
		}
	}
	// SDK deliberately accepts an undecorated JWT string at option time;
	// missing seed fails later in its unchanged signing callback.
	// SDK намеренно принимает строку JWT без decorations при применении опции;
	// отсутствие seed обнаруживается позже в прежнем callback подписи.
	credentials := nats.GetDefaultOptions()
	if err := diagnosticNATSOption(natsStageLoadCredentials, nats.UserCredentials(malformed))(&credentials); err != nil {
		t.Fatal("diagnostic option introduced new credential validation")
	}
	value, err := credentials.UserJWT()
	if err != nil || value != "PRIVATE_CONTENT" || credentials.SignatureCB == nil {
		t.Fatal("diagnostic option changed native credential callbacks")
	}
	_, signingError := credentials.SignatureCB([]byte("disposable challenge"))
	if !errors.Is(signingError, nkeys.ErrNoSeedFound) || natsFailureClass(natsStageConnect, signingError) != "credentials_configuration" {
		t.Fatal("native missing-seed failure lost its typed configuration class")
	}
	if strings.Contains(natsStartupFailure(natsStageConnect, signingError).Error(), root) {
		t.Fatal("native signing failure exposed credential path")
	}

	applications := 0
	option := diagnosticNATSOption(natsStageLoadCA, func(options *nats.Options) error { applications++; options.Name = "fixture"; return nil })
	options := nats.GetDefaultOptions()
	if err := option(&options); err != nil || applications != 1 || options.Name != "fixture" {
		t.Fatal("diagnostic wrapper changed SDK option application")
	}
}

// TestConnectNATSReportsNativeCAFailure exercises the actual gateway helper and
// SDK file parsing, rather than only the classifier. CA rejection occurs before
// dialing, with the original cause retained privately and one closed diagnostic.
// TestConnectNATSReportsNativeCAFailure проверяет сам helper gateway и разбор
// файла SDK, а не только классификатор. Отказ CA происходит до dial: исходная
// причина сохраняется приватно, наружу выдаётся одна закрытая диагностика.
func TestConnectNATSReportsNativeCAFailure(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		root := t.TempDir()
		path := filepath.Join(root, "PRIVATE_CA_PATH")
		wantClass := "filesystem"
		if malformed {
			if err := os.WriteFile(path, []byte("PRIVATE_CA_CONTENT"), 0o600); err != nil {
				t.Fatal("write CA fixture")
			}
			wantClass = "tls_configuration"
		}
		connection, err := connectNATS(Config{NATSURL: "tls://127.0.0.1:1", NATSTLSServerName: "fixture.invalid", NATSCAFile: path}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		var marked *natsStartupError
		if connection != nil || !errors.As(err, &marked) || marked.stage != natsStageLoadCA || natsFailureClass(marked.stage, marked.cause) != wantClass {
			t.Fatal("actual connect helper erased native CA failure")
		}
		if got := fmt.Sprintf("control-api-gateway failed: %v", err); got != "control-api-gateway failed: load realtime NATS CA error_class="+wantClass {
			t.Fatal("process diagnostic exposed or erased the native CA failure")
		}
	}
}
