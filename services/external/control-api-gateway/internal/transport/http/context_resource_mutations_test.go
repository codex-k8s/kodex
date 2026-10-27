package httptransport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestContextResourceMutationPreservesReferenceAndHeaderGuards checks accepted
// pins and each pre-RPC rejection shared by skill and memory lifecycle handlers.
// TestContextResourceMutationPreservesReferenceAndHeaderGuards проверяет допустимые
// привязки и каждый общий отказ до RPC для lifecycle навыков и памяти.
func TestContextResourceMutationPreservesReferenceAndHeaderGuards(t *testing.T) {
	for _, ref := range []string{"skl_fixture01", "mem_fixture01"} {
		for _, test := range []struct {
			name, reference, key, etag string
			valid                      bool
		}{
			{"valid", ref, "key-fixture01", `"4"`, true},
			{"reference", "bad", "key-fixture01", `"4"`, false},
			{"key", ref, "bad", `"4"`, false},
			{"missing-version", ref, "key-fixture01", "", false},
			{"zero-version", ref, "key-fixture01", `"0"`, false},
			{"noncanonical-version", ref, "key-fixture01", `"04"`, false},
		} {
			t.Run(ref+"/"+test.name, func(t *testing.T) {
				writer := httptest.NewRecorder()
				mutation, ok := requireVersionedReferenceMutation(writer, test.reference, test.key, test.etag)
				if ok != test.valid || !ok && (mutation != nil || writer.Code != http.StatusBadRequest) {
					t.Fatal("resource mutation guard changed")
				}
				if ok && (mutation.GetExpectedVersion() != 4 || mutation.GetIdempotencyKey() != test.key || writer.Body.Len() != 0) {
					t.Fatal("accepted mutation pins changed")
				}
			})
		}
	}
}

// TestContextBindingMutationPreservesBodyAndVersionGuards exercises strict body
// decoding, safe integer bounds and mutation headers without contacting a server.
// TestContextBindingMutationPreservesBodyAndVersionGuards проверяет строгий body,
// безопасные числовые пределы и заголовки mutation без обращения к серверу.
func TestContextBindingMutationPreservesBodyAndVersionGuards(t *testing.T) {
	const validBody = `{"revisionRef":"rev_fixture01","expectedBindingVersion":0}`
	for _, test := range []struct {
		name, agent, resource, body, etag string
		valid                             bool
	}{
		{"valid-skill", "agt_fixture01", "skl_fixture01", validBody, `"4"`, true},
		{"valid-memory", "agt_fixture01", "mem_fixture01", validBody, `"4"`, true},
		{"bad-json", "agt_fixture01", "skl_fixture01", "{", `"4"`, false},
		{"extra-body", "agt_fixture01", "skl_fixture01", validBody + "{}", `"4"`, false},
		{"unknown-field", "agt_fixture01", "skl_fixture01", `{"revisionRef":"rev_fixture01","private":"sentinel"}`, `"4"`, false},
		{"agent", "bad", "skl_fixture01", validBody, `"4"`, false},
		{"resource", "agt_fixture01", "bad", validBody, `"4"`, false},
		{"revision", "agt_fixture01", "skl_fixture01", `{"revisionRef":"bad"}`, `"4"`, false},
		{"negative-binding", "agt_fixture01", "skl_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":-1}`, `"4"`, false},
		{"overflow-binding", "agt_fixture01", "skl_fixture01", `{"revisionRef":"rev_fixture01","expectedBindingVersion":9007199254740992}`, `"4"`, false},
		{"missing-version", "agt_fixture01", "skl_fixture01", validBody, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/binding", strings.NewReader(test.body))
			body, mutation, ok := requireContextBindingMutation(writer, request, test.agent, test.resource, "key-fixture01", test.etag)
			if ok != test.valid || !ok && (mutation != nil || writer.Code != http.StatusBadRequest) {
				t.Fatal("binding mutation guard changed")
			}
			if ok && (body.RevisionRef != "rev_fixture01" || body.ExpectedBindingVersion != 0 || mutation.GetExpectedVersion() != 4 || writer.Body.Len() != 0) {
				t.Fatal("accepted binding fields changed")
			}
		})
	}
}

// TestSkillRevisionDigestMutationGuards covers the shared validate/publish/discard
// input path, retaining exact resource/revision/digest and versioned header checks.
// TestSkillRevisionDigestMutationGuards проверяет общий вход validate/publish/discard,
// сохраняя точные проверки resource/revision/digest и versioned-заголовков.
func TestSkillRevisionDigestMutationGuards(t *testing.T) {
	digest := strings.Repeat("a", 64)
	validBody := `{"expectedDigest":"` + digest + `"}`
	for _, test := range []struct {
		name, resource, revision, body, etag string
		valid                                bool
	}{
		{"valid", "skl_fixture01", "rev_fixture01", validBody, `"4"`, true},
		{"resource", "bad", "rev_fixture01", validBody, `"4"`, false},
		{"revision", "skl_fixture01", "bad", validBody, `"4"`, false},
		{"body", "skl_fixture01", "rev_fixture01", "{", `"4"`, false},
		{"missing-digest", "skl_fixture01", "rev_fixture01", "{}", `"4"`, false},
		{"bad-digest", "skl_fixture01", "rev_fixture01", `{"expectedDigest":"private-sentinel"}`, `"4"`, false},
		{"missing-version", "skl_fixture01", "rev_fixture01", validBody, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/revision", strings.NewReader(test.body))
			body, mutation, ok := requireSkillRevisionDigestMutation(writer, request, test.resource, test.revision, "key-fixture01", test.etag)
			if ok != test.valid || !ok && (mutation != nil || writer.Code != http.StatusBadRequest) {
				t.Fatal("revision digest mutation guard changed")
			}
			if ok && (body.ExpectedDigest != digest || mutation.GetExpectedVersion() != 4 || writer.Body.Len() != 0) {
				t.Fatal("accepted revision digest fields changed")
			}
		})
	}
}
