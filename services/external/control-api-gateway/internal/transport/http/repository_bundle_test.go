package httptransport

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// bundlePublicClient records the exact bounded owner request and returns only synthetic safe metadata for HTTP projection tests.
// bundlePublicClient учитывает exact bounded owner request и возвращает лишь synthetic safe metadata для HTTP projection tests.
type bundlePublicClient struct {
	cp.RepositoryBundlePublicationServiceClient
	response *cp.GetRepositoryBundlePublicationResponse
	request  *cp.GetRepositoryBundlePublicationRequest
}

// GetRepositoryBundlePublication records cursor/version/source pins without interpreting them as caller authority.
// GetRepositoryBundlePublication учитывает cursor/version/source pins, не принимая их за caller authority.
func (c *bundlePublicClient) GetRepositoryBundlePublication(_ context.Context, r *cp.GetRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.GetRepositoryBundlePublicationResponse, error) {
	c.request = r
	return c.response, nil
}

// bundlePublicFixture supplies one complete metadata page; file contents and credentials are deliberately absent.
// bundlePublicFixture задаёт одну complete metadata page; file contents и credentials намеренно отсутствуют.
func bundlePublicFixture() *cp.GetRepositoryBundlePublicationResponse {
	pin := &cp.RepositoryBundleArtifactPin{Ref: "art_archive000", Revision: 1, Version: 2, Sha256: strings.Repeat("a", 64), SizeBytes: 100}
	manifest := proto.Clone(pin).(*cp.RepositoryBundleArtifactPin)
	manifest.Ref = "art_manifest000"
	p := &cp.RepositoryBundlePublication{Ref: "rbp_fixture000", Version: 3, ProjectRef: "prj_fixture000", OriginRunRef: "run_fixture000", OriginRuntimeRevisionRef: "rrev_fixture000", InvocationRef: "inv_fixture000", GateRef: "gat_fixture000", ConnectionRef: "int_fixture000", ConnectionVersion: 4, GrantRef: "grt_fixture000", GrantVersion: 5, RepositoryRef: "fixture/repository", BaseCommitSha: strings.Repeat("b", 40), ProposalBranch: "kodex/bundle/rbp_fixture000", Archive: pin, Manifest: manifest, ApprovalDigest: strings.Repeat("c", 64), FileCount: 1, TotalBytes: 4, State: cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_WAITING_APPROVAL, CreatedAt: timestamppb.New(time.Unix(1700000000, 0)), ExpiresAt: timestamppb.New(time.Unix(1700000000, 0).Add(24 * time.Hour))}
	return &cp.GetRepositoryBundlePublicationResponse{Proposal: p, Files: []*cp.RepositoryBundleFile{{Path: "main.go", Mode: "100644", Bytes: 4, Sha256: strings.Repeat("d", 64)}}, Offset: 0, NextOffset: 1, Complete: true}
}

// TestRepositoryBundlePublicManifestBoundary proves exact owner-gate mapping, truthful cursors and a final encoded 24KiB ceiling, including JSON escape expansion; direct handler tests do not assert transport authentication.
// TestRepositoryBundlePublicManifestBoundary проверяет exact owner-Gate mapping, truthful cursors и окончательный encoded24KiB ceiling, включая JSON escape expansion; direct handler tests не подтверждают transport authentication.
func TestRepositoryBundlePublicManifestBoundary(t *testing.T) {
	for _, name := range []string{"valid", "blocked delivery", "partial delivery", "unknown delivery", "wrong gate", "false complete", "truncated page", "substituted revision", "unknown state", "escaped wire overflow", "terminal offset"} {
		t.Run(name, func(t *testing.T) {
			response := bundlePublicFixture()
			client := &bundlePublicClient{response: response}
			server := &Server{control: &controlplaneclient.Client{RepositoryBundles: client}}
			limit := 32
			params := generated.GetOwnerGateRepositoryBundleParams{Limit: &limit}
			want := 200
			switch name {
			case "blocked delivery":
				response.Proposal.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED
				response.Proposal.CandidateCommitSha = strings.Repeat("b", 40)
				response.Proposal.CandidateTreeSha = strings.Repeat("c", 40)
				response.Proposal.ConfirmedAt = timestamppb.Now()
				response.Proposal.ContinuationState = "BLOCKED"
				response.Proposal.ContinuationCode = "CALLBACK_INPUT_TOO_LARGE"
				response.Proposal.ContinuationNodeRef = "nod_exact12345"
			case "partial delivery":
				response.Proposal.ContinuationCode = "CALLBACK_INPUT_TOO_LARGE"
				want = 502
			case "unknown delivery":
				response.Proposal.ContinuationState = "RETRY"
				want = 502
			case "wrong gate":
				response.Proposal.GateRef = "gat_other000"
				want = 502
			case "false complete":
				response.Complete = false
				want = 502
			case "truncated page":
				response.Files = nil
				want = 502
			case "substituted revision":
				response.Proposal.Archive.Revision = 0
				want = 502
			case "unknown state":
				response.Proposal.State = cp.RepositoryBundlePublicationState(999)
				want = 502
			case "terminal offset":
				offset := 1
				version := int64(3)
				digest := response.Proposal.Manifest.Sha256
				params.Offset = &offset
				params.ExpectedVersion = &version
				params.ExpectedManifestSha256 = &digest
				response.Offset = 1
				response.Files = nil
			case "escaped wire overflow":
				response.Proposal.FileCount = 32
				response.Proposal.TotalBytes = 0
				response.NextOffset = 32
				response.Files = nil
				for i := range 32 {
					response.Files = append(response.Files, &cp.RepositoryBundleFile{Path: fmt.Sprintf("%s%03d.go", strings.Repeat("&", 490), i), Mode: "100644", Sha256: strings.Repeat("a", 64)})
				}
				want = 502
			}
			w := httptest.NewRecorder()
			server.GetOwnerGateRepositoryBundle(w, httptest.NewRequest("GET", "/api/v1/owner-gates/gat_fixture000/repository-bundle", nil), "gat_fixture000", params)
			if w.Code != want {
				t.Fatalf("wire status %d expected %d: %s", w.Code, want, w.Body.String())
			}
			if client.request == nil || client.request.GateRef != "gat_fixture000" || client.request.Limit != 32 {
				t.Fatal("owner request lost exact gate/bound")
			}
			if w.Body.Len() > 24<<10 {
				t.Fatal("wire exceeded budget")
			}
			if want == 200 && (w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "\"3\"" || !strings.Contains(w.Body.String(), `"complete":true`)) {
				t.Fatal("safe owner page lost anti-cache/version/completeness")
			}
		})
	}
}
