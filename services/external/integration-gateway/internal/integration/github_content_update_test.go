package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// githubContentBoundaryRequest preserves fixture authority pins while substituting input bytes;
// rejected boundary inputs still receive their actual digest rather than a mismatched digest shortcut.
// githubContentBoundaryRequest сохраняет привязки тестовой авторизации при подстановке входных байтов;
// отвергаемые граничные входы получают свой настоящий digest, а не отказ из-за постороннего mismatch.
func githubContentBoundaryRequest(t *testing.T, definition integrationpackage.Package, operation, content string, credential *CredentialRevision) Request {
	t.Helper()
	input := map[string]any{"path": "docs/requirements.md", "branch": "feature/1858", "message": "Synthetic content update", "content_base64": ""}
	if operation == "github.repository.content.update" {
		input["sha"] = strings.Repeat("a", 40)
	}
	request := invocationRequest(t, definition, operation, input, credential)
	request.Input["content_base64"] = content
	raw, err := json.Marshal(request.Input)
	if err != nil {
		t.Fatal("encode boundary input")
	}
	digest := sha256.Sum256(raw)
	request.InputDigest = hex.EncodeToString(digest[:])
	return request
}

// TestGitHubContentMutationByteBoundaries executes the real SDK adapter at decoded-content limits;
// it checks exact bytes/hash, repository/path/branch/blob pins and one provider request, while malformed,
// oversized and managed-narrowed inputs make no request and create retains its existing smaller schema.
// TestGitHubContentMutationByteBoundaries выполняет настоящий SDK adapter на границах декодированных байтов;
// проверяет точные bytes/hash, repository/path/branch/blob и один provider request, а неверный base64,
// превышение и managed narrowing не делают запросов; create сохраняет прежнюю меньшую schema.
func TestGitHubContentMutationByteBoundaries(t *testing.T) {
	for _, scenario := range []struct {
		name, operation, malformed string
		size                       int
		managed                    bool
		wantCalls                  int
	}{
		{name: "update-64KiB-plus-one", operation: "github.repository.content.update", size: (64 << 10) + 1, wantCalls: 1},
		{name: "update-256KiB", operation: "github.repository.content.update", size: 256 << 10, wantCalls: 1},
		{name: "update-256KiB-plus-one", operation: "github.repository.content.update", size: (256 << 10) + 1},
		{name: "update-malformed-base64", operation: "github.repository.content.update", malformed: "AB=="},
		{name: "create-24KiB", operation: "github.repository.content.create", size: 24 << 10, wantCalls: 1},
		{name: "create-24KiB-plus-one", operation: "github.repository.content.create", size: (24 << 10) + 1},
		{name: "managed-update-24KiB", operation: "github.repository.content.update", size: 24 << 10, managed: true, wantCalls: 1},
		{name: "managed-update-24KiB-plus-one", operation: "github.repository.content.update", size: (24 << 10) + 1, managed: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-content-token")
			definition := adapter.definitions["github"]
			if scenario.managed {
				definition = managedDefinitionFixture(t, definition, "Ограниченный GitHub", "UI")
				for i := range definition.Spec.Capabilities {
					if definition.Spec.Capabilities[i].Key != scenario.operation {
						continue
					}
					for j := range definition.Spec.Capabilities[i].InputFields {
						if definition.Spec.Capabilities[i].InputFields[j].Key == "content_base64" {
							definition.Spec.Capabilities[i].InputFields[j].MaximumLength = 32768
						}
					}
				}
				definition = sealedDefinitionFixture(t, definition)
			}
			content := make([]byte, scenario.size)
			for i := range content {
				content[i] = byte(i % 251)
			}
			encoded := base64.StdEncoding.EncodeToString(content)
			if scenario.malformed != "" {
				encoded = scenario.malformed
			}
			if scenario.size >= 256<<10 && len(encoded) != 349528 {
				t.Fatal("boundary no longer exercises equal base64 lengths across the decoded limit")
			}
			request := githubContentBoundaryRequest(t, definition, scenario.operation, encoded, credential)
			calls := 0
			commit := strings.Repeat("b", 40)
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodPut || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/contents/docs/requirements.md" || r.URL.RawQuery != "" || r.URL.Fragment != "" {
					t.Fatal("content mutation escaped exact provider route")
				}
				var body struct {
					Content, Branch, Message, SHA string
				}
				decoder := json.NewDecoder(r.Body)
				decoder.DisallowUnknownFields()
				if decoder.Decode(&body) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) || body.Branch != request.Input["branch"] || body.Message != request.Input["message"] {
					t.Fatal("SDK mutation body lost exact branch or message")
				}
				expectedSHA, _ := request.Input["sha"].(string)
				actual, err := base64.StdEncoding.Strict().DecodeString(body.Content)
				if err != nil || !bytes.Equal(actual, content) || sha256.Sum256(actual) != sha256.Sum256(content) || body.SHA != expectedSHA {
					t.Fatal("SDK mutation body changed content bytes, digest or expected blob SHA")
				}
				return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"commit":{"sha":"` + commit + `"}}`))}, nil
			})}
			result, err := adapter.Execute(t.Context(), request)
			if calls != scenario.wantCalls {
				t.Errorf("provider requests=%d want=%d", calls, scenario.wantCalls)
			}
			if scenario.wantCalls == 0 {
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != "INTEGRATION_REQUEST_REJECTED" || result.Summary != "" {
					t.Fatal("invalid content did not fail closed before provider mutation")
				}
				return
			}
			var projection struct{ SHA, Path string }
			digest := sha256.Sum256([]byte(result.Summary))
			if err != nil || json.Unmarshal([]byte(result.Summary), &projection) != nil || projection.SHA != commit || projection.Path != request.Input["path"] || result.Receipt.ProviderEffectRef != "github-commit:"+commit || result.Receipt.EffectKey != request.EffectKey || result.Receipt.InputDigest != request.InputDigest || result.Receipt.ResponseDigest != hex.EncodeToString(digest[:]) {
				t.Fatal("successful mutation lost exact output or receipt pins")
			}
		})
	}
}

// TestGitHubContentInputExpansionPreservesSafeResponseBudget exercises complete SDK reads at the
// existing 64 KiB safe-output boundary so fixing input cannot silently expand other provider results.
// TestGitHubContentInputExpansionPreservesSafeResponseBudget выполняет полные SDK reads на прежней
// границе безопасного ответа 64 KiB, чтобы исправление входа не расширило результаты других операций.
func TestGitHubContentInputExpansionPreservesSafeResponseBudget(t *testing.T) {
	for _, size := range []int{64 << 10, (64 << 10) + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			adapter := testAdapter(t)
			credential := testCredential(t, adapter, "synthetic-content-token")
			projection := map[string]any{"number": 1, "title": "Fixture", "state": "open", "body": ""}
			empty, _ := json.Marshal(projection)
			projection["body"] = strings.Repeat("x", size-len(empty))
			wire, _ := json.Marshal(projection)
			if len(wire) != size {
				t.Fatal("fixture safe projection missed byte boundary")
			}
			projection["id"] = 1
			raw, _ := json.Marshal(projection)
			calls := 0
			adapter.githubHTTPClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != http.MethodGet || r.URL.Host != "api.github.com" || r.URL.Path != "/repos/acme/repo/issues/1" {
					t.Fatal("response fixture escaped repository")
				}
				return &http.Response{Request: r, StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(raw))}, nil
			})}
			request := invocationRequest(t, adapter.definitions["github"], "github.issue.read", map[string]any{"issue_number": 1}, credential)
			result, err := adapter.Execute(t.Context(), request)
			if calls != 1 {
				t.Fatal("response boundary changed provider request cardinality")
			}
			if size == 64<<10 {
				if err != nil || len(result.Summary) != size {
					t.Fatal("valid bounded safe response was rejected")
				}
			} else {
				var safe *SafeError
				if !errors.As(err, &safe) || safe.Code != "INTEGRATION_RESPONSE_INVALID" || result.Summary != "" {
					t.Fatal("oversized safe response was accepted")
				}
			}
		})
	}
}
