package callback

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestCatalogDownloadUsesSeparateBoundedAuthenticatedClient verifies the file client outlives the short control timeout, replaces caller authority/cookies, omits upstream cookies and never dials for an already-cancelled request.
// TestCatalogDownloadUsesSeparateBoundedAuthenticatedClient проверяет отдельный файловый бюджет вопреки короткому control timeout, замену authority/cookies, отсутствие cookies upstream в ответе и отсутствие dial у заранее отменённого запроса.
func TestCatalogDownloadUsesSeparateBoundedAuthenticatedClient(t *testing.T) {
	body := []byte("exact synthetic file")
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	input := validWarmTurnFixture()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture01", Digest: strings.Repeat("a", 64), Total: 1, Purposes: []string{"PROJECT"}}
	path := "/v1/executions/" + input.LeaseRef + "/artifacts/art_fixture01?purpose=PROJECT&entry_ref=vfe_fixture01&revision=1&digest=" + digest
	calls := 0
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.RequestURI() != path || r.Header.Get("Authorization") != "Bearer execution-ticket" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Kodex-Callback-Method") != "artifact" || r.Header.Get("X-Kodex-Execution-Binding-Digest") != input.ExecutionBindingDigest {
			t.Error("download lost exact trusted authority")
		}
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Kodex-Artifact-Digest", digest)
		w.Header().Set("Set-Cookie", "unexpected=hidden")
		_, _ = w.Write(body)
	}))
	defer upstream.Close()
	base, _ := url.Parse(upstream.URL)
	client := &Client{http: &http.Client{Timeout: time.Millisecond}, files: filetransfer.NewClient(upstream.Client().Transport.(*http.Transport)), base: base, token: "execution-ticket"}
	defer client.Close()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Authorization", "Bearer local-token")
	request.Header.Set("Cookie", "caller=hidden")
	result := &fileDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	client.ServeCatalogFile(result, request, input)
	if result.Code != http.StatusOK || !bytes.Equal(result.Body.Bytes(), body) || result.Header().Get("Set-Cookie") != "" || result.Header().Get("Cache-Control") != "no-store" || calls != 1 {
		t.Fatal("catalog download failed")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result = &fileDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	client.ServeCatalogFile(result, request.WithContext(ctx), input)
	if result.Code != http.StatusBadGateway || calls != 1 {
		t.Fatal("cancelled download reached upstream")
	}
}

// TestCatalogDownloadRejectsRedirectAndInvalidMetadata checks redirect, oversize, digest mismatch and denial failures with exactly one upstream request per synthetic case.
// TestCatalogDownloadRejectsRedirectAndInvalidMetadata проверяет отказы перенаправления, превышения размера, несовпадения дайджеста и доступа с ровно одним upstream-запросом на случай.
func TestCatalogDownloadRejectsRedirectAndInvalidMetadata(t *testing.T) {
	input := validWarmTurnFixture()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture01", Digest: strings.Repeat("a", 64), Purposes: []string{"PROJECT"}}
	path := "/v1/executions/" + input.LeaseRef + "/artifacts/art_fixture01?purpose=PROJECT&entry_ref=vfe_fixture01&revision=1&digest=sha256:" + strings.Repeat("b", 64)
	for _, mode := range []string{"redirect", "oversize", "digest", "denied"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "redirect":
					w.Header().Set("Location", "/other")
					w.WriteHeader(http.StatusTemporaryRedirect)
				case "denied":
					w.WriteHeader(http.StatusForbidden)
				default:
					w.Header().Set("Content-Type", "text/plain")
					w.Header().Set("X-Kodex-Artifact-Digest", "sha256:"+strings.Repeat("b", 64))
					if mode == "oversize" {
						w.Header().Set("Content-Length", strconv.FormatInt(runtimecontract.MaximumArtifactTransferBytes+1, 10))
					} else {
						w.Header().Set("Content-Length", "0")
						w.Header().Set("X-Kodex-Artifact-Digest", "wrong")
					}
				}
			}))
			defer upstream.Close()
			base, _ := url.Parse(upstream.URL)
			client := &Client{http: upstream.Client(), files: filetransfer.NewClient(upstream.Client().Transport.(*http.Transport)), base: base, token: "fixture"}
			defer client.Close()
			result := &fileDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			client.ServeCatalogFile(result, httptest.NewRequest(http.MethodGet, path, nil), input)
			if result.Code != http.StatusBadGateway || calls != 1 {
				t.Fatal("invalid upstream response accepted or retried")
			}
		})
	}
}

// TestCatalogDownloadCancellationClosesActiveResponse waits for actual partial streaming, cancels the request and requires ErrAbortHandler plus timely closure of the upstream response.
// TestCatalogDownloadCancellationClosesActiveResponse дожидается фактической частичной передачи, отменяет запрос и требует ErrAbortHandler и своевременное закрытие upstream-ответа.
func TestCatalogDownloadCancellationClosesActiveResponse(t *testing.T) {
	input := validWarmTurnFixture()
	input.FileCatalog = &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture01", Digest: strings.Repeat("a", 64), Purposes: []string{"PROJECT"}}
	digest := "sha256:" + strings.Repeat("b", 64)
	started, closed := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", "100")
		w.Header().Set("X-Kodex-Artifact-Digest", digest)
		_, _ = w.Write([]byte("x"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(closed)
	}))
	defer upstream.Close()
	base, _ := url.Parse(upstream.URL)
	client := &Client{http: upstream.Client(), files: filetransfer.NewClient(upstream.Client().Transport.(*http.Transport)), base: base, token: "fixture"}
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/v1/executions/"+input.LeaseRef+"/artifacts/art_fixture01?purpose=PROJECT&entry_ref=vfe_fixture01&revision=1&digest="+digest, nil).WithContext(ctx)
	done := make(chan any, 1)
	writer := &notifyingFileWriter{ResponseRecorder: httptest.NewRecorder(), written: make(chan struct{})}
	go func() {
		defer func() { done <- recover() }()
		client.ServeCatalogFile(writer, request, input)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not begin")
	}
	select {
	case <-writer.written:
	case <-time.After(time.Second):
		t.Fatal("partial response did not reach downstream")
	}
	cancel()
	select {
	case result := <-done:
		if result != http.ErrAbortHandler {
			t.Fatal("partial response was not aborted")
		}
	case <-time.After(time.Second):
		t.Fatal("download did not cancel")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("upstream body remained open")
	}
}

// notifyingFileWriter records the response and signals the first write exactly once so cancellation tests can wait for real downstream streaming.
// notifyingFileWriter сохраняет ответ и ровно один раз сигнализирует о первой записи, позволяя тесту отмены дождаться реальной передачи клиенту.
type notifyingFileWriter struct {
	*httptest.ResponseRecorder
	written chan struct{}
	once    sync.Once
}

// SetWriteDeadline supplies the ResponseController capability expected by the handler without implementing timing in the in-memory test recorder.
// SetWriteDeadline предоставляет ожидаемый handler метод ResponseController без реализации сроков в тестовом recorder в памяти.
func (w *notifyingFileWriter) SetWriteDeadline(time.Time) error { return nil }

// fileDeadlineWriter adds a no-op write-deadline capability to ResponseRecorder for bounded-file handler unit tests, not a real deadline implementation.
// fileDeadlineWriter добавляет ResponseRecorder пустой метод срока записи для unit-тестов файлового handler, а не реальную реализацию сроков.
type fileDeadlineWriter struct{ *httptest.ResponseRecorder }

// SetWriteDeadline accepts deadline installation/reset for the recorder; actual HTTP transport timeout behavior is exercised separately.
// SetWriteDeadline принимает установку/сброс срока для recorder; реальное поведение HTTP transport timeout проверяется отдельно.
func (w *fileDeadlineWriter) SetWriteDeadline(time.Time) error { return nil }

// Write forwards bytes and errors to the recorder and closes the notification once after the first write, allowing deterministic cancellation sequencing.
// Write передаёт байты и ошибки recorder и после первой записи однократно закрывает уведомление для определённого порядка отмены.
func (w *notifyingFileWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	w.once.Do(func() { close(w.written) })
	return n, err
}

// TestInitialArtifactUsesFileBudgetAndExactSizeDigest serves delayed synthetic bytes with pinned metadata and verifies the separate file budget succeeds despite a one-millisecond control timeout.
// TestInitialArtifactUsesFileBudgetAndExactSizeDigest выдаёт задержанные синтетические байты с закреплёнными метаданными и проверяет успешный файловый бюджет при миллисекундном control timeout.
func TestInitialArtifactUsesFileBudgetAndExactSizeDigest(t *testing.T) {
	body := []byte("initial input")
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	input := validWarmTurnFixture()
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("X-Kodex-Artifact-Digest", digest)
		_, _ = w.Write(body)
	}))
	defer upstream.Close()
	base, _ := url.Parse(upstream.URL)
	client := &Client{http: &http.Client{Timeout: time.Millisecond}, files: filetransfer.NewClient(upstream.Client().Transport.(*http.Transport)), base: base, token: "fixture"}
	defer client.Close()
	var destination bytes.Buffer
	if err := client.WriteArtifact(t.Context(), input, runtimecontract.RunnerInputArtifact{Ref: "art_fixture01", Digest: digest, MediaType: "text/plain", SizeBytes: int64(len(body))}, &destination); err != nil || !bytes.Equal(destination.Bytes(), body) {
		t.Fatalf("initial file budget: %v", err)
	}
}
