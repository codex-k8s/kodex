package httptransport

import (
	"net/http"
	"path"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validSkillManifestPath(value string) bool {
	return value != "" && len(value) <= 512 && value != "." && !strings.ContainsAny(value, "\\:\x00\r\n") && !strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "../") && value != ".." && path.Clean(value) == value
}
func skillSpecificationInput(value generated.SkillBundleSpecification) (*controlplanev1.SkillBundleSpecification, bool) {
	if strings.TrimSpace(value.Name) == "" || len(value.Name) > 160 || len(value.Description) > 4000 || value.Files == nil || len(value.Files) > 128 {
		return nil, false
	}
	result := &controlplanev1.SkillBundleSpecification{Name: value.Name, Description: value.Description}
	seen := make(map[string]bool)
	for _, file := range value.Files {
		if !validSkillManifestPath(file.Path) || seen[file.Path] || !opaqueHTTPReference.MatchString(file.ArtifactRef) || !validManagedVersion(file.ArtifactRevision) {
			return nil, false
		}
		seen[file.Path] = true
		result.Files = append(result.Files, &controlplanev1.SkillBundleFileInput{Path: file.Path, ArtifactRef: file.ArtifactRef, ArtifactRevision: file.ArtifactRevision})
	}
	return result, true
}
func memorySpecificationInput(value generated.MemoryRecordSpecification) (*controlplanev1.MemoryRecordSpecification, bool) {
	retention := timestamppb.New(value.RetentionUntil)
	if strings.TrimSpace(value.Title) == "" || len(value.Title) > 160 || strings.TrimSpace(value.Summary) == "" || len(value.Summary) > 65536 || value.RetentionUntil.IsZero() || retention.CheckValid() != nil ||
		value.SourceRunRef != nil && !opaqueHTTPReference.MatchString(*value.SourceRunRef) {
		return nil, false
	}
	return &controlplanev1.MemoryRecordSpecification{Title: value.Title, Summary: value.Summary, SourceRunRef: stringValue(value.SourceRunRef), RetentionUntil: retention}, true
}
func requireContextCreateMutation(w http.ResponseWriter, key, etag, existingRef string) (*controlplanev1.MutationContext, bool) {
	if !httpIdempotencyKey.MatchString(key) || (existingRef == "") != (etag == "") {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return nil, false
	}
	if existingRef != "" {
		if !opaqueHTTPReference.MatchString(existingRef) {
			writeLocalProblem(w, 400, "INVALID_REQUEST", false)
			return nil, false
		}
		return requireVersionedMutation(w, key, etag)
	}
	return &controlplanev1.MutationContext{IdempotencyKey: key}, true
}

func (server *Server) CreateSkillBundleDraft(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateSkillBundleDraftParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.SkillBundleDraftCreateInput](w, r)
	if !ok {
		return
	}
	specification, ok := skillSpecificationInput(body.Specification)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireContextCreateMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), stringValue(body.BundleRef))
	if !ok {
		return
	}
	response, err := server.control.Command.CreateSkillBundleDraft(r.Context(), &controlplanev1.CreateSkillBundleDraftRequest{Mutation: mutation, ProjectRef: projectRef, BundleRef: stringValue(body.BundleRef), Specification: specification})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetBundle().GetProjectRef() != projectRef {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeSkillBundle(w, response.GetBundle(), stringValue(body.BundleRef), 201)
}
func (server *Server) SaveSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.SaveSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.SkillBundleSpecification](w, r)
	if !ok {
		return
	}
	specification, ok := skillSpecificationInput(body)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SaveSkillBundleDraft(r.Context(), &controlplanev1.SaveSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, Specification: specification})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetBundle().GetDraftRevision().GetRef() != revisionRef {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}
func (server *Server) CreateMemoryRecord(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateMemoryRecordParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.MemoryRecordCreateInput](w, r)
	if !ok {
		return
	}
	specification, ok := memorySpecificationInput(body.Specification)
	if !ok || body.AgentRef != nil && !opaqueHTTPReference.MatchString(*body.AgentRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireContextCreateMutation(w, p.IdempotencyKey, "", "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateMemoryRecord(r.Context(), &controlplanev1.CreateMemoryRecordRequest{Mutation: mutation, ProjectRef: projectRef, AgentRef: stringValue(body.AgentRef), Specification: specification})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetRecord().GetProjectRef() != projectRef || response.GetRecord().GetAgentRef() != stringValue(body.AgentRef) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), "", 201)
}
func (server *Server) ReviseMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.ReviseMemoryRecordParams) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.MemoryRecordSpecification](w, r)
	if !ok {
		return
	}
	specification, ok := memorySpecificationInput(body)
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ReviseMemoryRecord(r.Context(), &controlplanev1.ReviseMemoryRecordRequest{Mutation: mutation, RecordRef: recordRef, Specification: specification})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMemoryRecord(w, response.GetRecord(), recordRef, 201)
}

func (server *Server) ValidateSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.ValidateSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.ContextRevisionDigestInput](w, r)
	if !ok {
		return
	}
	if !validManagedDigest(body.ExpectedDigest) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.ValidateSkillBundleDraft(r.Context(), &controlplanev1.ValidateSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) ReviewSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.ReviewSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.SkillBundleReviewInput](w, r)
	if !ok {
		return
	}
	if !validManagedDigest(body.ExpectedDigest) || (body.Decision != "APPROVE" && body.Decision != "REJECT") || len(body.Comment) > 4000 {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	decision := controlplanev1.SkillReviewDecision_SKILL_REVIEW_DECISION_REJECT
	if body.Decision == "APPROVE" {
		decision = controlplanev1.SkillReviewDecision_SKILL_REVIEW_DECISION_APPROVE
	}
	response, err := server.control.Command.ReviewSkillBundleDraft(r.Context(), &controlplanev1.ReviewSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest, Decision: decision, Comment: body.Comment})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) PublishSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.PublishSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.ContextRevisionDigestInput](w, r)
	if !ok {
		return
	}
	if !validManagedDigest(body.ExpectedDigest) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.PublishSkillBundleDraft(r.Context(), &controlplanev1.PublishSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}

func (server *Server) DiscardSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.DiscardSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.ContextRevisionDigestInput](w, r)
	if !ok {
		return
	}
	if !validManagedDigest(body.ExpectedDigest) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.DiscardSkillBundleDraft(r.Context(), &controlplanev1.DiscardSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeSkillBundle(w, response.GetBundle(), bundleRef, 200)
}
