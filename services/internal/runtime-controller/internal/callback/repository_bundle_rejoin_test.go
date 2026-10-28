package callback

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// repositoryBundleRejoinPrepareOwner returns a frozen typed owner envelope and counts only preparation reads; no write method is implemented.
// repositoryBundleRejoinPrepareOwner возвращает frozen typed envelope, считая preparation reads; WRITE methods не реализованы.
type repositoryBundleRejoinPrepareOwner struct {
	cp.RepositoryBundlePublicationServiceClient
	response *cp.PrepareRepositoryBundlePublicationResponse
	calls    []*cp.PrepareRepositoryBundlePublicationRequest
}

// PrepareRepositoryBundlePublication records exact RPC pins and clones the immutable response, preserving original source/identity fields.
// PrepareRepositoryBundlePublication записывает exact RPC pins и клонирует immutable response, сохраняя исходные source/identity fields.
func (owner *repositoryBundleRejoinPrepareOwner) PrepareRepositoryBundlePublication(_ context.Context, request *cp.PrepareRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.PrepareRepositoryBundlePublicationResponse, error) {
	owner.calls = append(owner.calls, proto.Clone(request).(*cp.PrepareRepositoryBundlePublicationRequest))
	return proto.Clone(owner.response).(*cp.PrepareRepositoryBundlePublicationResponse), nil
}

// repositoryBundleRejoinRuntimeOwner supplies an existing exact ledger read and records the ordinary MCP audit requests, not durable production audit.
// repositoryBundleRejoinRuntimeOwner возвращает existing exact ledger read и учитывает обычные MCP audit requests, не durable production audit.
type repositoryBundleRejoinRuntimeOwner struct {
	cp.RuntimeWorkServiceClient
	invocationRef string
	state         *cp.GetIntegrationInvocationResponse
	reads         int
	projections   []*cp.RecordRunToolCallRequest
}

// GetIntegrationInvocation accepts only the original immutable invocation locator; a foreign locator never falls back to another receipt.
// GetIntegrationInvocation принимает только исходный immutable locator; foreign locator не подменяется другим receipt.
func (owner *repositoryBundleRejoinRuntimeOwner) GetIntegrationInvocation(_ context.Context, request *cp.GetIntegrationInvocationRequest, _ ...grpc.CallOption) (*cp.GetIntegrationInvocationResponse, error) {
	owner.reads++
	if request.GetInvocationRef() != owner.invocationRef {
		return nil, status.Error(codes.NotFound, "synthetic receipt not found")
	}
	return proto.Clone(owner.state).(*cp.GetIntegrationInvocationResponse), nil
}

// RecordRunToolCall records safe parameters/result from the real dispatcher and returns a synthetic event acknowledgement.
// RecordRunToolCall записывает safe parameters/result настоящего dispatcher и возвращает synthetic event acknowledgement.
func (owner *repositoryBundleRejoinRuntimeOwner) RecordRunToolCall(_ context.Context, request *cp.RecordRunToolCallRequest, _ ...grpc.CallOption) (*cp.RecordRunToolCallResponse, error) {
	owner.projections = append(owner.projections, proto.Clone(request).(*cp.RecordRunToolCallRequest))
	return &cp.RecordRunToolCallResponse{Event: &cp.RunEvent{Ref: "evt_rejoin000"}}, nil
}

// repositoryBundleRejoinMCPFixture supplies different current/original refs but one immutable completed Git fact; it grants no external access.
// repositoryBundleRejoinMCPFixture задаёт разные current/original refs при одном completed Git fact, не выдавая external access.
func repositoryBundleRejoinMCPFixture() (runtimecontract.RunnerInput, map[string]any, *repositoryBundleRejoinPrepareOwner, *repositoryBundleRejoinRuntimeOwner) {
	grant := runtimecontract.RunnerIntegrationGrant{Ref: "grt_fixture000", GrantVersion: 7, ConnectionRef: "int_fixture000", ConnectionVersion: 11, Operation: repositorybundle.Operation, CapabilityKey: repositorybundle.Operation, Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT"}
	input := runtimecontract.RunnerInput{Mode: runtimecontract.RunnerModeTurn, AssistantScope: runtimecontract.AssistantScopeNone, ProjectRef: "prj_fixture000", RunRef: "run_current000", NodeRef: "nod_current000", SessionRef: "ses_current000", TurnRef: "trn_current000", Attempt: 2, RuntimeRevisionRef: "rrev_current000", RuntimeRevisionDigest: strings.Repeat("f", 64), LeaseRef: "lse_current000", LeaseFence: "PRIVATE_FENCE_CANARY", LeaseGeneration: 9, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	archive := repositorybundle.ArtifactPin{Ref: "art_currentarchive000", Revision: 2, Version: 5, SHA256: strings.Repeat("a", 64), Size: 300}
	manifest := repositorybundle.ArtifactPin{Ref: "art_currentmanifest000", Revision: 3, Version: 6, SHA256: strings.Repeat("b", 64), Size: 200}
	arguments := map[string]any{"grant_ref": grant.Ref, "input": repositorybundle.InvocationInput(archive, manifest, strings.Repeat("c", 40))}
	p := &cp.RepositoryBundlePublication{Ref: "rbp_original000", Version: 7, ProjectRef: input.ProjectRef, OriginRunRef: "run_original000", OriginRuntimeRevisionRef: "rrev_original000", InvocationRef: "inv_original000", GateRef: "gat_original000", ConnectionRef: grant.ConnectionRef, ConnectionVersion: grant.ConnectionVersion, GrantRef: grant.Ref, GrantVersion: grant.GrantVersion, RepositoryRef: "fixture/repository", BaseCommitSha: strings.Repeat("c", 40), ProposalBranch: "kodex/bundle/rbp_original000", Archive: &cp.RepositoryBundleArtifactPin{Ref: "art_originalarchive000", Revision: 1, Version: 1, Sha256: archive.SHA256, SizeBytes: archive.Size}, Manifest: &cp.RepositoryBundleArtifactPin{Ref: "art_originalmanifest000", Revision: 1, Version: 1, Sha256: manifest.SHA256, SizeBytes: manifest.Size}, State: cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED, CandidateCommitSha: strings.Repeat("d", 40), CandidateTreeSha: strings.Repeat("e", 40), FileCount: 1, TotalBytes: 80}
	summary, _ := json.Marshal(map[string]any{"commit_sha": p.CandidateCommitSha, "tree_sha": p.CandidateTreeSha, "proposal_branch": p.ProposalBranch, "manifest_sha256": p.Manifest.Sha256, "file_count": p.FileCount, "total_bytes": p.TotalBytes})
	proof := &cp.RepositoryBundlePublicationRejoin{Schema: repositorybundle.RejoinSchema, Mode: repositorybundle.RejoinMode, CurrentRunRef: input.RunRef, CurrentNodeRef: input.NodeRef, CurrentRuntimeRevisionRef: input.RuntimeRevisionRef, CurrentLeaseRef: input.LeaseRef, CurrentGeneration: input.LeaseGeneration, CurrentRuntimeAttempt: input.Attempt, BusinessAttempt: 2, CurrentSessionRef: input.SessionRef, CurrentTurnRef: input.TurnRef, ConnectionVersion: grant.ConnectionVersion, SemanticDigest: strings.Repeat("a", 64), ReceiptRef: "erc_original000", ReceiptDigest: repositorybundle.Digest(summary), CurrentArchive: &cp.RepositoryBundleArtifactPin{Ref: archive.Ref, Revision: archive.Revision, Version: archive.Version, Sha256: archive.SHA256, SizeBytes: archive.Size}, CurrentManifest: &cp.RepositoryBundleArtifactPin{Ref: manifest.Ref, Revision: manifest.Revision, Version: manifest.Version, Sha256: manifest.SHA256, SizeBytes: manifest.Size}, ResultKey: "pr"}
	return input, arguments, &repositoryBundleRejoinPrepareOwner{response: &cp.PrepareRepositoryBundlePublicationResponse{Proposal: p, Rejoin: proof}}, &repositoryBundleRejoinRuntimeOwner{invocationRef: p.InvocationRef, state: &cp.GetIntegrationInvocationResponse{State: "SUCCEEDED", EffectReceiptRef: proof.ReceiptRef, ResultSummary: string(summary)}}
}

// TestRepositoryBundleReadOnlyRejoinMCP proves terminal text/structured output and ordinary audit/replay through callTool, not a direct owner-only preparation call.
// TestRepositoryBundleReadOnlyRejoinMCP проверяет terminal text/structured output и audit/replay через callTool, а не прямой owner-only prepare.
func TestRepositoryBundleReadOnlyRejoinMCP(t *testing.T) {
	input, args, prepare, owner := repositoryBundleRejoinMCPFixture()
	server := &Server{control: &controlplaneclient.Client{RepositoryBundles: prepare, Runtime: owner}, config: Config{RequestTimeout: time.Second}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	params, _ := json.Marshal(map[string]any{"name": repositorybundle.ToolName, "arguments": args})
	var baseline string
	for attempt := 0; attempt < 2; attempt++ {
		response := httptest.NewRecorder()
		server.callTool(response, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"same-call"`), Params: params}, input)
		var wire struct {
			Result struct {
				IsError    bool                       `json:"isError"`
				Structured repositoryBundleToolResult `json:"structuredContent"`
				Content    []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		if json.Unmarshal(response.Body.Bytes(), &wire) != nil || wire.Result.IsError || len(wire.Result.Content) != 1 {
			t.Fatal("typed rejoin did not traverse actual MCP dispatcher")
		}
		result := wire.Result.Structured
		if result.Mode != repositorybundle.RejoinMode || result.ReceiptRef != owner.state.EffectReceiptRef || result.CommitSHA != prepare.response.Proposal.CandidateCommitSha || result.ProposalBranch != prepare.response.Proposal.ProposalBranch || result.State != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED.String() {
			t.Fatal("MCP hid terminal immutable Git fact behind callback waiting")
		}
		var text repositoryBundleToolResult
		if json.Unmarshal([]byte(wire.Result.Content[0].Text), &text) != nil || text != result {
			t.Fatal("text and structured terminal receipts differ")
		}
		if attempt == 0 {
			baseline = wire.Result.Content[0].Text
		} else if baseline != wire.Result.Content[0].Text {
			t.Fatal("MCP replay changed immutable receipt")
		}
		if strings.Contains(response.Body.String(), input.LeaseFence) || strings.Contains(response.Body.String(), input.RuntimeRevisionRef) || strings.Contains(response.Body.String(), prepare.response.Proposal.OriginRuntimeRevisionRef) {
			t.Fatal("MCP exposed private execution proof")
		}
	}
	if len(prepare.calls) != 2 || owner.reads != 2 || len(owner.projections) != 4 || owner.projections[1].GetSafeResult() != baseline || owner.projections[3].GetSafeResult() != baseline || owner.projections[0].GetMutation().GetIdempotencyKey() != owner.projections[2].GetMutation().GetIdempotencyKey() {
		t.Fatal("terminal receipt lost audit/idempotent call correlation")
	}
	if prepare.calls[0].GetIdempotencyKey() != prepare.calls[1].GetIdempotencyKey() || prepare.response.Proposal.OriginRunRef == input.RunRef || prepare.response.Proposal.Archive.Ref == prepare.response.Rejoin.CurrentArchive.Ref {
		t.Fatal("fixture replaced original pins or minted another call key")
	}
}

// TestRepositoryBundleReadOnlyRejoinDenials checks that only explicit, current and ledger-consistent owner proof enters the special branch; normal mismatch remains closed.
// TestRepositoryBundleReadOnlyRejoinDenials проверяет explicit/current/ledger-consistent proof; mismatch normal branch остаётся закрытым.
func TestRepositoryBundleReadOnlyRejoinDenials(t *testing.T) {
	for _, name := range []string{"missing-proof", "empty-proof", "unknown-response", "unknown-proof", "unknown-current-pin", "schema", "mode", "run", "node", "revision", "lease", "generation", "attempt", "business", "session", "turn", "connection-version", "grant", "target", "base", "current-source", "semantic-format", "receipt-ref", "receipt-digest", "pending", "unknown-ledger", "ledger-unknown-fields", "commit", "branch", "manifest", "extra-fact-field"} {
		t.Run(name, func(t *testing.T) {
			input, args, prepare, owner := repositoryBundleRejoinMCPFixture()
			response := prepare.response
			p, proof := response.Proposal, response.Rejoin
			switch name {
			case "missing-proof":
				response.Rejoin = nil
			case "empty-proof":
				response.Rejoin = &cp.RepositoryBundlePublicationRejoin{}
			case "unknown-response":
				response.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "unknown-proof":
				proof.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "unknown-current-pin":
				proof.CurrentArchive.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "schema":
				proof.Schema = "unknown.v2"
			case "mode":
				proof.Mode = "WRITE"
			case "run":
				proof.CurrentRunRef = "run_foreign000"
			case "node":
				proof.CurrentNodeRef = "nod_foreign000"
			case "revision":
				proof.CurrentRuntimeRevisionRef = "rrev_foreign000"
			case "lease":
				proof.CurrentLeaseRef = "lse_foreign000"
			case "generation":
				proof.CurrentGeneration++
			case "attempt":
				proof.CurrentRuntimeAttempt++
			case "business":
				proof.BusinessAttempt = 5
			case "session":
				proof.CurrentSessionRef = "ses_foreign000"
			case "turn":
				proof.CurrentTurnRef = "trn_foreign000"
			case "connection-version":
				proof.ConnectionVersion++
			case "grant":
				p.GrantVersion++
			case "target":
				p.ConnectionRef = "int_foreign000"
			case "base":
				p.BaseCommitSha = strings.Repeat("a", 40)
			case "current-source":
				proof.CurrentArchive.Ref = "art_substituted000"
			case "semantic-format":
				proof.SemanticDigest = "invalid"
			case "receipt-ref":
				proof.ReceiptRef = "erc_foreign000"
			case "receipt-digest":
				proof.ReceiptDigest = strings.Repeat("f", 64)
			case "pending":
				p.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_QUEUED
			case "unknown-ledger":
				owner.state.State = "UNKNOWN_OUTCOME"
			case "ledger-unknown-fields":
				owner.state.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
			case "commit":
				p.CandidateCommitSha = strings.Repeat("a", 40)
			case "branch":
				p.ProposalBranch = "kodex/bundle/substituted"
			case "manifest":
				p.Manifest.Sha256 = strings.Repeat("f", 64)
			case "extra-fact-field":
				var fact map[string]any
				_ = json.Unmarshal([]byte(owner.state.ResultSummary), &fact)
				fact["unexpected"] = "PRIVATE_FACT_CANARY"
				raw, _ := json.Marshal(fact)
				owner.state.ResultSummary = string(raw)
				proof.ReceiptDigest = repositorybundle.Digest(raw)
			}
			server := &Server{control: &controlplaneclient.Client{RepositoryBundles: prepare, Runtime: owner}, config: Config{RequestTimeout: time.Second}, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			params, _ := json.Marshal(map[string]any{"name": repositorybundle.ToolName, "arguments": args})
			writer := httptest.NewRecorder()
			server.callTool(writer, httptest.NewRequest("POST", "/mcp", nil), mcpRequest{ID: json.RawMessage(`"deny-call"`), Params: params}, input)
			var wire struct {
				Result struct {
					IsError bool `json:"isError"`
				} `json:"result"`
			}
			if json.Unmarshal(writer.Body.Bytes(), &wire) != nil || !wire.Result.IsError || len(prepare.calls) != 1 || len(owner.projections) != 2 || owner.projections[1].GetState() != cp.RunToolCallState_RUN_TOOL_CALL_STATE_FAILED || strings.Contains(writer.Body.String(), "PRIVATE_FACT_CANARY") {
				t.Fatal("invalid typed rejoin escaped closed MCP/audit boundary")
			}
		})
	}
}
