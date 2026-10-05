package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/role_images_risk_actor.sql
var queryRoleImageRiskActor string

//go:embed sql/role_images_risk_report.sql
var queryRoleImageRiskReport string

//go:embed sql/role_images_risk_store_report.sql
var queryRoleImageRiskStoreReport string

//go:embed sql/role_images_risk_store_decision.sql
var queryRoleImageRiskStoreDecision string

//go:embed sql/role_images_risk_insert_attempt.sql
var queryRoleImageRiskInsertAttempt string

//go:embed sql/role_images_risk_restart_admission.sql
var queryRoleImageRiskRestartAdmission string

//go:embed sql/role_images_risk_history_view.sql
var queryRoleImageRiskHistoryView string

func (repository *Repository) hydrateImageRiskHistory(ctx context.Context, querier roleImageQuerier, current scope, artifact *entity.ImageArtifact) error {
	var attemptJSON []byte
	var decisionJSON string
	if err := querier.QueryRow(ctx, queryRoleImageRiskHistoryView, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_ref": artifact.Ref}).Scan(&attemptJSON, &decisionJSON); err != nil {
		return errs.ErrUnavailable
	}
	if err := json.Unmarshal(attemptJSON, &artifact.AdmissionAttempt); err != nil {
		return errs.ErrConflict
	}
	decision, err := decodeImageRiskDecision(decisionJSON)
	if err != nil {
		return err
	}
	if decision != nil && (decision.ArtifactRef != artifact.Ref || decision.OrganizationRef != artifact.OrganizationRef || decision.ProjectRef != artifact.ProjectRef || decision.ScopeKind != artifact.ScopeKind) {
		return errs.ErrConflict
	}
	artifact.RiskDecision = decision
	return nil
}

func (repository *Repository) requireImageRiskAuthority(ctx context.Context, tx pgx.Tx, current scope, principal value.Principal, scopeKind, projectRef, recipeRef string) (resolvedAccessTarget, error) {
	if principal.CallerWorkload != "control-api-gateway" || principal.ProjectRef != "" || current.authorityProjectID != "" {
		return resolvedAccessTarget{}, errs.ErrNotFound
	}
	var role string
	if err := tx.QueryRow(ctx, queryRoleImageRiskActor, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID}).Scan(&role); errors.Is(err, pgx.ErrNoRows) {
		return resolvedAccessTarget{}, errs.ErrNotFound
	} else if err != nil {
		return resolvedAccessTarget{}, errs.ErrUnavailable
	}
	if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
		return resolvedAccessTarget{}, err
	}
	target, err := repository.resolveScopedRoleImageAccessTarget(ctx, tx, current, recipeRef, projectRef, scopeKind)
	if err != nil {
		return resolvedAccessTarget{}, err
	}
	return target, nil
}

func (repository *Repository) readImageVulnerabilityReport(ctx context.Context, tx pgx.Tx, current scope, artifact lockedArtifact) (entity.ImageVulnerabilityReport, error) {
	result := entity.ImageVulnerabilityReport{Artifact: artifact.Artifact, ArtifactVersion: artifact.Artifact.Version, AdmissionRevision: artifact.Artifact.AdmissionRevision}
	var raw string
	var sourceVersion uint64
	err := tx.QueryRow(ctx, queryRoleImageRiskReport, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": artifact.ID, "admission_revision": artifact.Artifact.AdmissionRevision}).Scan(&raw, &result.ProjectionSHA256, &sourceVersion, &result.AdmissionReceiptSHA256, &result.EvidenceManifestDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		result.NextActions = []string{"REBUILD_FOR_REPORT"}
		return result, nil
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	report, err := runtimecontract.DecodeImageVulnerabilityReport([]byte(raw))
	if err != nil || imageReportDigest(raw) != result.ProjectionSHA256 || !imageVulnerabilityReportMatchesArtifact(report, artifact.Artifact) {
		return entity.ImageVulnerabilityReport{}, errs.ErrConflict
	}
	result.Available = true
	result.Report = report
	if artifact.AdmissionState == "REJECTED" && report.BlockingMatchCount > 0 {
		result.NextActions = []string{"ACCEPT_RISK", "REJECT_RISK"}
	}
	return result, nil
}

func imageReportDigest(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func imageVulnerabilityReportMatchesArtifact(report runtimecontract.ImageVulnerabilityReport, a entity.ImageArtifact) bool {
	return report.ArtifactRef == a.Ref && report.ImageDigest == a.ManifestDigest && report.ReportSHA256 == a.VulnerabilityEvidenceSHA256 && report.SBOMSHA256 == a.SBOMSHA256 &&
		report.ScopeKind == a.ScopeKind && report.OrganizationRef == a.OrganizationRef && report.ProjectRef == a.ProjectRef &&
		report.RecipeRef == a.RecipeRef && report.RecipeVersion == a.RecipeVersion && report.RecipeGeneration == a.RecipeGeneration &&
		report.BuildRef == a.BuildRef && report.BuildVersion == a.BuildVersion && report.BuildAttempt == a.BuildAttempt && report.PolicyRevision == a.PolicyRevision && report.PolicySHA256 == a.PolicySHA256
}

func (repository *Repository) GetVulnerabilityReport(ctx context.Context, principal value.Principal, filter roleimagerepo.VulnerabilityReportFilter) (entity.ImageVulnerabilityReport, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.ImageVulnerabilityReport{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := repository.requireImageRiskAuthority(ctx, tx, current, principal, filter.ScopeKind, filter.ProjectRef, filter.RecipeRef); err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	a, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact, current.organizationID, filter.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageVulnerabilityReport{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageVulnerabilityReport{}, errs.ErrUnavailable
	}
	if a.Artifact.RecipeRef != filter.RecipeRef || a.Artifact.ScopeKind != filter.ScopeKind || a.Artifact.ProjectRef != filter.ProjectRef {
		return entity.ImageVulnerabilityReport{}, errs.ErrNotFound
	}
	result, err := repository.readImageVulnerabilityReport(ctx, tx, current, a)
	if err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	if result.Available {
		var eligible bool
		if err := tx.QueryRow(ctx, queryRoleImagesAdmissionCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": a.ID}).Scan(&eligible); err != nil {
			return entity.ImageVulnerabilityReport{}, errs.ErrUnavailable
		}
		if !eligible || a.Artifact.PolicyRevision != repository.roleImages.PolicyRevision || a.Artifact.PolicySHA256 != repository.roleImages.PolicySHA256 {
			result.NextActions = nil
		}
		if filter.ExpectedReportSHA256 != "" && filter.ExpectedReportSHA256 != result.Report.ReportSHA256 {
			return entity.ImageVulnerabilityReport{}, errs.ErrConflict
		}
		if err := repository.pageImageVulnerabilityReport(&result, current, filter); err != nil {
			return entity.ImageVulnerabilityReport{}, err
		}
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageVulnerabilityReport{}, err
	}
	return result, nil
}

func (repository *Repository) pageImageVulnerabilityReport(result *entity.ImageVulnerabilityReport, current scope, filter roleimagerepo.VulnerabilityReportFilter) error {
	page := filter.Page
	filter.Page.Token = ""
	filterDigest := roleImageDigest(struct {
		Actor, Organization, Projection string
		Revision                        uint64
		Filter                          roleimagerepo.VulnerabilityReportFilter
	}{current.actorRef, current.organizationRef, result.ProjectionSHA256, result.AdmissionRevision, filter})
	filtered := make([]runtimecontract.ImageVulnerabilityFinding, 0, len(result.Report.Findings))
	for _, finding := range result.Report.Findings {
		if filter.PackageQuery != "" && !strings.Contains(strings.ToLower(finding.PackageName), strings.ToLower(filter.PackageQuery)) || filter.AdvisoryQuery != "" && !strings.Contains(strings.ToLower(finding.AdvisoryID), strings.ToLower(filter.AdvisoryQuery)) || filter.Severity != "" && finding.Severity != filter.Severity || filter.BlockingOnly != nil && finding.Blocking != *filter.BlockingOnly {
			continue
		}
		filtered = append(filtered, finding)
	}
	result.Total = int64(len(filtered))
	offset := 0
	if page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(page.Token)
		if err != nil || len(raw) > 128 {
			return errs.ErrInvalid
		}
		parts := strings.Split(string(raw), ".")
		if len(parts) != 2 {
			return errs.ErrInvalid
		}
		offset, err = strconv.Atoi(parts[0])
		if err != nil || offset < 1 || offset > len(filtered) || strconv.Itoa(offset) != parts[0] || !hmac.Equal([]byte(parts[1]), []byte(repository.imageReportCursorMAC(filterDigest, parts[0]))) {
			return errs.ErrConflict
		}
	}
	size := int(page.Size)
	if size == 0 {
		size = 50
	}
	if size < 1 || size > 100 {
		return errs.ErrInvalid
	}
	end := min(offset+size, len(filtered))
	result.Findings = filtered[offset:end]
	if end < len(filtered) {
		number := strconv.Itoa(end)
		result.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(number + "." + repository.imageReportCursorMAC(filterDigest, number)))
	}
	return nil
}

func (repository *Repository) imageReportCursorMAC(filterDigest, offset string) string {
	mac := hmac.New(sha256.New, repository.roleImages.LeaseSigningKey)
	_, _ = mac.Write([]byte("image-vulnerability-report\x00" + filterDigest + "\x00" + offset))
	return hex.EncodeToString(mac.Sum(nil))
}

func (repository *Repository) DecideAdmissionRisk(ctx context.Context, input roleimagerepo.AdmissionRiskInput) (entity.ImageAdmissionRiskResult, error) {
	return retryRoleImageTransaction(ctx, func() (entity.ImageAdmissionRiskResult, error) { return repository.decideAdmissionRisk(ctx, input) })
}

func (repository *Repository) decideAdmissionRisk(ctx context.Context, input roleimagerepo.AdmissionRiskInput) (entity.ImageAdmissionRiskResult, error) {
	current, err := repository.resolveScope(ctx, input.Principal)
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	target, err := repository.requireImageRiskAuthority(ctx, tx, current, input.Principal, input.ScopeKind, input.ProjectRef, input.RecipeRef)
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	recipe, err := scanLockedRecipe(tx.QueryRow(ctx, queryRoleImagesLockRecipe, current.organizationID, input.RecipeRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageAdmissionRiskResult{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	a, err := scanLockedArtifact(tx.QueryRow(ctx, queryRoleImagesLockArtifact, current.organizationID, input.ArtifactRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.ImageAdmissionRiskResult{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	if a.Artifact.RecipeRef != input.RecipeRef || a.Artifact.ScopeKind != input.ScopeKind || a.Artifact.ProjectRef != input.ProjectRef {
		return entity.ImageAdmissionRiskResult{}, errs.ErrNotFound
	}
	var active bool
	if err := tx.QueryRow(ctx, queryRoleImagesAdmissionCurrent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": a.ID}).Scan(&active); err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	if !active || recipe.Recipe.State != "ACTIVE" || a.Artifact.PolicyRevision != repository.roleImages.PolicyRevision || a.Artifact.PolicySHA256 != repository.roleImages.PolicySHA256 {
		return entity.ImageAdmissionRiskResult{}, errs.ErrConflict
	}
	operation := input.Mutation.Operation
	intentInput := input
	intentInput.Principal = value.Principal{}
	intentInput.Mutation.IntentDigest = ""
	intent := roleImageDigest(intentInput)
	if err := repository.lockRoleImageIdempotency(ctx, tx, current, operation, input.Mutation.IdempotencyKey); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	var replay entity.ImageAdmissionRiskResult
	if found, err := repository.loadRoleImageReceipt(ctx, tx, current, operation, input.Mutation.IdempotencyKey, intent, &replay); err != nil {
		return replay, err
	} else if found {
		// Receipt хранит исходный outcome; текущий owner/policy проверен перед ним.
		if replay.Decision.ArtifactRef != a.Artifact.Ref || replay.Decision.RecipeVersion != recipe.Recipe.Version || replay.Decision.RecipeGeneration != recipe.Recipe.Generation || replay.Decision.ManifestDigest != a.Artifact.ManifestDigest || replay.Decision.VulnerabilityEvidenceSHA256 != a.Artifact.VulnerabilityEvidenceSHA256 || replay.Decision.AdmissionRevision != a.Artifact.AdmissionRevision || replay.Decision.BuildRef != a.Artifact.BuildRef || replay.Decision.BuildAttempt != a.Artifact.BuildAttempt || replay.Decision.PolicyRevision != a.Artifact.PolicyRevision || replay.Decision.PolicySHA256 != a.Artifact.PolicySHA256 {
			return entity.ImageAdmissionRiskResult{}, errs.ErrConflict
		}
		replay.Artifact = a.Artifact
		if err := repository.hydrateImageRiskHistory(ctx, tx, current, &replay.Artifact); err != nil {
			return entity.ImageAdmissionRiskResult{}, err
		}
		if replay.Decision.Action == "ACCEPT_RISK" {
			replay.AdmissionAttempt = replay.Artifact.AdmissionAttempt
		}
		if err := committed(tx, ctx); err != nil {
			return entity.ImageAdmissionRiskResult{}, err
		}
		return replay, nil
	}
	if a.Artifact.Version != input.ExpectedArtifactVersion || recipe.Recipe.Version != input.ExpectedRecipeVersion {
		return entity.ImageAdmissionRiskResult{}, errs.ErrVersionMismatch
	}
	if a.AdmissionState != "REJECTED" || a.Artifact.AdmissionVerdict != "REJECTED" || a.Artifact.AdmissionRevision != input.ExpectedAdmissionRevision || a.Artifact.RecipeGeneration != input.ExpectedRecipeGeneration || a.Artifact.BuildRef != input.ExpectedBuildRef || a.Artifact.BuildAttempt != input.ExpectedBuildAttempt || a.Artifact.ManifestDigest != input.ManifestDigest || a.Artifact.VulnerabilityEvidenceSHA256 != input.VulnerabilityEvidenceSHA256 || a.Artifact.AdmissionReceiptSHA256 != input.PriorAdmissionReceiptSHA256 || a.Artifact.AdmissionReceiptOCIManifestDigest != input.PriorEvidenceManifestDigest || a.Artifact.PolicyRevision != input.PolicyRevision || a.Artifact.PolicySHA256 != input.PolicySHA256 {
		return entity.ImageAdmissionRiskResult{}, errs.ErrConflict
	}
	report, err := repository.readImageVulnerabilityReport(ctx, tx, current, a)
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	if !report.Available || report.ProjectionSHA256 != input.ProjectionSHA256 || report.Report.BlockingMatchCount == 0 {
		return entity.ImageAdmissionRiskResult{}, errs.ErrConflict
	}
	ref, err := newRef("imgrisk")
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	var decidedAt time.Time
	if err := tx.QueryRow(ctx, queryImageRiskServerTime).Scan(&decidedAt); err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrUnavailable
	}
	d := entity.ImageAdmissionRiskDecision{Ref: ref, ArtifactRef: a.Artifact.Ref, RecipeRef: a.Artifact.RecipeRef, ScopeKind: a.Artifact.ScopeKind, OrganizationRef: a.Artifact.OrganizationRef, ProjectRef: a.Artifact.ProjectRef, ActorRef: current.actorRef, Action: input.Action, Reason: input.Reason, ArtifactVersion: a.Artifact.Version, AdmissionRevision: a.Artifact.AdmissionRevision, RecipeVersion: a.Artifact.RecipeVersion, RecipeGeneration: a.Artifact.RecipeGeneration, BuildRef: a.Artifact.BuildRef, BuildVersion: a.Artifact.BuildVersion, BuildAttempt: a.Artifact.BuildAttempt, ManifestDigest: a.Artifact.ManifestDigest, VulnerabilityEvidenceSHA256: a.Artifact.VulnerabilityEvidenceSHA256, ProjectionSHA256: report.ProjectionSHA256, PriorAdmissionReceiptSHA256: a.Artifact.AdmissionReceiptSHA256, PriorEvidenceManifestDigest: a.Artifact.AdmissionReceiptOCIManifestDigest, PolicyRevision: a.Artifact.PolicyRevision, PolicySHA256: a.Artifact.PolicySHA256, SBOMSHA256: a.Artifact.SBOMSHA256, DecidedAt: decidedAt}
	riskJSON, riskSHA, err := canonicalImageRiskAcceptance(d, report.Report)
	if err != nil {
		return entity.ImageAdmissionRiskResult{}, errs.ErrConflict
	}
	d.BindingSHA256 = riskSHA
	decisionJSON := string(asJSON(d))
	storedRiskJSON, storedRiskSHA := "", ""
	if input.Action == "ACCEPT_RISK" {
		storedRiskJSON, storedRiskSHA = riskJSON, riskSHA
	}
	var decisionID string
	if err := tx.QueryRow(ctx, queryRoleImageRiskStoreDecision, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "artifact_id": a.ID, "admission_revision": a.Artifact.AdmissionRevision, "actor_id": current.actorID, "action": d.Action, "reason": d.Reason, "decision_json": decisionJSON, "decision_sha256": imageReportDigest(decisionJSON), "risk_json": storedRiskJSON, "risk_sha256": storedRiskSHA, "decided_at": decidedAt}).Scan(&decisionID); err != nil {
		return entity.ImageAdmissionRiskResult{}, mapRoleImageWriteError(err)
	}
	result := entity.ImageAdmissionRiskResult{Decision: d, Artifact: a.Artifact}
	result.Artifact.RiskDecision = &result.Decision
	if input.Action == "ACCEPT_RISK" {
		attempt, err := repository.insertImageAdmissionAttempt(ctx, tx, current, a.Artifact, a.ID, decisionID, d.Ref, "PENDING")
		if err != nil {
			return entity.ImageAdmissionRiskResult{}, err
		}
		result.AdmissionAttempt = &attempt
		result.Artifact.AdmissionAttempt = &attempt
		if err := tx.QueryRow(ctx, queryRoleImageRiskRestartAdmission, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": a.ID, "version": a.Artifact.Version}).Scan(&result.Artifact.Version, &result.Artifact.UpdatedAt); err != nil {
			return entity.ImageAdmissionRiskResult{}, mapRoleImageWriteError(err)
		}
		result.Artifact.AdmissionVerdict = ""
		result.Artifact.PromotionState = "REJECTED"
		result.Artifact.PromotionRequested = false
	}
	if err := repository.auditRoleImage(ctx, tx, current, target.projectID, operation, "ROLE_IMAGE", a.Artifact.RecipeRef, input.Action); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	if err := repository.storeRoleImageReceipt(ctx, tx, current, operation, input.Mutation.IdempotencyKey, intent, "IMAGE_ADMISSION_RISK_DECISION", result); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "ROLE_IMAGE_RECIPE_CHANGED", a.Artifact.ProjectRef, a.Artifact.RecipeRef, "i18n:ROLE_IMAGE_RECIPE_CHANGED", int64(recipe.Recipe.Version), input.Action); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	if err := committed(tx, ctx); err != nil {
		return entity.ImageAdmissionRiskResult{}, err
	}
	return result, nil
}

//go:embed sql/role_images_risk_server_time.sql
var queryImageRiskServerTime string

func (repository *Repository) insertImageAdmissionAttempt(ctx context.Context, tx pgx.Tx, current scope, artifact entity.ImageArtifact, artifactID, decisionID, decisionRef, state string) (entity.ImageAdmissionAttempt, error) {
	ref, err := newRef("imgadm")
	if err != nil {
		return entity.ImageAdmissionAttempt{}, errs.ErrUnavailable
	}
	result := entity.ImageAdmissionAttempt{Ref: ref, ArtifactRef: artifact.Ref, RiskDecisionRef: decisionRef, State: state, Version: 1, SourceAdmissionRevision: artifact.AdmissionRevision}
	if err := tx.QueryRow(ctx, queryRoleImageRiskInsertAttempt, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "artifact_id": artifactID, "risk_decision_id": decisionID, "source_admission_revision": artifact.AdmissionRevision, "source_receipt_sha256": artifact.AdmissionReceiptSHA256, "source_evidence_manifest_digest": artifact.AdmissionReceiptOCIManifestDigest, "source_artifact_json": asJSON(artifact), "state": state}).Scan(&result.Number, &result.CreatedAt); err != nil {
		return entity.ImageAdmissionAttempt{}, mapRoleImageWriteError(err)
	}
	return result, nil
}

//go:embed sql/role_images_risk_get_attempt.sql
var queryRoleImageRiskGetAttempt string

//go:embed sql/role_images_risk_claim_attempt.sql
var queryRoleImageRiskClaimAttempt string

type imageAdmissionAttemptSnapshot struct {
	Attempt                                                        entity.ImageAdmissionAttempt
	DecisionJSON, RiskJSON, RiskSHA, SourceReceipt, SourceEvidence string
}

func (repository *Repository) readImageAdmissionAttempt(ctx context.Context, tx pgx.Tx, current scope, artifactID, artifactRef string) (imageAdmissionAttemptSnapshot, error) {
	var result imageAdmissionAttemptSnapshot
	result.Attempt.ArtifactRef = artifactRef
	err := tx.QueryRow(ctx, queryRoleImageRiskGetAttempt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": artifactID}).Scan(&result.Attempt.Ref, &result.Attempt.Number, &result.Attempt.State, &result.Attempt.Version, &result.Attempt.Fence, &result.Attempt.RiskDecisionRef, &result.DecisionJSON, &result.RiskJSON, &result.RiskSHA, &result.Attempt.SourceAdmissionRevision, &result.SourceReceipt, &result.SourceEvidence, &result.Attempt.CreatedAt)
	return result, err
}

func (repository *Repository) ensureImageAdmissionAttempt(ctx context.Context, tx pgx.Tx, current scope, artifact entity.ImageArtifact, artifactID string, fence uint64) (entity.ImageAdmissionAttempt, string, string, string, string, error) {
	stored, err := repository.readImageAdmissionAttempt(ctx, tx, current, artifactID, artifact.Ref)
	if errors.Is(err, pgx.ErrNoRows) {
		attempt, err := repository.insertImageAdmissionAttempt(ctx, tx, current, artifact, artifactID, "", "", "PENDING")
		if err != nil {
			return entity.ImageAdmissionAttempt{}, "", "", "", "", err
		}
		stored.Attempt = attempt
	} else if err != nil {
		return entity.ImageAdmissionAttempt{}, "", "", "", "", errs.ErrUnavailable
	}
	if stored.Attempt.State != "PENDING" {
		return entity.ImageAdmissionAttempt{}, "", "", "", "", errs.ErrConflict
	}
	if stored.RiskJSON != "" {
		risk, err := runtimecontract.DecodeImageRiskAcceptance([]byte(stored.RiskJSON))
		if err != nil || imageReportDigest(stored.RiskJSON) != stored.RiskSHA {
			return entity.ImageAdmissionAttempt{}, "", "", "", "", errs.ErrConflict
		}
		locked := lockedArtifact{ID: artifactID, Artifact: artifact}
		report, err := repository.readImageVulnerabilityReport(ctx, tx, current, locked)
		if err != nil || !report.Available || !runtimecontract.ImageRiskAcceptanceMatchesReport(risk, report.Report) {
			return entity.ImageAdmissionAttempt{}, "", "", "", "", errs.ErrConflict
		}
	}
	command, err := tx.Exec(ctx, queryRoleImageRiskClaimAttempt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "artifact_id": artifactID, "ref": stored.Attempt.Ref, "number": stored.Attempt.Number, "fence": fence})
	if err != nil {
		return entity.ImageAdmissionAttempt{}, "", "", "", "", mapRoleImageWriteError(err)
	}
	if command.RowsAffected() != 1 {
		return entity.ImageAdmissionAttempt{}, "", "", "", "", errs.ErrConflict
	}
	stored.Attempt.State = "CLAIMED"
	stored.Attempt.Version++
	stored.Attempt.Fence = fence
	return stored.Attempt, stored.RiskJSON, stored.RiskSHA, stored.SourceReceipt, stored.SourceEvidence, nil
}

func (repository *Repository) matchImageAdmissionAttempt(ctx context.Context, tx pgx.Tx, current scope, artifactID, ref string, number uint32, state string) error {
	if ref == "" || number == 0 {
		return errs.ErrForbidden
	}
	stored, err := repository.readImageAdmissionAttempt(ctx, tx, current, artifactID, "")
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	if stored.Attempt.Ref != ref || stored.Attempt.Number != number || stored.Attempt.State != state {
		return errs.ErrForbidden
	}
	return nil
}

func (repository *Repository) validateAdmissionVulnerabilityReport(ctx context.Context, tx pgx.Tx, current scope, locked lockedArtifact, input roleimagerepo.AdmissionRecordInput) error {
	report, err := runtimecontract.DecodeImageVulnerabilityReport([]byte(input.VulnerabilityReportJSON))
	artifact := locked.Artifact
	artifact.VulnerabilityEvidenceSHA256 = input.VulnerabilityEvidenceSHA256
	artifact.SBOMSHA256 = input.SBOMSHA256
	if err != nil || imageReportDigest(input.VulnerabilityReportJSON) != input.VulnerabilityReportProjectionSHA256 || !imageVulnerabilityReportMatchesArtifact(report, artifact) {
		return errs.ErrInvalid
	}
	stored, err := repository.readImageAdmissionAttempt(ctx, tx, current, locked.ID, locked.Artifact.Ref)
	if err != nil {
		return errs.ErrUnavailable
	}
	if stored.RiskSHA != input.RiskAcceptanceSHA256 {
		return errs.ErrForbidden
	}
	if stored.RiskJSON != "" {
		risk, err := runtimecontract.DecodeImageRiskAcceptance([]byte(stored.RiskJSON))
		if err != nil || !runtimecontract.ImageRiskAcceptanceMatchesReport(risk, report) || imageReportDigest(stored.RiskJSON) != stored.RiskSHA {
			return errs.ErrForbidden
		}
	} else if input.Verdict == "ACCEPTED" && report.BlockingMatchCount > 0 {
		return errs.ErrForbidden
	}
	return nil
}

func (repository *Repository) storeAdmissionVulnerabilityReport(ctx context.Context, tx pgx.Tx, current scope, artifactID string, artifact entity.ImageArtifact, input roleimagerepo.AdmissionRecordInput) error {
	_, err := tx.Exec(ctx, queryRoleImageRiskStoreReport, pgx.StrictNamedArgs{"artifact_id": artifactID, "admission_revision": artifact.AdmissionRevision, "organization_id": current.organizationID, "projection_json": input.VulnerabilityReportJSON, "projection_sha256": input.VulnerabilityReportProjectionSHA256, "artifact_version": artifact.Version, "receipt_sha256": artifact.AdmissionReceiptSHA256, "evidence_manifest_digest": artifact.AdmissionReceiptOCIManifestDigest})
	if err != nil {
		return mapRoleImageWriteError(err)
	}
	return nil
}

func decodeImageRiskDecision(raw string) (*entity.ImageAdmissionRiskDecision, error) {
	if raw == "" {
		return nil, nil
	}
	var d entity.ImageAdmissionRiskDecision
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, errs.ErrConflict
	}
	return &d, nil
}

func canonicalImageRiskAcceptance(d entity.ImageAdmissionRiskDecision, report runtimecontract.ImageVulnerabilityReport) (string, string, error) {
	if d.Action == "REJECT_RISK" {
		raw := string(asJSON(d))
		return "", imageReportDigest(raw), nil
	}
	value := runtimecontract.ImageRiskAcceptance{Schema: runtimecontract.ImageRiskAcceptanceSchema, DecisionRef: d.Ref, DecisionVersion: 1, Action: d.Action, ScopeKind: d.ScopeKind, OrganizationRef: d.OrganizationRef, ProjectRef: d.ProjectRef, ArtifactRef: d.ArtifactRef, ImageDigest: d.ManifestDigest, ReportSHA256: d.VulnerabilityEvidenceSHA256, ProjectionSHA256: d.ProjectionSHA256, SourceAdmissionRevision: d.AdmissionRevision, SourceAdmissionReceiptSHA256: d.PriorAdmissionReceiptSHA256, SourceEvidenceManifestDigest: d.PriorEvidenceManifestDigest, RecipeRef: d.RecipeRef, RecipeVersion: d.RecipeVersion, RecipeGeneration: d.RecipeGeneration, BuildRef: d.BuildRef, BuildVersion: d.BuildVersion, BuildAttempt: d.BuildAttempt, PolicyRevision: d.PolicyRevision, PolicySHA256: d.PolicySHA256, Reason: d.Reason, DecidedByActorRef: d.ActorRef, DecidedAt: d.DecidedAt.UTC().Format(time.RFC3339Nano)}
	raw, err := runtimecontract.CanonicalImageRiskAcceptance(value)
	if err != nil || !runtimecontract.ImageRiskAcceptanceMatchesReport(value, report) {
		return "", "", errs.ErrConflict
	}
	return string(raw), imageReportDigest(string(raw)), nil
}
