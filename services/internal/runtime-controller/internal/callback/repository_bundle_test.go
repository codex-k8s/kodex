package callback

import (
	"context"
	"encoding/json"
	"errors"
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

// bundlePrepareClient records the actual native request and can lose or substitute the owner ACK; it never grants remote Git authority.
// bundlePrepareClient учитывает actual native request и моделирует lost/substituted owner ACK, не выдавая remote Git authority.
type bundlePrepareClient struct {
	cp.RepositoryBundlePublicationServiceClient
	calls  []*cp.PrepareRepositoryBundlePublicationRequest
	err    error
	mutate func(*cp.RepositoryBundlePublication)
}

// PrepareRepositoryBundlePublication returns only a typed pending proposal tied to the exact received origin, grant and source pins.
// PrepareRepositoryBundlePublication выдаёт лишь typed pending proposal, привязанную к exact origin/grant/source pins request.
func (c *bundlePrepareClient) PrepareRepositoryBundlePublication(_ context.Context, r *cp.PrepareRepositoryBundlePublicationRequest, _ ...grpc.CallOption) (*cp.PrepareRepositoryBundlePublicationResponse, error) {
	c.calls = append(c.calls, proto.Clone(r).(*cp.PrepareRepositoryBundlePublicationRequest))
	if c.err != nil {
		return nil, c.err
	}
	p := &cp.RepositoryBundlePublication{Ref: "rbp_fixture000", Version: 1, GateRef: "gat_fixture000", InvocationRef: "inv_fixture000", GrantRef: r.GrantRef, GrantVersion: 7, ConnectionRef: "int_fixture000", OriginRunRef: "run_fixture000", OriginRuntimeRevisionRef: "rrev_fixture000", BaseCommitSha: r.BaseCommitSha, ProposalBranch: "kodex/bundle/rbp_fixture000", Archive: r.Archive, Manifest: r.Manifest, FileCount: 108, TotalBytes: 291000, State: cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_WAITING_APPROVAL}
	if c.mutate != nil {
		c.mutate(p)
	}
	return &cp.PrepareRepositoryBundlePublicationResponse{Proposal: p}, nil
}

// TestRepositoryBundleNativeProducerBoundary proves exact immutable grant/lease mapping, compact result and one-shot lost-ACK behavior; it does not authenticate a real transport or publish Git.
// TestRepositoryBundleNativeProducerBoundary доказывает exact immutable grant/lease mapping, compact result и one-shot lost-ACK behavior; real transport authentication либо Git publication не проверяется.
func TestRepositoryBundleNativeProducerBoundary(t *testing.T) {
	grant := runtimecontract.RunnerIntegrationGrant{Ref: "grt_fixture000", GrantVersion: 7, ConnectionRef: "int_fixture000", Operation: repositorybundle.Operation, CapabilityKey: repositorybundle.Operation, Risk: "WRITE", ApprovalPolicy: "HUMAN_EACH_EFFECT"}
	input := runtimecontract.RunnerInput{ProjectRef: "prj_fixture000", RunRef: "run_fixture000", RuntimeRevisionRef: "rrev_fixture000", LeaseRef: "lse_fixture000", LeaseFence: "fence-fixture", LeaseGeneration: 9, IntegrationGrants: []runtimecontract.RunnerIntegrationGrant{grant}}
	args := map[string]any{"grant_ref": grant.Ref, "input": repositorybundle.InvocationInput(repositorybundle.ArtifactPin{Ref: "art_archive000", Revision: 2, Version: 5, SHA256: strings.Repeat("a", 64), Size: 390000}, repositorybundle.ArtifactPin{Ref: "art_manifest000", Revision: 3, Version: 6, SHA256: strings.Repeat("b", 64), Size: 12000}, strings.Repeat("c", 40))}
	for _, name := range []string{"valid", "foreign grant", "extra locator", "nonhuman grant", "no project", "unknown input", "lost ACK", "substituted source", "substituted origin", "unknown state"} {
		t.Run(name, func(t *testing.T) {
			selected := input
			selected.IntegrationGrants = append([]runtimecontract.RunnerIntegrationGrant(nil), input.IntegrationGrants...)
			raw, _ := json.Marshal(args)
			var arguments map[string]any
			if json.Unmarshal(raw, &arguments) != nil {
				t.Fatal("fixture decode")
			}
			c := &bundlePrepareClient{}
			server := &Server{control: &controlplaneclient.Client{RepositoryBundles: c}, config: Config{RequestTimeout: time.Second}}
			wantCalls := 1
			wantCode := codes.OK
			switch name {
			case "foreign grant":
				arguments["grant_ref"] = "grt_foreign000"
				wantCalls = 0
				wantCode = codes.PermissionDenied
			case "extra locator":
				arguments["connection_ref"] = "int_foreign000"
				wantCalls = 0
				wantCode = codes.PermissionDenied
			case "nonhuman grant":
				selected.IntegrationGrants[0].ApprovalPolicy = "NONE"
				wantCalls = 0
				wantCode = codes.PermissionDenied
			case "no project":
				selected.ProjectRef = ""
				wantCalls = 0
				wantCode = codes.PermissionDenied
			case "unknown input":
				arguments["input"].(map[string]any)["inline_bytes"] = "not allowed"
				wantCalls = 0
				wantCode = codes.InvalidArgument
			case "lost ACK":
				c.err = status.Error(codes.Unavailable, "synthetic ACK lost")
				wantCode = codes.Unavailable
			case "substituted source":
				c.mutate = func(p *cp.RepositoryBundlePublication) {
					p.Archive = proto.Clone(p.Archive).(*cp.RepositoryBundleArtifactPin)
					p.Archive.Revision++
				}
				wantCode = codes.Unavailable
			case "substituted origin":
				c.mutate = func(p *cp.RepositoryBundlePublication) { p.OriginRuntimeRevisionRef = "rrev_other000" }
				wantCode = codes.Unavailable
			case "unknown state":
				c.mutate = func(p *cp.RepositoryBundlePublication) { p.State = cp.RepositoryBundlePublicationState(999) }
				wantCode = codes.Unavailable
			}
			result, err := server.prepareRepositoryBundle(t.Context(), selected, arguments, json.RawMessage(`"call-fixed"`))
			if status.Code(err) != wantCode || len(c.calls) != wantCalls {
				t.Fatalf("producer boundary: %v calls=%d expected=%v/%d", err, len(c.calls), wantCode, wantCalls)
			}
			if name == "valid" {
				request := c.calls[0]
				if request.LeaseRef != input.LeaseRef || request.Fence != input.LeaseFence || request.Generation != input.LeaseGeneration || request.GrantRef != grant.Ref || request.IdempotencyKey != stableKey(input.LeaseRef, `"call-fixed"`) {
					t.Fatal("producer lost lease/grant/idempotency pins")
				}
				encoded, e := json.Marshal(result)
				if e != nil || len(encoded) > 1024 || strings.Contains(string(encoded), "inline_bytes") {
					t.Fatal("pending result exposed source bytes")
				}
			}
		})
	}
	// The generic path must stop before dereferencing the absent owner client.
	// Generic path останавливается до обращения к отсутствующему owner client.
	if _, err := (&Server{}).invoke(t.Context(), input, args, json.RawMessage(`"generic"`)); err == nil {
		t.Fatal("generic invocation accepted bundle publication")
	} else {
		var local *integrationCallInputError
		if !errors.As(err, &local) {
			t.Fatal("wrong generic refusal", err)
		}
	}
}
