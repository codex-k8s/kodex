package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func writeBackProviderFixture(t *testing.T, adapter *Adapter, provider string) *cp.ManagedConfigurationGitWriteBackWork {
	t.Helper()
	source := sourceWorkFixture(t, adapter, provider)
	created := timestamppb.New(time.Now().Add(-time.Minute))
	content := []byte("name: fixture\n")
	p := &cp.ManagedConfigurationGitWriteBack{Ref: "mcwb_fixture", Version: 10, ConfigurationRef: "cfg_fixture", Kind: cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, ConfigurationVersion: 2, SourceRef: "source_fixture", SourceVersion: 3, ConnectionRef: source.GetConnectionRef(), ConnectionVersion: source.GetConnectionVersion(), RepositoryRef: source.GetRepositoryRef(), SourceRefName: "main", Path: "recipe.json", BaseCommitSha: strings.Repeat("a", 40), BaseContentSha256: strings.Repeat("a", 64), ProposedContentSha256: sourceContentDigest(content), ContentFormat: "JSON", ProposalBranch: "kodex/writeback/mcwb_fixture", ApprovalDigest: strings.Repeat("b", 64), State: cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_CLAIMED, CandidateCommitSha: strings.Repeat("b", 40), CreatedAt: created, ApprovedAt: created, BranchConfirmedAt: created}
	return &cp.ManagedConfigurationGitWriteBackWork{Proposal: p, Lease: &cp.ManagedConfigurationGitWriteBackLease{ProposalRef: p.Ref, Attempt: 2, ClaimGeneration: 2, Claimant: "fixture", Fence: "fixture-fence", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, Mode: cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE, Effect: cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST, DefinitionKey: source.GetDefinitionKey(), DefinitionVersion: source.GetDefinitionVersion(), DefinitionDigest: source.GetDefinitionDigest(), DefinitionPackage: source.GetDefinitionPackage(), PublicConfiguration: source.GetPublicConfiguration(), CredentialRevision: source.GetCredentialRevision(), ProposedContent: content, EffectMarker: "kodex-configuration-writeback:" + p.Ref, CommitMessage: "Update managed configuration", CommitAuthorName: "Kodex", CommitAuthorEmail: "configuration@kodex.invalid", CommitTime: created, CandidateTreeSha: strings.Repeat("c", 40), CandidateBlobSha: strings.Repeat("d", 40), Deadline: timestamppb.New(time.Now().Add(time.Minute))}
}

func TestWriteBackProviderPRReadbackIsExactAndNeverRetriesCreate(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		for _, mode := range []string{"success", "foreign-head", "foreign-repo", "foreign-url", "foreign-marker", "duplicate", "unauthorized", "rate-limit", "readonly"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				adapter := testAdapter(t)
				work := writeBackProviderFixture(t, adapter, provider)
				if mode == "readonly" {
					work.Mode = cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY
					work.Proposal.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME
					work.EffectStartedAt = timestamppb.Now()
				}
				gets, posts := 0, 0
				client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Header.Get("Authorization") != "Bearer source-fixture-token" {
						t.Fatal("provider credential changed")
					}
					status := http.StatusOK
					if request.Method == http.MethodPost {
						posts++
						status = http.StatusCreated
					} else if request.Method == http.MethodGet {
						gets++
					} else {
						t.Fatal("unexpected provider method")
					}
					if mode == "unauthorized" {
						status = http.StatusUnauthorized
					}
					if mode == "rate-limit" {
						status = http.StatusTooManyRequests
					}
					p := work.GetProposal()
					head, repository, marker := p.GetCandidateCommitSha(), p.GetRepositoryRef(), work.GetEffectMarker()
					if mode == "foreign-head" {
						head = strings.Repeat("f", 40)
					}
					if mode == "foreign-repo" {
						repository = "foreign/repository"
					}
					if mode == "foreign-marker" {
						marker = "foreign"
					}
					var value any
					if provider == "github" {
						if request.URL.Path != "/repos/acme/repo/pulls" {
							t.Fatal("GitHub repository path changed")
						}
						url := "https://github.com/acme/repo/pull/7"
						if mode == "foreign-url" {
							url = "https://other.example/pull/7"
						}
						value = map[string]any{"number": 7, "html_url": url, "body": marker, "head": map[string]any{"ref": p.GetProposalBranch(), "sha": head, "repo": map[string]string{"full_name": repository}}, "base": map[string]any{"ref": "main", "repo": map[string]string{"full_name": repository}}}
					} else {
						if request.URL.EscapedPath() != "/api/v4/projects/group%2Fproject/merge_requests" {
							t.Fatal("GitLab repository path changed")
						}
						url := strings.TrimSuffix(work.GetPublicConfiguration().AsMap()["base_url"].(string), "/") + "/group/project/-/merge_requests/7"
						if mode == "foreign-url" {
							url = "https://other.example/merge_requests/7"
						}
						value = map[string]any{"iid": 7, "web_url": url, "description": marker, "source_branch": p.GetProposalBranch(), "target_branch": "main", "sha": head, "source_project_id": 1, "target_project_id": 1, "references": map[string]string{"full": repository + "!7"}}
					}
					if request.Method == http.MethodGet {
						if mode == "duplicate" {
							value = []any{value, value}
						} else {
							value = []any{value}
						}
					} else {
						raw, _ := io.ReadAll(request.Body)
						var input map[string]any
						if json.Unmarshal(raw, &input) != nil {
							t.Fatal("invalid PR input")
						}
						if input["title"] != "Update managed configuration" {
							t.Fatal("PR title changed")
						}
					}
					raw, _ := json.Marshal(value)
					return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				})}
				adapter.githubHTTPClient, adapter.providerHTTPClient = client, client
				execution, err := adapter.OpenConfigurationWriteBack(t.Context(), work)
				if err != nil {
					t.Fatal(err)
				}
				defer execution.Close()
				_, createErr := execution.CreatePullRequest(t.Context(), work.GetProposal().GetCandidateCommitSha())
				if mode == "readonly" {
					if createErr == nil || posts != 0 {
						t.Fatal("readonly recovery created PR")
					}
					return
				}
				if posts != 1 {
					t.Fatalf("create calls=%d", posts)
				}
				result, found, findErr := execution.FindPullRequest(t.Context(), work.GetProposal().GetCandidateCommitSha())
				valid := mode == "success"
				if valid && (createErr != nil || findErr != nil || !found || result.Ref != "7") {
					t.Fatalf("exact PR result: create=%v find=%v found=%v", createErr, findErr, found)
				}
				if !valid && found && findErr == nil {
					t.Fatal("foreign or ambiguous PR became receipt")
				}
				if gets != 1 || posts != 1 {
					t.Fatal("hidden provider retry")
				}
			})
		}
	}
}

func TestWriteBackRejectsMissingGitDestinationAndTamperedWorkBeforeCredentialRead(t *testing.T) {
	for _, mode := range []string{"api-only", "git-only", "unknown", "digest", "branch", "author", "expired", "state"} {
		t.Run(mode, func(t *testing.T) {
			adapter := testAdapter(t)
			work := writeBackProviderFixture(t, adapter, "github")
			switch mode {
			case "api-only", "git-only":
				definition := managedDefinitionFixture(t, adapter.definitions["github"], "Управляемый пакет", "UI")
				destinations := []integrationpackage.NetworkDestination{}
				for _, destination := range definition.Spec.NetworkDestinations {
					if (mode == "api-only" && destination.Key == "github_api") || (mode == "git-only" && destination.Key == "github_git") {
						destinations = append(destinations, destination)
					}
				}
				definition.Spec.NetworkDestinations = destinations
				definition = sealedDefinitionFixture(t, definition)
				work.DefinitionPackage, _ = json.Marshal(definition)
				work.DefinitionVersion, work.DefinitionDigest = definition.Metadata.Version, definition.Digest
			case "unknown":
				work.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 1})
			case "digest":
				work.ProposedContent = []byte("tampered")
			case "branch":
				work.Proposal.ProposalBranch = "main"
			case "author":
				work.CommitAuthorEmail = "foreign@kodex.invalid"
			case "expired":
				work.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
			case "state":
				work.Proposal.State = cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED
			}
			before := proto.Clone(work)
			if execution, err := adapter.OpenConfigurationWriteBack(t.Context(), work); err == nil {
				execution.Close()
				t.Fatal("invalid work accepted")
			}
			if !proto.Equal(before, work) {
				t.Fatal("validation mutated owner snapshot")
			}
		})
	}
}
