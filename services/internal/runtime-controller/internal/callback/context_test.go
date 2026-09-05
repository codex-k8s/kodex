package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestContextArtifactRouteBindsOwnerReadAndDoesNotExposeMismatches(t *testing.T) {
	content := []byte("---\nname: fixture\ndescription: Synthetic skill\n---\nRead the approved references.\n")
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	for _, scenario := range []string{"exact", "revision", "project", "digest", "size", "unknown pin", "wrong binding", "duplicate query", "revoked", "unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			manager, _, input, _, ticket := providerCredentialRefreshRouteFixture(t, func(input *runtimecontract.RunnerInput) {
				now := time.Now().UTC()
				input.ContextSnapshot.Skills = []runtimecontract.RuntimeSkillBundle{{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 2,
					BindingRef: "ctxb_abcdefgh", BindingVersion: 3, Digest: strings.Repeat("a", 64), Name: "fixture", Description: "Synthetic skill",
					ScanEngine: "fixture", ScanDigest: strings.Repeat("b", 64), ScannedAt: now,
					Provenance: runtimecontract.RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("c", 64), CreatedAt: now},
					Files:      []runtimecontract.RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 7, Digest: digest, SizeBytes: int64(len(content))}},
				}}
				input.ContextSnapshot.Digest, _ = input.ContextSnapshot.ComputeDigest()
			})
			response := &cp.ReadExecutionArtifactResponse{Artifact: &cp.Artifact{Ref: "art_abcdefgh", ProjectRef: input.ProjectRef,
				Revision: 7, SizeBytes: int64(len(content)), Digest: digest}, Content: content}
			query := url.Values{"context_kind": {"SKILL_BUNDLE"}, "skill_ref": {"sklb_abcdefgh"}, "skill_revision_ref": {"sklv_abcdefgh"},
				"skill_path": {"SKILL.md"}, "artifact_revision": {"7"}}
			switch scenario {
			case "revision":
				response.Artifact.Revision++
			case "project":
				response.Artifact.ProjectRef = "proj_ijklmnop"
			case "digest":
				response.Content = []byte("different content")
			case "size":
				response.Artifact.SizeBytes++
			case "unknown pin":
				query.Set("artifact_revision", "8")
			case "duplicate query":
				query.Add("skill_ref", "sklb_abcdefgh")
			}
			client := &artifactProjectionClient{response: response}
			if scenario == "revoked" {
				client.err = status.Error(codes.NotFound, "owner fixture rejected the lease")
			} else if scenario == "unavailable" {
				client.err = status.Error(codes.Unavailable, "owner fixture unavailable")
			}
			server := &Server{config: Config{RequestTimeout: time.Second}, manager: manager, control: &controlplaneclient.Client{Runtime: client}}
			request := httptest.NewRequest(http.MethodGet, "/v1/executions/"+input.LeaseRef+"/artifacts/art_abcdefgh?"+query.Encode(), nil)
			request.Header.Set("Authorization", "Bearer "+ticket)
			bindTestExecutionHeaders(request, input, "artifact")
			if scenario == "wrong binding" {
				request.Header.Set("X-Kodex-Execution-Binding-Digest", strings.Repeat("f", 64))
			}
			writer := httptest.NewRecorder()
			server.route(writer, request)
			if scenario == "exact" {
				if writer.Code != http.StatusOK || writer.Body.String() != string(content) || writer.Header().Get("X-Kodex-Artifact-Digest") != digest {
					t.Fatalf("exact context download failed: %d", writer.Code)
				}
			} else if writer.Code < 400 || strings.Contains(writer.Body.String(), string(content)) {
				t.Fatalf("mismatch exposed context bytes: %d", writer.Code)
			}
			deniedBeforeRPC := scenario == "unknown pin" || scenario == "wrong binding" || scenario == "duplicate query"
			if deniedBeforeRPC && len(client.requests) != 0 || !deniedBeforeRPC && len(client.requests) != 1 {
				t.Fatal("context owner read cardinality is invalid")
			}
			if len(client.requests) == 1 && (client.requests[0].LeaseRef != input.LeaseRef || client.requests[0].Fence != input.LeaseFence || client.requests[0].Generation != input.LeaseGeneration) {
				t.Fatal("context read lost exact lease authority")
			}
		})
	}
}

func TestContextArtifactSelectorRequiresExactSnapshotMembership(t *testing.T) {
	now := time.Now().UTC()
	input := runtimecontract.RunnerInput{OrganizationRef: "org_abcdefgh", ProjectRef: "proj_abcdefgh", AgentRef: "agt_abcdefgh"}
	skill := runtimecontract.RuntimeSkillBundle{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 2,
		BindingRef: "ctxb_abcdefgh", BindingVersion: 3, Digest: strings.Repeat("a", 64), Name: "fixture", Description: "Synthetic skill",
		ScanEngine: "fixture", ScanDigest: strings.Repeat("b", 64), ScannedAt: now,
		Provenance: runtimecontract.RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("c", 64), CreatedAt: now},
		Files:      []runtimecontract.RuntimeSkillFile{{Path: "SKILL.md", ArtifactRef: "art_abcdefgh", ArtifactRevision: 7, Digest: "sha256:" + strings.Repeat("d", 64), SizeBytes: 42}},
	}
	snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef,
		ProjectRef: input.ProjectRef, AgentRef: input.AgentRef, Skills: []runtimecontract.RuntimeSkillBundle{skill}}
	snapshot.Digest, _ = snapshot.ComputeDigest()
	input.ContextSnapshot = &snapshot
	query := url.Values{"context_kind": {"SKILL_BUNDLE"}, "skill_ref": {skill.BundleRef}, "skill_revision_ref": {skill.RevisionRef},
		"skill_path": {"SKILL.md"}, "artifact_revision": {"7"}}
	if selected, file, ok := contextArtifactPin(input, "art_abcdefgh", query.Encode(), now); !ok || selected.RevisionRef != skill.RevisionRef || file != skill.Files[0] {
		t.Fatal("exact pinned file was not selected")
	}
	for _, key := range []string{"context_kind", "skill_ref", "skill_revision_ref", "skill_path", "artifact_revision"} {
		changed, _ := url.ParseQuery(query.Encode())
		changed.Set(key, "foreign")
		if _, _, ok := contextArtifactPin(input, "art_abcdefgh", changed.Encode(), now); ok {
			t.Fatal("foreign selector was accepted")
		}
		changed, _ = url.ParseQuery(query.Encode())
		changed.Add(key, changed.Get(key))
		if _, _, ok := contextArtifactPin(input, "art_abcdefgh", changed.Encode(), now); ok {
			t.Fatal("duplicate selector was accepted")
		}
	}
	for _, raw := range []string{query.Encode() + "&unexpected=x", query.Encode() + ";", strings.Replace(query.Encode(), "SKILL.md", "..%2FSKILL.md", 1)} {
		if _, _, ok := contextArtifactPin(input, "art_abcdefgh", raw, now); ok {
			t.Fatal("unsafe query was accepted")
		}
	}
	input.ProjectRef = "proj_ijklmnop"
	if _, _, ok := contextArtifactPin(input, "art_abcdefgh", query.Encode(), now); ok {
		t.Fatal("foreign project was accepted")
	}
}
