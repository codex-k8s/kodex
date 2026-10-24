package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryRunAttachmentAgentDependencies reads exact agent version/capabilities and effective
// instruction pins, preferring managed prompt-template bindings over legacy published
// instructions.
// queryRunAttachmentAgentDependencies читает точные agent version/capabilities и effective
// instruction pins, предпочитая managed prompt-template bindings прежним опубликованным
// инструкциям.
//
//go:embed sql/run_attachment_agent_dependencies.sql
var queryRunAttachmentAgentDependencies string

// queryRunAttachmentAccountDependencies reads the exact tenant/account version, enabled state and
// current credential revision identifier as digest dependencies, without credential material.
// queryRunAttachmentAccountDependencies читает точные tenant/account version, enabled state и
// идентификатор текущей credential revision как зависимости дайджеста, без материала реквизитов.
//
//go:embed sql/run_attachment_account_dependencies.sql
var queryRunAttachmentAccountDependencies string

// queryRunAttachmentImageDependencies projects a JSON tuple of the agent environment image and
// recipe admission/promotion/runtime pins; left joins preserve absent dependencies.
// queryRunAttachmentImageDependencies формирует JSON tuple image окружения агента и recipe
// admission/promotion/runtime pins; left joins сохраняют отсутствие зависимостей.
//
//go:embed sql/run_attachment_image_dependencies.sql
var queryRunAttachmentImageDependencies string

const (
	// runAttachmentTargetUnavailable marks an absent or invalid launch target/workflow snapshot in the
	// eligibility projection without granting any launch authority.
	// runAttachmentTargetUnavailable помечает отсутствующую либо неверную launch target/workflow
	// snapshot в проекции допустимости, не выдавая полномочий запуска.
	runAttachmentTargetUnavailable = "TARGET_UNAVAILABLE"
	// runAttachmentRuntimeNotReady marks unavailable runtime contract or agent/provider dependencies
	// after preserving any earlier refusal reason.
	// runAttachmentRuntimeNotReady помечает недоступный runtime contract либо зависимости
	// agent/provider с сохранением более ранней причины отказа.
	runAttachmentRuntimeNotReady = "RUNTIME_NOT_READY"
	// runAttachmentSessionUnavailable marks an existing run that does not expose ADD_TURN before later
	// target/runtime checks can supply their own reason.
	// runAttachmentSessionUnavailable помечает существующий run без ADD_TURN до того, как последующие
	// target/runtime проверки смогут задать свою причину.
	runAttachmentSessionUnavailable = "SESSION_UNAVAILABLE"
)

// GetRunAttachmentEligibility evaluates exact project/target and optional run under a five-second
// read-only repeatable-read transaction with launch/view authorization, ordered dependency pins
// and refusal precedence; commits a digest-bound hint without creating a grant or changing run
// state.
// GetRunAttachmentEligibility оценивает точные project/target и необязательный run в пятисекундной
// read-only repeatable-read транзакции с launch/view authorization, упорядоченными dependency pins
// и приоритетом отказов; возвращает после commit подсказку с дайджестом без создания grant или
// изменения run state.
func (repository *Repository) GetRunAttachmentEligibility(ctx context.Context, principal value.Principal, projectRef string, target entity.RunTarget, runRef string) (entity.RunAttachmentEligibility, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	target.Name = ""
	result := entity.RunAttachmentEligibility{ProjectRef: projectRef, Target: target, RunRef: runRef}
	if !validOverlayHistoryRef(projectRef) || !validOverlayHistoryRef(target.Ref) || !contains([]string{"AGENT", "WORKFLOW"}, target.Type) || runRef != "" && !validOverlayHistoryRef(runRef) {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	resolved, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectRef, ResourceKind: target.Type, ResourceRef: target.Ref})
	if err != nil {
		return result, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != resolved.projectID {
		return result, errs.ErrNotFound
	}
	launch := command.Command{Kind: command.LaunchRun, Payload: command.LaunchRunInput{ProjectRef: projectRef, Target: target}}
	if err := repository.authorizeCommand(ctx, tx, current, launch); err != nil {
		return result, err
	}
	parts := []any{current.organizationID, current.actorID, current.authorityProjectID, projectRef, target.Type, target.Ref, runRef}
	authority, err := repository.capabilityAuthority(ctx, tx, current, projectRef, "")
	if err != nil {
		return result, err
	}
	parts = append(parts, authority.subject, authority.bindings)
	if runRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: runRef, ProjectRef: projectRef}); err != nil {
			return result, err
		}
		run, err := repository.readRunWithIncidents(ctx, tx, current, runRef)
		if err != nil {
			return result, err
		}
		if run.ProjectRef != projectRef || run.Target.Type != target.Type || run.Target.Ref != target.Ref {
			return result, errs.ErrInvalid
		}
		result.RunVersion = run.Version
		parts = append(parts, run.Version, run.SessionRef, run.State)
		if !contains(run.NextActions, "ADD_TURN") {
			result.Reason = runAttachmentSessionUnavailable
		}
		if result.Reason == "" {
			var candidate resumableSessionCandidate
			if err := tx.QueryRow(ctx, queryResumableSessionGet, pgx.StrictNamedArgs{
				"organization_id": current.organizationID, "actor_id": current.actorID,
				"authority_project_id": current.authorityProjectID, "run_ref": runRef,
			}).Scan(&candidate.RunRef, &candidate.Version, &candidate.SessionID, &candidate.SessionRef, &candidate.ProjectID, &candidate.ProjectRef, &candidate.TargetType, &candidate.TargetRef, &candidate.AccountRef); err != nil {
				return result, errs.ErrUnavailable
			}
			binding, err := readSessionModelCatalog(ctx, tx, current.organizationID, candidate.SessionID, candidate.AccountRef)
			if err != nil {
				return result, err
			}
			parts = append(parts, binding)
		}
	}
	refs, workflowRef, workflowDigest, reason, err := repository.resolveAttachmentLaunchTarget(ctx, tx, current, resolved.projectID, target)
	if err != nil {
		return result, err
	}
	result.WorkflowVersionRef = workflowRef
	parts = append(parts, workflowRef, workflowDigest)
	if result.Reason == "" {
		result.Reason = reason
	}
	if len(refs) != 0 {
		ready, err := repository.attachmentRuntimeContractReady(ctx, tx, current.organizationID, resolved.projectID, refs)
		if err != nil {
			return result, err
		}
		parts = append(parts, ready)
		if !ready && result.Reason == "" {
			result.Reason = runAttachmentRuntimeNotReady
		}
		allowed, err := repository.attachmentAgentsAllowed(ctx, tx, current.organizationID, resolved.projectID, refs)
		if err != nil {
			return result, err
		}
		parts = append(parts, allowed)
		if !allowed && result.Reason == "" {
			result.Reason = fileTargetCapabilityRequired
		}
		seen := map[string]bool{}
		for index, ref := range refs {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			dependency, ready, err := repository.attachmentAgentDependencies(ctx, tx, current, resolved.projectID, ref, runRef == "" || index != 0)
			if err != nil {
				return result, err
			}
			parts = append(parts, dependency)
			if !ready && result.Reason == "" {
				result.Reason = runAttachmentRuntimeNotReady
			}
		}
	}
	if result.Reason == "" {
		result.Reason = fileTargetAvailable
		result.Eligible = true
	}
	parts = append(parts, result.Reason)
	result.Digest, result.EvaluatedAt = jsonValueDigest(parts), time.Now().UTC()
	if err := tx.Commit(ctx); err != nil {
		return entity.RunAttachmentEligibility{}, errs.ErrUnavailable
	}
	return result, nil
}

// resolveAttachmentLaunchTarget resolves an agent or validates the published workflow snapshot and
// exact coordinator, returning coordinator plus step agents and version/digest pins; missing or
// invalid targets become a denial hint.
// resolveAttachmentLaunchTarget разрешает агента либо проверяет опубликованный workflow snapshot и
// точного coordinator, возвращая coordinator и step agents с version/digest pins; отсутствие либо
// неверная цель даёт подсказку отказа.
func (repository *Repository) resolveAttachmentLaunchTarget(ctx context.Context, tx pgx.Tx, current scope, projectID string, target entity.RunTarget) ([]string, string, string, string, error) {
	if target.Type == "AGENT" {
		var name string
		if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectAgentsOrganizationIdProjectIdRef, current.organizationID, projectID, target.Ref).Scan(&name); errors.Is(err, pgx.ErrNoRows) {
			return nil, "", "", runAttachmentTargetUnavailable, nil
		} else if err != nil {
			return nil, "", "", "", errs.ErrUnavailable
		}
		return []string{target.Ref}, "", "", "", nil
	}
	var name, versionID, versionRef, digest, coordinatorRef, coordinatorName string
	var raw []byte
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunSelectWorkflowsOrganizationIdProjectIdRef, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID, "workflow_ref": target.Ref,
	}).Scan(&name, &versionID, &versionRef, &raw, &digest, &coordinatorRef, &coordinatorName); errors.Is(err, pgx.ErrNoRows) {
		return nil, "", "", runAttachmentTargetUnavailable, nil
	} else if err != nil {
		return nil, "", "", "", errs.ErrUnavailable
	}
	var version entity.WorkflowVersion
	if json.Unmarshal(raw, &version) != nil || !validWorkflowVersion(version) || version.CoordinatorAgentRef != coordinatorRef {
		return nil, versionRef, digest, runAttachmentTargetUnavailable, nil
	}
	refs := []string{coordinatorRef}
	for _, step := range version.Steps {
		refs = append(refs, step.AgentRef)
	}
	return refs, versionRef, digest, "", nil
}

// attachmentRuntimeContractReady checks all target agents against the configured role-runtime
// contract revision/digest and default image through the dedicated authoritative SQL.
// attachmentRuntimeContractReady проверяет всех целевых агентов относительно настроенных
// role-runtime contract revision/digest и default image через отдельный авторитетный SQL.
func (repository *Repository) attachmentRuntimeContractReady(ctx context.Context, tx pgx.Tx, organizationID, projectID string, refs []string) (bool, error) {
	var ready bool
	if err := tx.QueryRow(ctx, queryCommandsLaunchrunValidateAgentRuntimeContract, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "agent_refs": refs,
		"role_runtime_contract_revision": repository.roleImages.RoleRuntimeContractRevision,
		"role_runtime_contract_sha256":   repository.roleImages.RoleRuntimeContractSHA256,
		"default_role_image_digest":      repository.roleImages.DefaultImageDigest,
	}).Scan(&ready); err != nil {
		return false, errs.ErrUnavailable
	}
	return ready, nil
}

// attachmentAgentsAllowed requires the artifact capability for the exact tenant/project agent list
// through the shared capability predicate, without granting capabilities.
// attachmentAgentsAllowed требует artifact capability для точного списка агентов tenant/project
// через общий capability predicate, не выдавая capabilities.
func (repository *Repository) attachmentAgentsAllowed(ctx context.Context, tx pgx.Tx, organizationID, projectID string, refs []string) (bool, error) {
	return repository.agentsHaveCapabilities(ctx, tx, organizationID, projectID, refs, []string{runtimecontract.ArtifactCapability})
}

// attachmentAgentDependencies collects sorted capability, instruction, image, runtime/environment
// and provider-account pins; optionally validates current model catalogs, preserving
// frozen-catalog continuation behavior and distinguishing ineligible dependencies from read
// failures.
// attachmentAgentDependencies собирает отсортированные capability, instruction, image,
// runtime/environment и provider-account pins; необязательно проверяет текущие model catalogs,
// сохраняя frozen-catalog поведение продолжения и различая недопустимые зависимости и ошибки
// чтения.
func (repository *Repository) attachmentAgentDependencies(ctx context.Context, tx pgx.Tx, current scope, projectID, agentRef string, checkCatalog bool) ([]any, bool, error) {
	var version int64
	var capabilities []string
	var instructionRef, instructionDigest string
	if err := tx.QueryRow(ctx, queryRunAttachmentAgentDependencies, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID, "agent_ref": agentRef,
	}).Scan(&version, &capabilities, &instructionRef, &instructionDigest); errors.Is(err, pgx.ErrNoRows) {
		return []any{agentRef}, false, nil
	} else if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	slices.Sort(capabilities)
	parts := []any{agentRef, version, capabilities, instructionRef, instructionDigest}
	var imageDependency json.RawMessage
	if err := tx.QueryRow(ctx, queryRunAttachmentImageDependencies, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": projectID, "agent_ref": agentRef,
	}).Scan(&imageDependency); err != nil || !json.Valid(imageDependency) {
		return nil, false, errs.ErrUnavailable
	}
	parts = append(parts, imageDependency)
	view, err := repository.scanAgentRuntimeConfigurationView(tx.QueryRow(ctx, queryRuntimeConfigurationGetAgentView, current.organizationID, agentRef, "OWNER", current.actorID))
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, errs.ErrNotFound) {
		return parts, false, nil
	}
	if err != nil {
		return nil, false, errs.ErrUnavailable
	}
	parts = append(parts, view.Configuration.Ref, view.Configuration.Version, view.Configuration.Digest,
		view.Configuration.Provider, view.Configuration.Model, view.Configuration.ProviderPolicy.Ref, view.Configuration.ProviderPolicy.Version,
		view.Configuration.ProviderPolicy.Digest, view.Configuration.ProviderPolicy.AccountCandidates,
		view.PublishedOverlay.Ref, view.PublishedOverlay.Version, view.PublishedOverlay.Digest,
		view.EnvironmentBinding, view.Environment.Ref, view.Environment.Version, view.Environment.State,
		view.Environment.CurrentVersion.Ref, view.Environment.CurrentVersion.Digest, view.Environment.CurrentVersion.Image.ArtifactRef, view.Environment.CurrentVersion.Image.Digest)
	ready := instructionRef != ""
	if checkCatalog {
		if _, _, err := validateRuntimeCatalogCandidatesSnapshot(ctx, tx, current, view.Configuration.Provider, view.Configuration.Model, view.PublishedOverlay.Content, view.Configuration.ProviderPolicy.AccountCandidates, false, false); err != nil {
			if continuationIneligible(err) {
				ready = false
			} else {
				return nil, false, err
			}
		}
	}
	for _, candidate := range view.Configuration.ProviderPolicy.AccountCandidates {
		var accountVersion int64
		var enabled bool
		var state, credentialRevision string
		err := tx.QueryRow(ctx, queryRunAttachmentAccountDependencies, pgx.StrictNamedArgs{"organization_id": current.organizationID, "account_ref": candidate.AccountRef}).Scan(&accountVersion, &enabled, &state, &credentialRevision)
		if errors.Is(err, pgx.ErrNoRows) {
			ready = false
			continue
		}
		if err != nil {
			return nil, false, errs.ErrUnavailable
		}
		parts = append(parts, candidate.AccountRef, accountVersion, enabled, state, credentialRevision)
		if !checkCatalog {
			continue
		}
		catalog, err := readModelCatalogTx(ctx, tx, current, view.Configuration.Provider, candidate.AccountRef)
		if continuationIneligible(err) {
			ready = false
			continue
		}
		if err != nil {
			return nil, false, err
		}
		parts = append(parts, catalog.Revision, catalog.Digest, catalog.Status)
	}
	return parts, ready, nil
}
