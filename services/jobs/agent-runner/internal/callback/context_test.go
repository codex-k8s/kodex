package callback

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestSkillCallbackUsesExactExecutionAndContextPins(t *testing.T) {
	for _, mode := range []string{"positive", "denied", "wrong digest", "wrong size", "corrupt body"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte("synthetic skill bytes")
			digest := sha256.Sum256(body)
			pin := runtimecontract.RuntimeSkillFile{Path: "references/fixture.txt", ArtifactRef: "art_abcdefgh", ArtifactRevision: 7,
				Digest: "sha256:" + hex.EncodeToString(digest[:]), SizeBytes: int64(len(body))}
			skill := runtimecontract.RuntimeSkillBundle{BundleRef: "sklb_abcdefgh", RevisionRef: "sklv_abcdefgh", Revision: 1,
				BindingRef: "ctxb_abcdefgh", BindingVersion: 1, Digest: strings.Repeat("a", 64), Name: "fixture", Description: "Fixture skill",
				ScanEngine: "fixture", ScanDigest: strings.Repeat("b", 64), ScannedAt: time.Now(),
				Provenance: runtimecontract.RuntimeContextProvenance{ActorRef: "act_abcdefgh", SourceKind: "UI", Digest: strings.Repeat("c", 64), CreatedAt: time.Now()},
				Files:      []runtimecontract.RuntimeSkillFile{pin, {Path: "SKILL.md", ArtifactRef: "art_ijklmnop", ArtifactRevision: 1, Digest: pin.Digest, SizeBytes: pin.SizeBytes}}}
			input := validWarmTurnFixture()
			snapshot := runtimecontract.RuntimeContextSnapshot{Schema: runtimecontract.RuntimeContextSchema, OrganizationRef: input.OrganizationRef, ProjectRef: input.ProjectRef, AgentRef: input.AgentRef,
				Skills: []runtimecontract.RuntimeSkillBundle{skill}}
			snapshot.Digest, _ = snapshot.ComputeDigest()
			input.ContextSnapshot = &snapshot
			input.ExecutionBindingDigest, input.MCPBindingDigest, _ = runtimecontract.RuntimeExecutionBindingDigests(input)
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls++
				query := request.URL.Query()
				if request.Method != http.MethodGet || request.URL.Path != "/v1/executions/"+input.LeaseRef+"/artifacts/"+pin.ArtifactRef ||
					len(query) != 5 || query.Get("context_kind") != "SKILL_BUNDLE" || query.Get("skill_ref") != skill.BundleRef || query.Get("skill_revision_ref") != skill.RevisionRef ||
					query.Get("skill_path") != pin.Path || query.Get("artifact_revision") != "7" ||
					request.Header.Get("Authorization") != "Bearer fixture-ticket" || request.Header.Get("X-Kodex-Execution-Binding-Digest") != input.ExecutionBindingDigest ||
					request.Header.Get("X-Kodex-Runtime-Revision-Digest") != input.RuntimeRevisionDigest || request.Header.Get("X-Kodex-Callback-Method") != "artifact" ||
					request.Header.Get("X-Kodex-Attempt") != strconv.Itoa(int(input.Attempt)) {
					t.Error("callback lost exact context or execution binding")
				}
				if mode == "denied" {
					http.Error(writer, "sensitive owner diagnostic", http.StatusForbidden)
					return
				}
				writer.Header().Set("Content-Type", "text/plain")
				writer.Header().Set("X-Kodex-Artifact-Digest", pin.Digest)
				writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
				if mode == "wrong digest" {
					writer.Header().Set("X-Kodex-Artifact-Digest", "sha256:"+strings.Repeat("a", 64))
				}
				if mode == "wrong size" {
					writer.Header().Set("Content-Length", strconv.Itoa(len(body)+1))
				}
				payload := append([]byte(nil), body...)
				if mode == "corrupt body" {
					payload[0] = 'x'
				}
				_, _ = writer.Write(payload)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := &Client{http: server.Client(), base: base, token: "fixture-ticket"}
			var destination bytes.Buffer
			err := client.WriteSkillFile(t.Context(), input, skill, pin, &destination)
			if (err == nil) != (mode == "positive") || calls != 1 {
				t.Fatalf("unexpected callback outcome: %v", err)
			}
			if mode == "positive" && !bytes.Equal(body, destination.Bytes()) {
				t.Fatal("callback changed bytes")
			}
			if err != nil && strings.Contains(err.Error(), "sensitive") {
				t.Fatal("callback exposed owner diagnostic")
			}
			pin.ArtifactRevision++
			if client.WriteSkillFile(t.Context(), input, skill, pin, &destination) == nil || calls != 1 {
				t.Fatal("unbound file reached callback")
			}
		})
	}
}
