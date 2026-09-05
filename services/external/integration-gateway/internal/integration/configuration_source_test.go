package integration

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sourceWorkFixture(t *testing.T, adapter *Adapter, provider string) *cp.ManagedConfigurationSourceWork {
	t.Helper()
	credential := testCredential(t, adapter, "source-fixture-token")
	operation := "github.repository.content.read"
	input := map[string]any{"path": "recipe.json", "ref": "main"}
	repository := "acme/repo"
	if provider == "gitlab" {
		operation, repository, input = "gitlab.repository.file.read", "group/project", map[string]any{"file_path": "recipe.json", "ref": "main"}
	}
	request := invocationRequest(t, adapter.definitions[provider], operation, input, credential)
	configuration, err := structpb.NewStruct(request.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	return &cp.ManagedConfigurationSourceWork{
		Lease:     &cp.ManagedConfigurationSourceLease{WorkRef: "cwork_fixture01", SourceGeneration: 2, Attempt: 1, ClaimGeneration: 3, Claimant: "gateway-fixture", Fence: "fixture-fence", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))},
		SourceRef: "csource_fixture01", ConfigurationRef: "cfg_fixture01", Kind: cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE,
		ConnectionRef: request.ConnectionRef, ConnectionVersion: 1, DefinitionKey: request.DefinitionKey, DefinitionVersion: request.DefinitionVersion, DefinitionDigest: request.DefinitionDigest, DefinitionPackage: request.DefinitionPackage,
		PublicConfiguration: configuration, CredentialRevision: &cp.IntegrationCredentialRevision{Ref: credential.Ref, Revision: credential.Revision, SecretRef: credential.SecretRef, SecretUid: credential.SecretUID, SecretResourceVersion: credential.SecretResourceVersion, ContentSha256: credential.ContentSHA256},
		RepositoryRef: repository, RefName: "main", Path: "recipe.json", ContentFormat: "JSON", MaximumContentBytes: 256 << 10, Deadline: timestamppb.New(time.Now().Add(time.Minute)),
	}
}

func TestConfigurationSourceReadsExactCommitAndRegularBlob(t *testing.T) {
	for _, provider := range []string{"github", "gitlab"} {
		for _, mode := range []string{"initial", "unchanged", "forward", "diverged", "symlink", "submodule", "blob_digest", "content_digest", "foreign_commit", "partial", "missing", "oversize", "unauthorized", "forbidden", "foreign_repository", "expired", "unknown_payload", "foreign_package", "gated_read"} {
			t.Run(provider+"/"+mode, func(t *testing.T) {
				adapter := testAdapter(t)
				work := sourceWorkFixture(t, adapter, provider)
				commit, previous, tree := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
				content := []byte(`{"name":"fixture","environment":{}}`)
				gitHash := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(content))+"\x00"), content...))
				blob := hex.EncodeToString(gitHash[:])
				digest := sha256.Sum256(content)
				if mode == "forward" || mode == "diverged" {
					work.PreviousCommitSha = previous
				}
				if mode == "unchanged" {
					work.PreviousCommitSha = commit
				}
				if mode == "foreign_repository" {
					work.RepositoryRef = "foreign/repository"
				}
				if mode == "expired" {
					work.Lease.ExpiresAt = timestamppb.New(time.Now().Add(-time.Second))
				}
				if mode == "unknown_payload" {
					work.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 1})
				}
				if mode == "foreign_package" {
					work.DefinitionDigest = strings.Repeat("f", 64)
				}
				if mode == "gated_read" {
					definition := managedDefinitionFixture(t, adapter.definitions[provider], "Источник с подтверждением", "UI")
					for index := range definition.Spec.Capabilities {
						if definition.Spec.Capabilities[index].Risk == "READ" {
							definition.Spec.Capabilities[index].ApprovalPolicy = "HUMAN_EACH_EFFECT"
						}
					}
					definition = sealedDefinitionFixture(t, definition)
					work.DefinitionPackage, _ = json.Marshal(definition)
					work.DefinitionVersion, work.DefinitionDigest = definition.Metadata.Version, definition.Digest
				}
				calls, contentCalls := 0, 0
				client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					calls++
					if mode == "gated_read" {
						t.Fatal("configuration source bypassed human approval")
					}
					if request.Method != http.MethodGet || request.Header.Get("Authorization") != "Bearer source-fixture-token" {
						t.Fatal("source provider authority changed")
					}
					status := http.StatusOK
					if mode == "unauthorized" {
						status = http.StatusUnauthorized
					}
					if mode == "forbidden" {
						status = http.StatusForbidden
					}
					var value any
					var raw []byte
					regularMode, regularType := "100644", "blob"
					if mode == "symlink" {
						regularMode = "120000"
					}
					if mode == "submodule" {
						regularMode, regularType = "160000", "commit"
					}
					if provider == "github" {
						switch request.URL.Path {
						case "/repos/acme/repo/commits/main":
							raw = []byte(commit)
						case "/repos/acme/repo/compare/" + previous + "..." + commit:
							ancestor := previous
							if mode == "diverged" {
								ancestor = tree
							}
							value = map[string]any{"status": "ahead", "ahead_by": 1, "behind_by": 0, "base_commit": map[string]string{"sha": previous}, "merge_base_commit": map[string]string{"sha": ancestor}}
						case "/repos/acme/repo/git/commits/" + commit:
							got := commit
							if mode == "foreign_commit" {
								got = previous
							}
							value = map[string]any{"sha": got, "tree": map[string]string{"sha": tree}}
						case "/repos/acme/repo/git/trees/" + tree:
							entries := []any{map[string]any{"path": work.Path, "mode": regularMode, "type": regularType, "sha": blob, "size": len(content)}}
							if mode == "missing" {
								entries = []any{}
							}
							value = map[string]any{"sha": tree, "truncated": mode == "partial", "tree": entries}
						case "/repos/acme/repo/git/blobs/" + blob:
							contentCalls++
							got, encoded := blob, base64.StdEncoding.EncodeToString(content)
							if mode == "blob_digest" {
								got = previous
							}
							if mode == "content_digest" {
								encoded = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", len(content))))
							}
							value = map[string]any{"sha": got, "encoding": "base64", "size": len(content), "content": encoded}
						default:
							t.Fatal("source reader escaped exact GitHub route")
						}
					} else {
						const root = "/api/v4/projects/group/project/repository"
						switch request.URL.Path {
						case root + "/commits/main":
							value = map[string]string{"id": commit}
						case root + "/merge_base":
							refs := request.URL.Query()["refs[]"]
							if len(refs) != 2 || refs[0] != previous || refs[1] != commit {
								t.Fatal("merge base request lost exact pins")
							}
							ancestor := previous
							if mode == "diverged" {
								ancestor = tree
							}
							value = map[string]string{"id": ancestor}
						case root + "/tree":
							if request.URL.Query().Get("ref") != commit {
								t.Fatal("tree read used mutable ref")
							}
							entries := []any{map[string]string{"id": blob, "path": work.Path, "type": regularType, "mode": regularMode}}
							if mode == "missing" {
								entries = []any{}
							}
							if mode == "partial" {
								entries = append(entries, entries[0])
							}
							value = entries
						case root + "/files/" + work.Path:
							contentCalls++
							if request.URL.Query().Get("ref") != commit {
								t.Fatal("file read used mutable ref")
							}
							gotCommit, gotBlob, gotDigest := commit, blob, hex.EncodeToString(digest[:])
							if mode == "foreign_commit" {
								gotCommit = previous
							}
							if mode == "blob_digest" {
								gotBlob = previous
							}
							if mode == "content_digest" {
								gotDigest = strings.Repeat("e", 64)
							}
							value = map[string]any{"file_path": work.Path, "ref": commit, "commit_id": gotCommit, "blob_id": gotBlob, "content_sha256": gotDigest, "encoding": "base64", "size": len(content), "content": base64.StdEncoding.EncodeToString(content)}
						default:
							t.Fatal("source reader escaped exact GitLab route")
						}
					}
					if raw == nil {
						raw, _ = json.Marshal(value)
					}
					if mode == "oversize" {
						raw = []byte(strings.Repeat("x", maximumSourceResponseBytes+1))
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
				})}
				adapter.githubHTTPClient, adapter.providerHTTPClient = client, client
				result, err := adapter.ReadConfigurationSource(t.Context(), work)
				defer clear(result.Content)
				valid := mode == "initial" || mode == "unchanged" || mode == "forward"
				if valid {
					want := cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_INITIAL
					if mode == "unchanged" {
						want = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_UNCHANGED
					}
					if mode == "forward" {
						want = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_FAST_FORWARD
					}
					if err != nil || result.CommitSHA != commit || result.Ancestry != want || result.ContentSHA256 != hex.EncodeToString(digest[:]) || string(result.Content) != string(content) || contentCalls != 1 {
						t.Fatal("exact source result was not preserved")
					}
				} else if err == nil || len(result.Content) != 0 {
					t.Fatal("invalid source was accepted")
				}
				if (mode == "foreign_repository" || mode == "expired" || mode == "unknown_payload" || mode == "foreign_package") && calls != 0 {
					t.Fatal("invalid work reached provider")
				}
				if mode == "diverged" && (contentCalls != 0 || ConfigurationSourceFailure(err) != cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_DIVERGED) {
					t.Fatal("divergence did not stop content read")
				}
			})
		}
	}
}
