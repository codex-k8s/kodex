package callback

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

// integrationOutcomesClient supplies owner wire counterexamples and records the
// exact fenced request; it does not simulate owner authorization or GitHub effects.
// integrationOutcomesClient задаёт owner wire counterexamples и сохраняет точный
// fenced request; owner authorization либо GitHub effects не имитируются.
type integrationOutcomesClient struct {
	cp.RuntimeWorkServiceClient
	response *cp.GetExecutionIntegrationOutcomesResponse
	request  *cp.GetExecutionIntegrationOutcomesRequest
	calls    int
}

// GetExecutionIntegrationOutcomes records the exact request and returns test wire.
// GetExecutionIntegrationOutcomes сохраняет точный запрос и возвращает test wire.
func (client *integrationOutcomesClient) GetExecutionIntegrationOutcomes(_ context.Context, request *cp.GetExecutionIntegrationOutcomesRequest, _ ...grpc.CallOption) (*cp.GetExecutionIntegrationOutcomesResponse, error) {
	client.request = request
	client.calls++
	return client.response, nil
}

// TestIntegrationOutcomesClosedPage rejects cross-mode selectors, malformed wire,
// stale input and reconstructed audit evidence while preserving exact Unicode pages.
// TestIntegrationOutcomesClosedPage отвергает cross-mode selectors, malformed wire,
// stale input и восстановленное audit evidence, сохраняя точные Unicode pages.
func TestIntegrationOutcomesClosedPage(t *testing.T) {
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_project001", LeaseRef: "lse_origin001", LeaseFence: "fixture-fence", LeaseGeneration: 7, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{{}}}
	digest := strings.Repeat("a", 64)
	args := map[string]any{"outcomes_read": map[string]any{"source_invocation_ref": "inv_source001", "source_sha256": digest, "offset_bytes": 0, "maximum_bytes": 4096}}
	pageText := "Полный результат"
	valid := cp.GetExecutionIntegrationOutcomesResponse{SourceInvocationRef: "inv_source001", SourceSha256: digest, SizeBytes: int64(len(pageText)), OutcomesCount: 3, OffsetBytes: 0, NextOffsetBytes: int64(len(pageText)), Eof: true, PageSha256: workflowReceiptDigest([]byte(pageText)), Text: pageText}
	client := &integrationOutcomesClient{response: &valid}
	server := &Server{config: Config{RequestTimeout: time.Second}, control: &controlplaneclient.Client{Runtime: client}}
	result, err := server.integrationOutcomes(t.Context(), input, args)
	if err != nil {
		t.Fatal("exact page unavailable", err)
	}
	if client.request.LeaseRef != input.LeaseRef || client.request.Fence != input.LeaseFence || client.request.Generation != input.LeaseGeneration {
		t.Fatal("page lost lease pins")
	}
	safe, err := safeIntegrationOutcomesReceipt(input, args, result)
	if err != nil || strings.Contains(safe, pageText) || !strings.Contains(safe, valid.PageSha256) {
		t.Fatal("safe receipt lost commitments or retained text")
	}
	raw, _ := json.Marshal(result)
	var reconstructed map[string]any
	_ = json.Unmarshal(raw, &reconstructed)
	if _, err := safeIntegrationOutcomesReceipt(input, args, reconstructed); err == nil {
		t.Fatal("reconstructed map became trusted activity")
	}
	other := input
	other.LeaseGeneration++
	if _, err := safeIntegrationOutcomesReceipt(other, args, result); err == nil {
		t.Fatal("stale input reused page evidence")
	}
	value := result.(integrationOutcomesResult)
	value.wire["outcomes_page"].(map[string]any)["text"] = "altered"
	if _, err := safeIntegrationOutcomesReceipt(input, args, value); err == nil {
		t.Fatal("mutated page retained trusted receipt")
	}
	calls := client.calls
	for _, bad := range []map[string]any{{"outcomes_read": args["outcomes_read"], "grant_ref": "grt_foreign001"}, {"outcomes_read": nil}, {"outcomes_read": map[string]any{"source_invocation_ref": "inv_source001", "source_sha256": digest, "offset_bytes": 0, "maximum_bytes": 4097}}} {
		if _, err := server.integrationOutcomes(t.Context(), input, bad); err == nil {
			t.Fatal("malformed selector accepted")
		}
	}
	if client.calls != calls {
		t.Fatal("invalid selector reached owner")
	}
	for name, mutate := range map[string]func(*cp.GetExecutionIntegrationOutcomesResponse){
		"digest":    func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.SourceSha256 = strings.Repeat("b", 64) },
		"source":    func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.SourceInvocationRef = "inv_foreign001" },
		"offset":    func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.OffsetBytes = 1 },
		"next":      func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.NextOffsetBytes++ },
		"eof":       func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.Eof = false },
		"page-hash": func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.PageSha256 = digest },
		"count":     func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.OutcomesCount = 0 },
		"unicode":   func(r *cp.GetExecutionIntegrationOutcomesResponse) { r.Text = string([]byte{0xff}) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := valid
			mutate(&changed)
			client.response = &changed
			if _, err := server.integrationOutcomes(t.Context(), input, args); err == nil {
				t.Fatal("malformed owner page accepted")
			}
		})
	}
}
