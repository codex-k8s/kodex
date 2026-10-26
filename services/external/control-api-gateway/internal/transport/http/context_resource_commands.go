package httptransport

import (
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// validSkillManifestPath permits bounded normalized relative paths with approved
// document/image suffixes, no hidden/space-padded components or traversal syntax.
// The root entry name is case-sensitive SKILL.md; no file is opened by this check.
// validSkillManifestPath допускает ограниченные нормализованные относительные
// пути документов/изображений без hidden/space-padded частей и traversal.
// Имя корневого входа — точное SKILL.md; эта проверка не открывает файлы.
func validSkillManifestPath(value string) bool {
	if value == "" || len(value) > 240 || !utf8.ValidString(value) || value == "." || strings.ContainsAny(value, "\\:\x00\r\n") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "../") || value == ".." || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if strings.TrimSpace(part) != part || strings.HasPrefix(part, ".") {
			return false
		}
	}
	if strings.EqualFold(value, "SKILL.md") {
		return value == "SKILL.md"
	}
	switch strings.ToLower(path.Ext(value)) {
	case ".md", ".txt", ".json", ".csv", ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

// skillSpecificationInput checks Unicode name/description bounds and a nonempty
// bounded file list, then validates artifact/version refs and case-folded path
// uniqueness. The required root SKILL.md must be present; scan/provenance are not
// accepted as editable input or synthesized here.
// skillSpecificationInput проверяет Unicode-пределы name/description и непустой
// ограниченный file list, затем artifact/version и уникальность путей без
// регистра. Корневой SKILL.md обязателен; scan/provenance не принимаются
// как редактируемые поля и здесь не выдумываются.
func skillSpecificationInput(value generated.SkillBundleSpecification) (*controlplanev1.SkillBundleSpecification, bool) {
	if strings.TrimSpace(value.Name) == "" || utf8.RuneCountInString(value.Name) > 160 || utf8.RuneCountInString(value.Description) > 2000 || !utf8.ValidString(value.Name+value.Description) || strings.ContainsRune(value.Name+value.Description, 0) || len(value.Files) == 0 || len(value.Files) > 128 {
		return nil, false
	}
	result := &controlplanev1.SkillBundleSpecification{Name: value.Name, Description: value.Description}
	seen := make(map[string]bool)
	for _, file := range value.Files {
		key := strings.ToLower(file.Path)
		if !validSkillManifestPath(file.Path) || seen[key] || !opaqueHTTPReference.MatchString(file.ArtifactRef) || !validManagedVersion(file.ArtifactRevision) {
			return nil, false
		}
		seen[key] = true
		result.Files = append(result.Files, &controlplanev1.SkillBundleFileInput{Path: file.Path, ArtifactRef: file.ArtifactRef, ArtifactRevision: file.ArtifactRevision})
	}
	return result, seen["skill.md"]
}

// memorySpecificationInput validates nonempty bounded title/summary, rejects NUL,
// requires nonzero protobuf-compatible retention and validates optional sourceRunRef.
// It copies accepted content unchanged and does not decide ownership or expiry policy.
// memorySpecificationInput проверяет непустые ограниченные title/summary без NUL,
// ненулевой совместимый с protobuf retention и optional sourceRunRef.
// Принятый текст копируется неизменно; ownership и expiry policy здесь не решаются.
func memorySpecificationInput(value generated.MemoryRecordSpecification) (*controlplanev1.MemoryRecordSpecification, bool) {
	retention := timestamppb.New(value.RetentionUntil)
	if strings.TrimSpace(value.Title) == "" || utf8.RuneCountInString(value.Title) > 160 || strings.ContainsRune(value.Title+value.Summary, 0) || strings.TrimSpace(value.Summary) == "" || len(value.Summary) > 65536 || value.RetentionUntil.IsZero() || retention.CheckValid() != nil ||
		value.SourceRunRef != nil && !opaqueHTTPReference.MatchString(*value.SourceRunRef) {
		return nil, false
	}
	return &controlplanev1.MemoryRecordSpecification{Title: value.Title, Summary: value.Summary, SourceRunRef: stringValue(value.SourceRunRef), RetentionUntil: retention}, true
}

// requireContextCreateMutation requires an idempotency key and matching presence
// of existingRef/If-Match. A new resource carries no expected version; an existing
// bundle delegates to the ordinary exact versioned-header guard.
// requireContextCreateMutation требует idempotency key и согласованное наличие
// existingRef/If-Match. Новый ресурс не несёт expected version; существующий
// bundle использует обычную точную проверку versioned-заголовков.
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

// CreateSkillBundleDraft binds the project context, validates editable content
// and create-versus-existing OCC, then calls the specialized owner command.
// The returned bundle must belong to the requested project before HTTP201/ETag.
// CreateSkillBundleDraft связывает project context, проверяет содержимое
// и create/existing OCC, затем вызывает отдельную owner-команду.
// Ответ bundle должен принадлежать запрошенному проекту до HTTP201/ETag.
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

// SaveSkillBundleDraft checks exact bundle/revision, content and versioned headers
// before its typed owner command. The response must retain the requested draft
// revision and bundle, preventing a substituted revision from being returned.
// SaveSkillBundleDraft проверяет точные bundle/revision, содержимое и versioned
// headers до typed owner-команды. Ответ обязан сохранить запрошенные draft
// revision и bundle, исключая выдачу подменённой ревизии.
func (server *Server) SaveSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.SaveSkillBundleDraftParams) {
	if !opaqueHTTPReference.MatchString(bundleRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	specification, ok := decodeContextSpecification(w, r, skillSpecificationInput)
	if !ok {
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

// CreateMemoryRecord validates project context, editable memory content, optional
// agent and create idempotency before the owner command. Both project and optional
// agent in the returned record must match the request before a201 view is written.
// CreateMemoryRecord проверяет project context, память, optional agent и create
// idempotency до owner-команды. Проект и optional agent ответа обязаны совпасть
// с запросом до выдачи представления201.
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

// ReviseMemoryRecord validates resource, editable specification and exact mutation
// headers before asking the owner for a new revision; the returned record is
// checked against the original ref rather than trusting the RPC body blindly.
// ReviseMemoryRecord проверяет ресурс, спецификацию и точные mutation headers
// до запроса новой ревизии владельцу; ответ проверяется против исходного ref,
// а не принимается без проверки по одному успеху RPC.
func (server *Server) ReviseMemoryRecord(w http.ResponseWriter, r *http.Request, recordRef generated.MemoryRecordRef, p generated.ReviseMemoryRecordParams) {
	if !opaqueHTTPReference.MatchString(recordRef) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	specification, ok := decodeContextSpecification(w, r, memorySpecificationInput)
	if !ok {
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ReviseMemoryRecord(r.Context(), &controlplanev1.ReviseMemoryRecordRequest{Mutation: mutation, RecordRef: recordRef, Specification: specification})
	writeMemoryRecordRPCResult(w, response, err, recordRef, 201)
}

// ValidateSkillBundleDraft reuses the exact digest-mutation input guard and
// calls only the specialized validation RPC. It never marks a revision CLEAN
// locally or turns the submitted digest into admission authority.
// ValidateSkillBundleDraft использует общий точный digest-mutation guard
// и отдельный validation RPC. Он не назначает CLEAN локально и не превращает
// присланный digest в полномочие допуска.
func (server *Server) ValidateSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.ValidateSkillBundleDraftParams) {
	body, mutation, ok := requireSkillRevisionDigestMutation(w, r, bundleRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.ValidateSkillBundleDraft(r.Context(), &controlplanev1.ValidateSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	writeSkillBundleRPCResult(w, response, err, bundleRef, 200)
}

// ReviewSkillBundleDraft accepts only APPROVE/REJECT, a valid expected digest
// and a bounded comment before versioned owner review; the explicit enum mapping
// does not replace owner permission, scan or lifecycle guards.
// ReviewSkillBundleDraft допускает только APPROVE/REJECT, верный expected digest
// и ограниченный comment до versioned owner review; явное enum-преобразование
// не заменяет owner permission, scan или lifecycle-проверки.
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
	writeSkillBundleRPCResult(w, response, err, bundleRef, 200)
}

// PublishSkillBundleDraft validates the same resource/revision/body/header pins
// as validation but invokes the distinct publish RPC; all scan/review eligibility
// remains an owner decision, and the resulting bundle view is checked afterward.
// PublishSkillBundleDraft проверяет те же resource/revision/body/header pins,
// но вызывает отдельный publish RPC; допустимость scan/review решает владелец,
// а итоговое представление bundle затем проверяется.
func (server *Server) PublishSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.PublishSkillBundleDraftParams) {
	body, mutation, ok := requireSkillRevisionDigestMutation(w, r, bundleRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.PublishSkillBundleDraft(r.Context(), &controlplanev1.PublishSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	writeSkillBundleRPCResult(w, response, err, bundleRef, 200)
}

// DiscardSkillBundleDraft validates exact revision digest and mutation headers
// before its separate discard command, preserving that command's identity and
// returning only a validated bundle matching the requested resource.
// DiscardSkillBundleDraft проверяет точные revision digest и mutation headers
// до отдельной discard-команды, сохраняя её identity и выдавая только
// проверенный bundle, соответствующий запрошенному ресурсу.
func (server *Server) DiscardSkillBundleDraft(w http.ResponseWriter, r *http.Request, bundleRef generated.SkillBundleRef, revisionRef generated.ContextRevisionRef, p generated.DiscardSkillBundleDraftParams) {
	body, mutation, ok := requireSkillRevisionDigestMutation(w, r, bundleRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}

	response, err := server.control.Command.DiscardSkillBundleDraft(r.Context(), &controlplanev1.DiscardSkillBundleDraftRequest{Mutation: mutation, BundleRef: bundleRef, RevisionRef: revisionRef, ExpectedDigest: body.ExpectedDigest})
	writeSkillBundleRPCResult(w, response, err, bundleRef, 200)
}
