package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// Public entrypoint: scripts/tests/runtime-mcp-catalog-test.sh. Ответ получается
// настоящим serveMCP, а не копией metadata/schema в consumer fixture.
func TestRuntimeMCPCatalogWireProducer(t *testing.T) {
	path := os.Getenv("KODEX_RUNTIME_MCP_CATALOG_FIXTURE")
	if path == "" {
		t.Skip("KODEX_RUNTIME_MCP_CATALOG_FIXTURE is not configured")
	}
	type fixture struct {
		Name    string
		Input   runtimecontract.RunnerInput
		Catalog json.RawMessage
	}
	files := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, ProjectRef: "prj_fixture", LeaseRef: "lea_fixture", LeaseFence: "fence", LeaseGeneration: 1, FileCatalog: &runtimecontract.RuntimeFileCatalog{Ref: "vfc_fixture", Digest: strings.Repeat("a", 64), Purposes: []string{runtimecontract.FilePurposeProject}}}
	email := files
	email.IntegrationGrants = []runtimecontract.RunnerIntegrationGrant{{Ref: "igr_fixture", ConnectionRef: "int_fixture", DefinitionKey: "email", DefinitionVersion: "1.4.1", DefinitionDigest: strings.Repeat("b", 64), CapabilityKey: "email.message.send", Operation: "SEND", InputSchema: `{"type":"object","properties":{"subject":{"type":"string"}}}`, InputSchemaSHA256: strings.Repeat("c", 64)}}
	schemaDigest := sha256.Sum256([]byte(email.IntegrationGrants[0].InputSchema))
	email.IntegrationGrants[0].InputSchemaSHA256 = hex.EncodeToString(schemaDigest[:])
	inputs := []fixture{{Name: "ordinary"}, {Name: "system-assistant", Input: runtimecontract.RunnerInput{SystemAssistant: true}}, {Name: "files", Input: files}, {Name: "email-and-files", Input: email}, {Name: "delegation", Input: runtimecontract.RunnerInput{DelegationTargets: []runtimecontract.RunnerDelegationTarget{{Ref: "agt_fixture"}}}}}
	for i := range inputs {
		request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"agent-runner-tools","method":"tools/list","params":{}}`))
		response := httptest.NewRecorder()
		(&Server{}).serveMCP(response, request, inputs[i].Input)
		if response.Code != http.StatusOK {
			t.Fatal("actual MCP catalog unavailable")
		}
		inputs[i].Catalog = append([]byte(nil), response.Body.Bytes()...)
	}
	raw, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
