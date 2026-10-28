package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// repositoryBundleAvailable advertises the specialized producer only for a project employee with an exact pinned human-each-effect bundle grant; discovery is not authority.
// repositoryBundleAvailable показывает specialized producer лишь project employee с exact pinned human-each-effect bundle grant; discovery не является authority.
func repositoryBundleAvailable(input runtimecontract.RunnerInput) bool {
	return runtimecontract.RuntimeRepositoryBundleAvailable(input)
}

// repositoryBundleGrant selects only the exact immutable grant and closed typed operation; it cannot select a foreign connection or turn a generic grant into bulk write authority.
// repositoryBundleGrant выбирает лишь exact immutable grant и closed typed operation; foreign connection либо generic grant не превращаются в bulk write authority.
func repositoryBundleGrant(input runtimecontract.RunnerInput, arguments map[string]any) (runtimecontract.RunnerIntegrationGrant, bool) {
	if !repositoryBundleAvailable(input) {
		return runtimecontract.RunnerIntegrationGrant{}, false
	}
	grant, ok := integrationGrantForCall(input, arguments)
	return grant, ok && grant.Operation == repositorybundle.Operation && grant.CapabilityKey == repositorybundle.Operation && grant.Risk == "WRITE" && grant.ApprovalPolicy == "HUMAN_EACH_EFFECT"
}

// repositoryBundleTool describes metadata-only preparation from already captured CLEAN archive/manifest refs; source bytes and credentials never enter its arguments.
// repositoryBundleTool описывает metadata-only preparation из уже captured CLEAN archive/manifest refs; source bytes и credentials в arguments не попадают.
func repositoryBundleTool() map[string]any {
	return map[string]any{"name": repositorybundle.ToolName, "description": "Prepare one complete typed JSON source container plus canonical manifest for owner approval, then end the turn and await the same-session decision. Read this grant's input_schema from get_integration_catalog and pass exactly those scalar pins as input. No source bytes, tokens, per-file calls or publish retries. Container canonical JSON: {version:1,manifest_sha256:<manifest SHA256>,files:[{path,mode,content_base64}]}; files follow the manifest order and content_base64 is standard padded Base64 of exact local bytes. Manifest canonical JSON: {version:1,total_bytes:<sum>,files:[{path,mode,bytes,sha256}]}, UTF8 path order, mode 100644 or 100755, lowercase SHA256. Canonical JSON has no whitespace, those exact field orders, and escapes <, >, &, U+2028/U+2029 as lowercase backslash-u sequences. Limits: 256 regular files, 1MiB decoded total, 1MiB encoded container, 128KiB manifest. Each decoded member must pass the server-owned GIT_REPOSITORY_SOURCE_ONLY format policy and mandatory exact-byte antivirus scanning. Valid UTF-8 HTML/SVG/XHTML and shebang source scripts are allowed only for Git publication, never for platform preview or execution; MZ/ELF executables, invalid text and unsupported binary files are rejected. Outer JSON CLEAN never certifies decoded members. Do not rename, omit or reencode quarantined members to bypass policy. If files were just created locally, first finish their platform capture and use exact artifact pins from the next immutable RuntimeRevision; never invent future artifact refs. If mode is READ_ONLY_REJOIN with SUCCEEDED, use the returned existing receipt/commit immediately: do not await a new gate/callback or repeat publication.",
		"inputSchema": objectSchema([]string{"grant_ref", "input"}, map[string]any{"grant_ref": opaqueRefSchema(), "input": map[string]any{"type": "object"}})}
}

// repositoryBundleInput carries the exact scalar package schema while keeping source bytes out of the model channel; strict decode rejects extras, fractions and overflow.
// repositoryBundleInput переносит exact scalar package schema без source bytes в model channel; strict decode отклоняет extras, fractions и overflow.
type repositoryBundleInput struct {
	ArchiveRef       string `json:"archive_ref"`
	ArchiveRevision  int64  `json:"archive_revision"`
	ArchiveVersion   int64  `json:"archive_version"`
	ArchiveSHA256    string `json:"archive_sha256"`
	ArchiveSize      int64  `json:"archive_size_bytes"`
	ManifestRef      string `json:"manifest_ref"`
	ManifestRevision int64  `json:"manifest_revision"`
	ManifestVersion  int64  `json:"manifest_version"`
	ManifestSHA256   string `json:"manifest_sha256"`
	ManifestSize     int64  `json:"manifest_size_bytes"`
	BaseCommitSHA    string `json:"base_commit_sha"`
}

// repositoryBundleToolResult exposes compact pending proposal/approval pins, not a completed Git write; later native continuation reports the actual owner receipt.
// repositoryBundleToolResult раскрывает compact pending proposal/approval pins, не completed Git write; последующее native continuation сообщает actual owner receipt.
type repositoryBundleToolResult struct {
	ProposalRef            string `json:"proposal_ref"`
	InvocationRef          string `json:"invocation_ref"`
	GateRef                string `json:"gate_ref"`
	ManifestSHA256         string `json:"manifest_sha256"`
	State                  string `json:"state"`
	FileCount              int32  `json:"file_count"`
	TotalBytes             int64  `json:"total_bytes"`
	Mode                   string `json:"mode,omitempty"`
	ReceiptRef             string `json:"receipt_ref,omitempty"`
	ReceiptDigest          string `json:"receipt_digest,omitempty"`
	CommitSHA              string `json:"commit_sha,omitempty"`
	TreeSHA                string `json:"tree_sha,omitempty"`
	ProposalBranch         string `json:"proposal_branch,omitempty"`
	ResultKey              string `json:"result_key,omitempty"`
	SemanticDigest         string `json:"semantic_digest,omitempty"`
	OriginalManifestSHA256 string `json:"original_manifest_sha256,omitempty"`
}

// prepareRepositoryBundle forwards exact leased origin and scalar artifact pins once, using a stable native-call key. Unknown ACK is never retried with new content, key, gate or external write.
// prepareRepositoryBundle однократно передаёт exact leased origin и scalar artifact pins со stable native-call key. Unknown ACK не повторяется с новыми content, key, Gate либо external write.
func (server *Server) prepareRepositoryBundle(ctx context.Context, input runtimecontract.RunnerInput, arguments map[string]any, callID json.RawMessage) (any, error) {
	grant, ok := repositoryBundleGrant(input, arguments)
	if !ok {
		return nil, status.Error(codes.PermissionDenied, "repository bundle grant is unavailable")
	}
	object, ok := arguments["input"].(map[string]any)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "repository bundle input is invalid")
	}
	raw, err := json.Marshal(object)
	if err != nil || len(raw) > 4<<10 {
		return nil, status.Error(codes.InvalidArgument, "repository bundle input is invalid")
	}
	var pins repositoryBundleInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&pins) != nil || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return nil, status.Error(codes.InvalidArgument, "repository bundle input is invalid")
	}
	request := &cp.PrepareRepositoryBundlePublicationRequest{LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, GrantRef: grant.Ref, IdempotencyKey: stableKey(input.LeaseRef, string(callID)), BaseCommitSha: pins.BaseCommitSHA,
		Archive:  &cp.RepositoryBundleArtifactPin{Ref: pins.ArchiveRef, Revision: pins.ArchiveRevision, Version: pins.ArchiveVersion, Sha256: pins.ArchiveSHA256, SizeBytes: pins.ArchiveSize},
		Manifest: &cp.RepositoryBundleArtifactPin{Ref: pins.ManifestRef, Revision: pins.ManifestRevision, Version: pins.ManifestVersion, Sha256: pins.ManifestSHA256, SizeBytes: pins.ManifestSize}}
	bounded, cancel := context.WithTimeout(ctx, server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.RepositoryBundles.PrepareRepositoryBundlePublication(bounded, request)
	if err != nil {
		return nil, status.Error(status.Code(err), "prepare repository bundle publication")
	}
	if response != nil && response.GetRejoin() != nil {
		return server.repositoryBundleRejoin(bounded, input, grant, request, response)
	}
	proposal := response.GetProposal()
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || proposal == nil || !repositorybundle.ValidContinuation(proposal.GetContinuationState(), proposal.GetContinuationCode(), proposal.GetContinuationNodeRef()) || len(proposal.ProtoReflect().GetUnknown()) != 0 || proposal.GetState() < cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_WAITING_APPROVAL || proposal.GetState() > cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_FAILED || proposal.GetVersion() < 1 || proposal.GetTotalBytes() < 0 || proposal.GetTotalBytes() > repositorybundle.MaximumContentBytes || proposal.GetProposalBranch() != "kodex/bundle/"+proposal.GetRef() || proposal.GetRef() == "" || proposal.GetGateRef() == "" || !safeInvocationRef(proposal.GetInvocationRef()) || proposal.GetOriginRunRef() != input.RunRef || proposal.GetOriginRuntimeRevisionRef() != input.RuntimeRevisionRef || proposal.GetGrantRef() != grant.Ref || proposal.GetConnectionRef() != grant.ConnectionRef || proposal.GetGrantVersion() != grant.GrantVersion || !protoBundlePinsEqual(proposal.GetArchive(), request.Archive) || !protoBundlePinsEqual(proposal.GetManifest(), request.Manifest) || proposal.GetBaseCommitSha() != pins.BaseCommitSHA || proposal.GetFileCount() < 1 || proposal.GetFileCount() > repositorybundle.MaximumFiles {
		return nil, status.Error(codes.Unavailable, "repository bundle receipt is invalid")
	}
	state := proposal.GetState().String()
	return repositoryBundleToolResult{ProposalRef: proposal.GetRef(), InvocationRef: proposal.GetInvocationRef(), GateRef: proposal.GetGateRef(), ManifestSHA256: pins.ManifestSHA256, State: state, FileCount: proposal.GetFileCount(), TotalBytes: proposal.GetTotalBytes()}, nil
}

// protoBundlePinsEqual requires every known pin field and rejects unknown fields, preventing a receipt from substituting another source revision or format.
// protoBundlePinsEqual требует все known pin fields и отклоняет unknown fields, не допуская подмены source revision либо format в receipt.
func protoBundlePinsEqual(a, b *cp.RepositoryBundleArtifactPin) bool {
	return a != nil && b != nil && len(a.ProtoReflect().GetUnknown()) == 0 && a.GetRef() == b.GetRef() && a.GetRevision() == b.GetRevision() && a.GetVersion() == b.GetVersion() && a.GetSha256() == b.GetSha256() && a.GetSizeBytes() == b.GetSizeBytes()
}
