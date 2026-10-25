package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// repositoryBundleAdapterWork derives the real shipped capability schema/scope/digest and projected synthetic credential for private adapter admission tests; no provider is contacted.
// repositoryBundleAdapterWork выводит actual shipped schema/scope/digest и projected synthetic credential для private adapter admission tests; provider не вызывается.
func repositoryBundleAdapterWork(t *testing.T) (*Adapter, *cp.RepositoryBundlePublicationWork, []byte) {
	t.Helper()
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "bundle-synthetic-token")
	archive, manifest, err := repositorybundle.CanonicalContainer([]repositorybundle.File{{Entry: repositorybundle.Entry{Path: "main.go", Mode: "100644"}, Bytes: []byte("package main\n")}})
	if err != nil {
		t.Fatal(err)
	}
	a := repositorybundle.ArtifactPin{Ref: "art_archive000", Revision: 1, Version: 2, SHA256: repositorybundle.Digest(archive), Size: int64(len(archive))}
	m := repositorybundle.ArtifactPin{Ref: "art_manifest000", Revision: 1, Version: 2, SHA256: repositorybundle.Digest(manifest), Size: int64(len(manifest))}
	r := invocationRequest(t, adapter.definitions["github"], repositorybundle.Operation, repositorybundle.InvocationInput(a, m, strings.Repeat("a", 40)), credential)
	configuration, err := structpb.NewStruct(r.Configuration)
	if err != nil {
		t.Fatal(err)
	}
	input, err := structpb.NewStruct(r.Input)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	p := &cp.RepositoryBundlePublication{Ref: "rbp_adapter000", Version: 3, GrantRef: r.GrantRef, GrantVersion: r.GrantVersion, ConnectionRef: r.ConnectionRef, ConnectionVersion: 5, InvocationRef: "inv_adapter000", RepositoryRef: "acme/repo", BaseCommitSha: strings.Repeat("a", 40), ProposalBranch: "kodex/bundle/rbp_adapter000", ApprovalDigest: strings.Repeat("f", 64), FileCount: 1, TotalBytes: 13, State: cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CLAIMED, CreatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(24 * time.Hour)), Archive: &cp.RepositoryBundleArtifactPin{Ref: a.Ref, Revision: a.Revision, Version: a.Version, Sha256: a.SHA256, SizeBytes: a.Size}, Manifest: &cp.RepositoryBundleArtifactPin{Ref: m.Ref, Revision: m.Revision, Version: m.Version, Sha256: m.SHA256, SizeBytes: m.Size}}
	invocation := &cp.IntegrationInvocationClaim{InvocationRef: p.InvocationRef, ConnectionRef: r.ConnectionRef, GrantRef: r.GrantRef, GrantVersion: r.GrantVersion, DefinitionKey: r.DefinitionKey, DefinitionVersion: r.DefinitionVersion, DefinitionDigest: r.DefinitionDigest, DefinitionPackage: r.DefinitionPackage, Operation: r.Operation, CapabilityKey: r.CapabilityKey, Risk: cp.IntegrationRisk_INTEGRATION_RISK_WRITE, ApprovalPolicy: cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT, PublicConfiguration: configuration, BoundedInput: input, EffectKey: r.EffectKey, InputDigest: r.InputDigest, ResourceScope: &cp.IntegrationResourceScope{Kind: cp.IntegrationResourceKind_INTEGRATION_RESOURCE_KIND_GITHUB_REPOSITORY, Values: r.ResourceScope, Digest: r.ResourceScopeDigest}, CredentialRevision: &cp.IntegrationCredentialRevision{Ref: credential.Ref, Revision: credential.Revision, SecretRef: credential.SecretRef, SecretUid: credential.SecretUID, SecretResourceVersion: credential.SecretResourceVersion, ContentSha256: credential.ContentSHA256}}
	return adapter, &cp.RepositoryBundlePublicationWork{Proposal: p, Lease: &cp.RepositoryBundleLease{ProposalRef: p.Ref, Claimant: "gateway-fixture", Fence: "fence-fixture", Attempt: 1, Generation: 1, ExpiresAt: timestamppb.New(now.Add(3 * time.Minute))}, Invocation: invocation, Mode: cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE, Manifest: manifest, CommitTime: p.CreatedAt, Deadline: timestamppb.New(now.Add(2 * time.Minute))}, archive
}

// TestRepositoryBundlePrivateAdapterAdmission exercises the actual shipped schema→typed work→credential reader boundary, rejects substitutions/unknown nested fields and verifies recovery has metadata only; it never pushes Git.
// TestRepositoryBundlePrivateAdapterAdmission проверяет actual shipped schema→typed work→credential reader boundary, отклоняет substitutions/unknown nested fields и подтверждает metadata-only recovery; Git push не выполняется.
func TestRepositoryBundlePrivateAdapterAdmission(t *testing.T) {
	adapter, base, archive := repositoryBundleAdapterWork(t)
	for _, name := range []string{"valid", "blocked continuation", "wrong owner", "wrong source", "wrong manifest", "wrong grant", "nested unknown", "configuration unknown", "recovery", "recovery with bytes", "recovery short SHA"} {
		t.Run(name, func(t *testing.T) {
			work := proto.Clone(base).(*cp.RepositoryBundlePublicationWork)
			body := archive
			denied := true
			switch name {
			case "blocked continuation":
				work.Proposal.ContinuationState, work.Proposal.ContinuationCode, work.Proposal.ContinuationNodeRef = "BLOCKED", "CALLBACK_INPUT_TOO_LARGE", "nod_exact12345"
			case "valid":
				denied = false
			case "wrong owner":
				work.Proposal.RepositoryRef = "foreign/repo"
			case "wrong source":
				work.Proposal.Archive.Revision++
			case "wrong manifest":
				work.Manifest = append(append([]byte(nil), work.Manifest...), byte(' '))
			case "wrong grant":
				work.Invocation.GrantVersion++
			case "nested unknown":
				work.Proposal.Archive.ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
			case "configuration unknown":
				work.Invocation.PublicConfiguration.Fields["owner"].ProtoReflect().SetUnknown([]byte{0xf8, 0x07, 0x01})
			case "recovery", "recovery with bytes", "recovery short SHA":
				work.Mode = cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY
				work.Proposal.State = cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME
				work.Proposal.CandidateCommitSha = strings.Repeat("b", 40)
				work.Proposal.CandidateTreeSha = strings.Repeat("c", 40)
				work.EffectStartedAt = timestamppb.Now()
				if name != "recovery with bytes" {
					body = nil
				}
				if name == "recovery short SHA" {
					work.Proposal.CandidateCommitSha = "abcde"
				}
				denied = name != "recovery"
			}
			opened, err := adapter.OpenRepositoryBundle(t.Context(), work, body)
			if (err != nil) != denied {
				raw, _ := json.Marshal(map[string]string{"case": name})
				t.Fatalf("private work admission %s: %v", raw, err)
			}
			if opened != nil {
				defer opened.Close()
				if name == "recovery" && len(opened.files) != 0 {
					t.Fatal("recovery obtained source bodies")
				}
			}
		})
	}
}
