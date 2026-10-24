package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	emailbridgeapi "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	promptservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/prompt"
	scheduleservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/schedule"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// credentialProjectionPendingCode identifies a credential projection delay for the bounded owner
// READ-test retry path; expiry of that retry budget maps to integration unavailability rather than
// proving credential invalidity.
// credentialProjectionPendingCode обозначает задержку credential projection для ограниченного
// повтора owner READ-test; исчерпание бюджета отображается в недоступность integration без
// доказательства невалидности credential.
const credentialProjectionPendingCode = "INTEGRATION_CREDENTIAL_PROJECTION_PENDING"

// ReconcileWarmRuntime reconciles provider policy and the locked warm session in a Serializable
// transaction, replacing an ineligible account binding or committing unavailable state when no
// replacement exists. It verifies catalog/config/environment/image/prompt/workspace pins, builds a
// server-owned desired snapshot, binds its specification/digest and commits before returning
// materializationRequired. Failures carry the current safe stage; ignored optional limits JSON and
// request-context rollback remain historical behavior.
// ReconcileWarmRuntime согласует provider policy и заблокированную warm session в Serializable
// транзакции, заменяет недопустимый account binding либо фиксирует unavailable без доступной
// замены. Проверяет catalog/config/environment/image/prompt/workspace pins, строит серверный
// desired snapshot, связывает specification/digest и выполняет commit до возврата
// materializationRequired. Ошибка содержит текущий безопасный stage; ignored optional limits JSON
// и request-context rollback сохраняют историческое поведение.
func (repository *Repository) ReconcileWarmRuntime(ctx context.Context, principal value.Principal, instance string) (result entity.SystemAssistant, desired map[string]any, materializationRequired bool, returnedError error) {
	stage := errs.WarmReconcileScope
	defer func() { returnedError = errs.WithWarmRuntimeStage(returnedError, stage) }()
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	stage = errs.WarmReconcileTransaction
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stage = errs.WarmReconcileProviderPolicy
	if _, err := repository.reconcileSystemAssistantProviderPolicy(ctx, tx, scope); err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	stage = errs.WarmReconcileSessionBinding
	sessionBinding, err := repository.lockWarmSessionBinding(ctx, tx, scope.organizationID)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	sessionMigrated := false
	if !sessionBinding.providerAccountEligible {
		stage = errs.WarmReconcileSessionReplacement
		providerAccountID, selectErr := repository.selectProviderAccountForAgent(
			ctx, tx, scope.organizationID, sessionBinding.assistantRef,
		)
		if errors.Is(selectErr, errs.ErrConflict) {
			if err := repository.markWarmRuntimeUnavailable(ctx, tx, scope.organizationID, sessionBinding); err != nil {
				return entity.SystemAssistant{}, nil, false, err
			}
			assistant, err := repository.getAssistantTx(ctx, tx, scope)
			if err != nil {
				return entity.SystemAssistant{}, nil, false, err
			}
			if err := tx.Commit(ctx); err != nil {
				return entity.SystemAssistant{}, nil, false, errs.ErrConflict
			}
			return assistant, nil, false, nil
		}
		if selectErr != nil {
			return entity.SystemAssistant{}, nil, false, selectErr
		}
		if err := repository.replaceWarmSession(ctx, tx, scope.organizationID, sessionBinding, providerAccountID); err != nil {
			return entity.SystemAssistant{}, nil, false, err
		}
		sessionMigrated = true
	}
	stage = errs.WarmReconcileSnapshot
	var assistant entity.SystemAssistant
	var limits []byte
	var promptRef, promptDigest, promptContent, ownerInstructions, systemSessionRef string
	var warmInstance, runtimeKey, profileRevision, provider, model, roleDefinitionRef string
	var providerAccountRef, providerCredentialRef, providerSecretName string
	var providerSecretUID, providerSecretResourceVersion, providerCredentialSHA256 string
	var runtimeConfigRef, runtimeConfigDigest, providerPolicyRef, providerPolicyDigest string
	var configOverlayRef, configOverlayDigest, configOverlay string
	var runtimeEnvironmentRef, runtimeEnvironmentDigest string
	var environmentBindingRef, environmentBindingDigest string
	var rawEnvironmentValues, rawSecretProjections, rawEnvironmentTools []byte
	var rawResourcePolicy, rawVolumePolicy, rawNetworkPolicy, rawKubernetesAccessProfile []byte
	var environmentCoreDigest, resourcesDigest, volumesDigest, networkDigest, rbacDigest string
	var imageArtifactRef, imageRecipeRef, imageReference, imageManifestDigest, imageContractSHA256 string
	var imageRecipeGeneration, imageContractRevision int64
	var providerCredentialRevisionNumber, runtimeConfigVersion, providerPolicyVersion int64
	var configOverlayVersion, runtimeEnvironmentVersion, environmentBindingVersion int64
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSelectAssistantRuntimeOrganizationId,
		scope.organizationID, repository.roleImages.RoleRuntimeContractRevision,
		repository.roleImages.RoleRuntimeContractSHA256).Scan(
		&assistant.Ref, &assistant.StableKey, &assistant.Name, &assistant.Purpose,
		&assistant.CorePromptRevision, &ownerInstructions, &assistant.RuntimeState,
		&assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &systemSessionRef,
		&limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt,
		&promptRef, &promptDigest, &promptContent, &warmInstance, &runtimeKey,
		&profileRevision, &provider, &model, &roleDefinitionRef, &providerAccountRef,
		&providerCredentialRef, &providerCredentialRevisionNumber, &providerSecretName,
		&providerSecretUID, &providerSecretResourceVersion, &providerCredentialSHA256,
		&runtimeConfigRef, &runtimeConfigVersion, &runtimeConfigDigest,
		&providerPolicyRef, &providerPolicyVersion, &providerPolicyDigest,
		&configOverlayRef, &configOverlayVersion, &configOverlayDigest, &configOverlay,
		&runtimeEnvironmentRef, &runtimeEnvironmentVersion, &runtimeEnvironmentDigest,
		&environmentBindingRef, &environmentBindingVersion, &environmentBindingDigest,
		&rawEnvironmentValues, &rawSecretProjections, &rawEnvironmentTools,
		&environmentCoreDigest, &rawResourcePolicy, &rawVolumePolicy, &rawNetworkPolicy, &rawKubernetesAccessProfile,
		&resourcesDigest, &volumesDigest, &networkDigest, &rbacDigest,
		&imageArtifactRef, &imageRecipeRef, &imageRecipeGeneration, &imageReference,
		&imageManifestDigest, &imageContractRevision, &imageContractSHA256,
	)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
	}
	stage = errs.WarmReconcileOverlay
	canonicalOverlay, verifiedOverlayDigest, err := runtimecontract.CanonicalConfigOverlay(configOverlay)
	if err != nil || canonicalOverlay != configOverlay || verifiedOverlayDigest != configOverlayDigest {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("verify config overlay: %w", errs.ErrConflict)
	}
	stage = errs.WarmReconcileSessionBinding
	currentBinding, err := repository.lockWarmSessionBinding(ctx, tx, scope.organizationID)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	stage = errs.WarmReconcileCatalog
	configuration, _, err := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, assistant.Ref, "")
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	verifiedCandidate, retainedPolicy, err := checkedSessionModelCatalog(ctx, tx, scope.organizationID, currentBinding.sessionID, providerAccountRef, configuration, configOverlay)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	if retainedPolicy != nil {
		providerPolicyRef, providerPolicyVersion, providerPolicyDigest = retainedPolicy.PolicyRef, retainedPolicy.PolicyVersion, retainedPolicy.PolicyDigest
	}
	stage = errs.WarmReconcileOverlay
	parsedOverlay, err := runtimecontract.ParseConfigOverlay(configOverlay)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("parse config overlay: %w", errs.ErrConflict)
	}
	effectiveEffort := parsedOverlay.ModelReasoningEffort
	if effectiveEffort == "" {
		effectiveEffort = verifiedCandidate.DefaultReasoningEffort
	}
	reasoningMode := runtimecontract.ReasoningSupported
	if effectiveEffort == "" {
		reasoningMode = runtimecontract.ReasoningUnsupported
	}
	stage = errs.WarmReconcileEnvironment
	var environmentValues []runtimecontract.RuntimeEnvironmentValue
	var secretProjections []runtimecontract.RuntimeSecretProjection
	if err := decodeStoredRuntimeEnvironment(rawEnvironmentValues, rawSecretProjections, &environmentValues, &secretProjections); err != nil {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("decode runtime environment values: %w", errs.ErrConflict)
	}
	var environmentTools []runtimecontract.RuntimeEnvironmentTool
	if err := decodeStrict(rawEnvironmentTools, &environmentTools); err != nil {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("decode runtime environment tools: %w", errs.ErrConflict)
	}
	environmentImage := runtimecontract.RuntimeEnvironmentImage{
		ArtifactRef: imageArtifactRef, RecipeRef: imageRecipeRef,
		RecipeGeneration: imageRecipeGeneration,
		Reference:        imageReference, Digest: imageManifestDigest,
	}
	environmentPolicy, err := decodeRuntimeEnvironmentPolicy(rawResourcePolicy, rawVolumePolicy, rawNetworkPolicy,
		rawKubernetesAccessProfile, resourcesDigest, volumesDigest, networkDigest, rbacDigest)
	if err != nil || environmentPolicy.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("verify runtime environment policy: %w", errs.ErrConflict)
	}
	effectiveKubernetesAccess, err := runtimecontract.RuntimeKubernetesAccessForExecution(
		environmentPolicy.KubernetesAccess, "agent-runner", "system-assistant-warm")
	if err != nil {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("resolve runtime Kubernetes access: %w", errs.ErrConflict)
	}
	verifiedCoreDigest, err := runtimecontract.RuntimeEnvironmentCoreDigest(environmentValues, secretProjections, environmentImage, environmentTools)
	if err != nil || verifiedCoreDigest != environmentCoreDigest {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("verify runtime environment core digest: %w", errs.ErrConflict)
	}
	verifiedEnvironmentDigest, err := runtimecontract.RuntimeEnvironmentDigest(
		environmentValues, secretProjections, environmentImage, environmentTools, environmentPolicy)
	if err != nil || verifiedEnvironmentDigest != runtimeEnvironmentDigest {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("verify runtime environment digest: %w", errs.ErrConflict)
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.OwnerInstructions = ownerInstructions
	assistant.WarmSessionRef = systemSessionRef
	assistant.System = true
	assistant.Deletable = false
	stage = errs.WarmReconcileLifecycle
	stale := assistant.LastHeartbeatAt == nil || time.Since(*assistant.LastHeartbeatAt) > 45*time.Second
	required := sessionMigrated || !contains([]string{"READY", "BUSY"}, assistant.RuntimeState) || assistant.RuntimeRevision != assistant.DesiredRuntimeRevision || warmInstance != instance || stale
	if required && !sessionMigrated {
		if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeUpdateAssistantRuntimeRuntimeStateWarmInstanceRefVersion, scope.organizationID, instance); err != nil {
			return entity.SystemAssistant{}, nil, false, errs.ErrUnavailable
		}
		assistant.RuntimeState = "RECOVERING"
		assistant.Version++
	}
	stage = errs.WarmReconcilePrompt
	materializedPrompt, promptSnapshot, err := materializeWarmPlatformPrompt(promptContent, ownerInstructions, promptRef, promptDigest, assistant.Ref, systemSessionRef)
	if err != nil || !materializedPrompt.Complete {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("materialize warm prompt: %w", errs.ErrConflict)
	}
	resolvedInstructions := materializedPrompt.Prompt
	resolvedInstructionsSum := sha256.Sum256([]byte(resolvedInstructions))
	resolvedInstructionsDigest := hex.EncodeToString(resolvedInstructionsSum[:])
	stage = errs.WarmReconcileWorkspace
	workspacePolicy, err := runtimeWorkspacePolicyWithLimits(environmentPolicy.Resources.WorkspaceLimits)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	snapshot := map[string]any{
		"organizationRef": scope.organizationRef, "assistantRef": assistant.Ref, "agentRef": assistant.Ref,
		"stableKey": assistant.StableKey, "sessionRef": systemSessionRef,
		"assistantScope":   string(runtimecontract.AssistantScopeSystem),
		"systemSessionRef": systemSessionRef, "runtimeRevisionRef": assistant.DesiredRuntimeRevision,
		"runtimeRevisionVersion": assistant.Version, "runtimeRevision": profileRevision,
		"runtimeKey": runtimeKey, "profileRevision": profileRevision,
		"runtimeProvider": provider, "runtimeModel": model, "corePromptRef": promptRef,
		"effectiveReasoningEffort": effectiveEffort, "reasoningMode": reasoningMode,
		"providerAccountRef":               providerAccountRef,
		"providerCredentialRevisionRef":    providerCredentialRef,
		"providerCredentialRevisionNumber": providerCredentialRevisionNumber,
		"providerSecretName":               providerSecretName,
		"providerSecretUID":                providerSecretUID,
		"providerSecretResourceVersion":    providerSecretResourceVersion,
		"providerCredentialSHA256":         providerCredentialSHA256,
		"corePromptDigest":                 promptDigest, "corePrompt": promptContent,
		"instructionRef":                promptRef,
		"instructionDigest":             resolvedInstructionsDigest,
		"promptTemplateRef":             promptRef,
		"promptTemplateDigest":          promptDigest,
		"promptMaterializationDigest":   materializedPrompt.Digest,
		"promptSnapshot":                promptSnapshot,
		"promptRuntimeContractVersion":  1,
		"promptServiceTemplateRevision": materializedPrompt.ServiceTemplateRevision,
		"promptServiceTemplateDigest":   materializedPrompt.ServiceTemplateDigest,
		"promptTargetKind":              promptservice.TargetAgent,
		"ownerInstructions":             ownerInstructions, "instructions": resolvedInstructions,
		"resourceLimits": assistant.ResourceLimits, "directSecretAccess": false,
		"roleDefinitionRef":           roleDefinitionRef,
		"imageReference":              imageReference,
		"imageManifestDigest":         imageManifestDigest,
		"roleImageArtifactRef":        environmentImage.ArtifactRef,
		"roleImageRecipeRef":          environmentImage.RecipeRef,
		"roleImageRecipeGeneration":   environmentImage.RecipeGeneration,
		"roleRuntimeContractRevision": imageContractRevision,
		"roleRuntimeContractSHA256":   imageContractSHA256,
		"runtimeConfigRef":            runtimeConfigRef,
		"runtimeConfigVersion":        runtimeConfigVersion,
		"runtimeConfigDigest":         runtimeConfigDigest,
		"providerPolicyRef":           providerPolicyRef,
		"providerPolicyVersion":       providerPolicyVersion,
		"providerPolicyDigest":        providerPolicyDigest,
		"configOverlayRef":            configOverlayRef,
		"configOverlayVersion":        configOverlayVersion,
		"configOverlayDigest":         configOverlayDigest,
		"configOverlay":               configOverlay,
		"runtimeEnvironmentRef":       runtimeEnvironmentRef,
		"runtimeEnvironmentVersion":   runtimeEnvironmentVersion,
		"runtimeEnvironmentDigest":    runtimeEnvironmentDigest,
		"environmentBindingRef":       environmentBindingRef,
		"environmentBindingVersion":   environmentBindingVersion,
		"environmentBindingDigest":    environmentBindingDigest,
		"environmentValues":           environmentValues,
		"secretProjections":           secretProjections,
		"environmentImage":            environmentImage,
		"environmentTools":            environmentTools,
		"environmentPolicy":           environmentPolicy,
		"effectiveKubernetesAccess":   effectiveKubernetesAccess,
		"workspacePolicy":             workspacePolicy,
	}
	stage = errs.WarmReconcileContext
	contextSnapshot, err := repository.runtimeContextSnapshot(ctx, tx, scope, "", "", assistant.Ref)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	snapshot["contextSnapshot"] = contextSnapshot
	stage = errs.WarmReconcileSpecification
	specificationChanged, err := repository.bindWarmSpecification(ctx, tx, scope, &assistant, snapshot)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, err
	}
	required = required || specificationChanged
	stage = errs.WarmReconcileDigest
	revisionDigest, err := runtimeRevisionDigestFromSnapshot(snapshot)
	if err != nil {
		return entity.SystemAssistant{}, nil, false, fmt.Errorf("digest warm runtime revision: %w", errs.ErrConflict)
	}
	snapshot["revisionDigest"] = revisionDigest
	stage = errs.WarmReconcileCommit
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, nil, false, errs.ErrConflict
	}
	return assistant, snapshot, required, nil
}

// systemAssistantProviderPolicySnapshot holds the locked system assistant/runtime configuration
// identity, version/model/policy mode and current/desired account candidates for one
// reconciliation. It contains metadata, not provider credential values or a standalone grant.
// systemAssistantProviderPolicySnapshot хранит заблокированные identity системного
// помощника/runtime configuration, version/model/policy mode и текущих/ожидаемых account
// candidates одного согласования. Содержит метаданные, не значения credentials и не
// самостоятельный grant.
type systemAssistantProviderPolicySnapshot struct {
	agentID, agentRef, configID, runtimeProfileRef, provider, model, mode string
	configVersion                                                         int64
	currentCandidates, desiredCandidates                                  []entity.ProviderAccountCandidate
}

// reconcileSystemAssistantProviderPolicy reads and validates current/desired policy candidates,
// preserving the initial empty LEAST_USED bootstrap exception and fallback default-model
// selection. It captures catalog pins, sorts candidates and skips an unchanged policy; otherwise
// publishes new policy/config/audit references and desired runtime revision in the caller
// transaction. It does not commit and attaches precise reconcile stages to failures.
// reconcileSystemAssistantProviderPolicy читает и проверяет текущих/ожидаемых policy candidates,
// сохраняет начальное исключение empty LEAST_USED и fallback выбора default model. Фиксирует
// catalog pins, сортирует candidates, пропускает неизменённую policy; иначе публикует новые
// policy/config/audit refs и desired runtime revision в транзакции вызывающего. Не выполняет
// commit, добавляет точный reconcile stage к ошибкам.
func (repository *Repository) reconcileSystemAssistantProviderPolicy(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
) (changed bool, returnedError error) {
	stage := errs.WarmReconcileProviderPolicyRead
	defer func() { returnedError = errs.WithWarmRuntimeStage(returnedError, stage) }()
	var snapshot systemAssistantProviderPolicySnapshot
	var rawCurrent, rawDesired []byte
	err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSelectSystemProviderPolicy, pgx.StrictNamedArgs{
		"organization_id": current.organizationID,
	}).Scan(
		&snapshot.agentID,
		&snapshot.agentRef,
		&snapshot.configID,
		&snapshot.configVersion,
		&snapshot.runtimeProfileRef,
		&snapshot.provider,
		&snapshot.model,
		&snapshot.mode,
		&rawCurrent,
		&rawDesired,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	stage = errs.WarmReconcileProviderPolicyDecode
	if json.Unmarshal(rawCurrent, &snapshot.currentCandidates) != nil ||
		json.Unmarshal(rawDesired, &snapshot.desiredCandidates) != nil ||
		(!validProviderPolicy(snapshot.mode, snapshot.currentCandidates) &&
			!(snapshot.configVersion == 1 && snapshot.mode == "LEAST_USED" && string(rawCurrent) == "[]")) {
		return false, errs.ErrConflict
	}
	if len(snapshot.desiredCandidates) == 0 {
		return false, nil
	}
	bootstrapPolicy := snapshot.configVersion == 1 && snapshot.mode == "LEAST_USED" && len(snapshot.currentCandidates) == 0
	desiredCandidates := snapshot.desiredCandidates
	stage = errs.WarmReconcileProviderPolicyCatalog
	snapshot.desiredCandidates, err = captureRuntimeCatalogPins(ctx, tx, current, snapshot.provider, snapshot.model, desiredCandidates)
	if bootstrapPolicy && errors.Is(err, errs.ErrConflict) {
		snapshot.model, err = firstRunProviderDefaultModel(ctx, tx, current, snapshot.provider, desiredCandidates)
		if err == nil {
			snapshot.desiredCandidates, err = captureRuntimeCatalogPins(ctx, tx, current, snapshot.provider, snapshot.model, desiredCandidates)
		}
	}
	if err != nil {
		return false, err
	}
	stage = errs.WarmReconcileProviderPolicyPublish
	desiredMode := "LEAST_USED"
	if len(snapshot.desiredCandidates) == 1 {
		desiredMode = "FIXED"
	}
	if !validProviderPolicy(desiredMode, snapshot.desiredCandidates) {
		return false, errs.ErrConflict
	}
	sort.Slice(snapshot.currentCandidates, func(left, right int) bool {
		return snapshot.currentCandidates[left].AccountRef < snapshot.currentCandidates[right].AccountRef
	})
	sort.Slice(snapshot.desiredCandidates, func(left, right int) bool {
		return snapshot.desiredCandidates[left].AccountRef < snapshot.desiredCandidates[right].AccountRef
	})
	if snapshot.mode == desiredMode && providerCandidatesEqual(snapshot.currentCandidates, snapshot.desiredCandidates) {
		return false, nil
	}
	rawDesired, err = json.Marshal(snapshot.desiredCandidates)
	if err != nil {
		return false, errs.ErrConflict
	}
	policyRef, err := newRef("ppol")
	if err != nil {
		return false, err
	}
	configRef, err := newRef("rconf")
	if err != nil {
		return false, err
	}
	auditRef, err := newRef("aud")
	if err != nil {
		return false, err
	}
	version := snapshot.configVersion + 1
	policyDigest := digestBytes([]byte(desiredMode), rawDesired)
	configDigest := digestBytes(
		[]byte(snapshot.runtimeProfileRef),
		[]byte(snapshot.provider),
		[]byte(snapshot.model),
		[]byte(policyRef),
		[]byte(strconvFormat(version)),
		[]byte(policyDigest),
	)
	var publishedRef string
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimePublishSystemProviderPolicy, pgx.StrictNamedArgs{
		"policy_ref": policyRef, "organization_id": current.organizationID, "agent_id": snapshot.agentID,
		"version_number": version, "policy_mode": desiredMode, "account_candidates": rawDesired,
		"policy_digest": policyDigest, "created_by": current.actorID, "config_ref": configRef,
		"runtime_profile_ref": snapshot.runtimeProfileRef, "provider": snapshot.provider, "model": snapshot.model,
		"config_digest": configDigest, "current_config_id": snapshot.configID,
		"next_runtime_revision": "system-assistant-runtime-" + configDigest, "audit_ref": auditRef,
	}).Scan(&publishedRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil || publishedRef != configRef {
		return false, errs.ErrUnavailable
	}
	return true, nil
}

// providerCandidatesEqual compares candidate slices positionally using exact struct equality; nil
// and empty compare equal and callers must sort first when order is semantically irrelevant.
// providerCandidatesEqual сравнивает срезы candidates позиционно точным равенством структур; nil и
// пустой равны, вызывающий сначала сортирует, если порядок семантически незначим.
func providerCandidatesEqual(left, right []entity.ProviderAccountCandidate) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// warmSessionBinding carries the canonical warm assistant/session/creator identity and observed
// provider-account eligibility. An empty session denotes initial binding work, not a fabricated
// provider or a reusable authorization token.
// warmSessionBinding несёт канонические identity warm assistant/session/creator и наблюдаемую
// eligibility provider account. Пустая session означает начальную привязку, не фиктивного
// провайдера и не повторно используемый токен полномочий.
type warmSessionBinding struct {
	assistantRef            string
	sessionID               string
	sessionRef              string
	createdBy               string
	providerAccountEligible bool
}

// lockWarmSessionBinding first locks the runtime owner for an initial missing session; only NoRows
// falls through to the existing-session binding query. Missing existing binding becomes Conflict
// and other reads Unavailable. The caller owns the transaction and decides whether replacement or
// unavailable state follows.
// lockWarmSessionBinding сначала блокирует runtime owner для начального отсутствия session; лишь
// NoRows направляет к запросу существующего session binding. Отсутствие существующей связи даёт
// Conflict, другие ошибки — Unavailable. Транзакцией и выбором replacement/unavailable владеет
// вызывающий.
func (repository *Repository) lockWarmSessionBinding(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
) (warmSessionBinding, error) {
	var binding warmSessionBinding
	// Отсутствующая первая сессия блокируется владельцем runtime, а не
	// заменяется сессией с фиктивным provider account.
	err := tx.QueryRow(ctx, queryWorkersInitialWarmSessionBinding, pgx.StrictNamedArgs{
		"organization_id": organizationID,
	}).Scan(&binding.assistantRef, &binding.createdBy)
	if err == nil {
		return binding, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return warmSessionBinding{}, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeLockSessionBinding, pgx.StrictNamedArgs{
		"organization_id": organizationID,
	}).Scan(
		&binding.assistantRef,
		&binding.sessionID,
		&binding.sessionRef,
		&binding.createdBy,
		&binding.providerAccountEligible,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return warmSessionBinding{}, errs.ErrConflict
	}
	if err != nil {
		return warmSessionBinding{}, errs.ErrUnavailable
	}
	return binding, nil
}

// replaceWarmSession requires a selected provider account, closes any previous warm session,
// creates the replacement under the persisted creator and binds its model catalog before switching
// the runtime session with the expected old reference. It validates returned IDs/version and
// leaves all effects/commit to the caller transaction.
// replaceWarmSession требует выбранный provider account, закрывает прежнюю warm session при
// наличии, создаёт замену от сохранённого creator и связывает model catalog до переключения
// runtime session с ожидаемой старой ссылкой. Проверяет возвращённые IDs/version; все эффекты и
// commit остаются в транзакции вызывающего.
func (repository *Repository) replaceWarmSession(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	current warmSessionBinding,
	providerAccountID string,
) error {
	if providerAccountID == "" {
		return errs.ErrConflict
	}
	if current.sessionID != "" {
		if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeCloseSession, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"session_id":      current.sessionID,
		}); err != nil {
			return errs.ErrUnavailable
		}
	}
	nextSessionRef, err := newRef("ses")
	if err != nil {
		return err
	}
	var nextSessionID string
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeInsertSession, pgx.StrictNamedArgs{
		"session_ref":         nextSessionRef,
		"organization_id":     organizationID,
		"provider_account_id": providerAccountID,
		"created_by":          current.createdBy,
	}).Scan(&nextSessionID); err != nil || nextSessionID == "" {
		return errs.ErrUnavailable
	}
	if err := bindSessionModelCatalog(ctx, tx, organizationID, nextSessionID, current.assistantRef); err != nil {
		return err
	}
	var runtimeVersion int64
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeSwitchSession, pgx.StrictNamedArgs{
		"organization_id":     organizationID,
		"current_session_ref": current.sessionRef,
		"next_session_ref":    nextSessionRef,
	}).Scan(&runtimeVersion); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	} else if err != nil || runtimeVersion < 1 {
		return errs.ErrUnavailable
	}
	return nil
}

// markWarmRuntimeUnavailable closes an existing warm session and marks the exact runtime/session
// binding unavailable in the caller transaction. NoRows becomes Conflict, malformed returned
// version/other failures Unavailable; it neither creates a substitute session nor commits.
// markWarmRuntimeUnavailable закрывает существующую warm session и помечает точный runtime/session
// binding unavailable в транзакции вызывающего. NoRows даёт Conflict, неверная возвращённая
// version/прочие ошибки — Unavailable; не создаёт подменную session и не выполняет commit.
func (repository *Repository) markWarmRuntimeUnavailable(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	current warmSessionBinding,
) error {
	if current.sessionID != "" {
		if _, err := tx.Exec(ctx, queryWorkersReconcilewarmruntimeCloseSession, pgx.StrictNamedArgs{
			"organization_id": organizationID,
			"session_id":      current.sessionID,
		}); err != nil {
			return errs.ErrUnavailable
		}
	}
	var runtimeVersion int64
	if err := tx.QueryRow(ctx, queryWorkersReconcilewarmruntimeMarkUnavailable, pgx.StrictNamedArgs{
		"organization_id": organizationID,
		"session_ref":     current.sessionRef,
	}).Scan(&runtimeVersion); errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrConflict
	} else if err != nil || runtimeVersion < 1 {
		return errs.ErrUnavailable
	}
	return nil
}

// ReportWarmRuntime requires the exact runtime-controller report permission, nonempty
// instance/revision and a closed runtime state. It updates the matching runtime in ReadCommitted,
// derives Ready from the reported state and emits audit/invalidation only for a changed report
// before commit. Optional limits JSON remains permissive and this report is not an independent
// provider probe.
// ReportWarmRuntime требует точные workload runtime-controller и report permission, непустые
// instance/revision и состояние закрытого набора. Обновляет совпавший runtime в ReadCommitted,
// выводит Ready из сообщённого состояния, создаёт audit/invalidation лишь при изменённом report до
// commit. Optional limits JSON остаётся permissive; report не является независимой проверкой
// провайдера.
func (repository *Repository) ReportWarmRuntime(ctx context.Context, principal value.Principal, payload command.WarmRuntimeInput) (entity.SystemAssistant, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SystemAssistant{}, err
	}
	if principal.CallerWorkload != "runtime-controller" || principal.Permission != "platform.runtime.warm.report" ||
		payload.WorkloadInstance == "" || payload.RuntimeRevision == "" ||
		!contains([]string{"STARTING", "READY", "BUSY", "RECOVERING", "UNAVAILABLE"}, payload.State) {
		return entity.SystemAssistant{}, errs.ErrForbidden
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var assistant entity.SystemAssistant
	var limits []byte
	var changed bool
	err = tx.QueryRow(ctx, queryWorkersReportwarmruntimeUpdateAssistantRuntimeRuntimeStateRuntimeRevisionWarmInstanceRef, pgx.StrictNamedArgs{
		"organization_id":   scope.organizationID,
		"workload_instance": payload.WorkloadInstance,
		"runtime_revision":  payload.RuntimeRevision,
		"runtime_state":     payload.State,
	}).Scan(&assistant.StableKey, &assistant.CorePromptRevision, &assistant.OwnerInstructions, &assistant.RuntimeState, &assistant.RuntimeRevision, &assistant.DesiredRuntimeRevision, &assistant.WarmSessionRef, &limits, &assistant.LastHeartbeatAt, &assistant.Version, &assistant.UpdatedAt, &changed)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.SystemAssistant{}, errs.ErrConflict
	}
	if err != nil {
		return entity.SystemAssistant{}, errs.ErrUnavailable
	}
	_ = json.Unmarshal(limits, &assistant.ResourceLimits)
	assistant.System = true
	assistant.Ready = contains([]string{"READY", "BUSY"}, payload.State)
	if changed {
		auditRef, refErr := newRef("aud")
		if refErr != nil {
			return entity.SystemAssistant{}, refErr
		}
		if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction,
			auditRef,
			scope.organizationID,
			nil,
			scope.actorID,
			"controlplane.report_warm_runtime",
			"SYSTEM_ASSISTANT",
			assistant.StableKey,
			"i18n:SYSTEM_ASSISTANT_HEARTBEAT_RECORDED",
			principal.CorrelationRef,
		); err != nil {
			return entity.SystemAssistant{}, errs.ErrUnavailable
		}
		if err := repository.emitPlatformEvent(ctx, tx, scope, "SYSTEM_ASSISTANT_CHANGED", "", assistant.StableKey, "i18n:SYSTEM_ASSISTANT_HEARTBEAT_RECORDED"); err != nil {
			return entity.SystemAssistant{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SystemAssistant{}, errs.ErrConflict
	}
	return assistant, nil
}

// ClaimDueSchedules uses database time in ReadCommitted to dead-letter expired work, reclaim
// expired attempts first and then resolve due schedules with pinned revision/input/prompt/target
// snapshots. It advances next-run time with OCC, records skipped occurrences without a claim and
// persists fresh lease/fence/generation/attempt plus change events before commit. Caller supplies
// a valid bounded limit; historical ignored reference-generation errors and error mappings are
// preserved.
// ClaimDueSchedules использует время БД в ReadCommitted для dead-letter, сначала перезахватывает
// истёкшие attempts, затем разрешает due schedules с pinned revision/input/prompt/target
// snapshots. Продвигает next-run через OCC, записывает skipped occurrences без claim, сохраняет
// свежие lease/fence/generation/attempt и change events до commit. Корректный ограниченный limit
// передаёт вызывающий; прежние ignored reference-generation errors и отображения ошибок
// сохраняются.
func (repository *Repository) ClaimDueSchedules(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var now time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleClock).Scan(&now); err != nil {
		return nil, errs.ErrUnavailable
	}
	deadRows, err := tx.Query(ctx, queryWorkersScheduleOccurrenceExpireDeadLetter, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	deadIDs, err := pgx.CollectRows(deadRows, pgx.RowTo[string])
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, occurrenceID := range deadIDs {
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return nil, err
		}
	}
	result := make([]map[string]any, 0, limit)
	expiredRows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectExpiredOccurrences, scope.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// expiredOccurrence captures the locked expired schedule occurrence identity, immutable
	// input/revision/target digests and prior attempt/generation for reclaim. The new lease uses these
	// persisted pins rather than mutable schedule parameters.
	// expiredOccurrence фиксирует identity заблокированного истёкшего occurrence, immutable
	// input/revision/target digests и прежние attempt/generation для перезахвата. Новая lease
	// использует эти сохранённые pins вместо изменяемых параметров расписания.
	type expiredOccurrence struct {
		id, ref, scheduleRef, inputDigest, scheduleRevisionRef, scheduleRevisionDigest string
		targetRef, targetDigest, automationTextDigest, promptInputsDigest              string
		scheduledFor                                                                   time.Time
		scheduleVersion, generation, scheduleRevision, targetVersion                   int64
		attempt                                                                        int32
	}
	expired := make([]expiredOccurrence, 0, limit)
	for expiredRows.Next() {
		var item expiredOccurrence
		if err := expiredRows.Scan(&item.id, &item.ref, &item.scheduleRef, &item.scheduledFor, &item.scheduleVersion,
			&item.inputDigest, &item.generation, &item.attempt, &item.scheduleRevisionRef, &item.scheduleRevision, &item.scheduleRevisionDigest,
			&item.targetRef, &item.targetVersion, &item.targetDigest, &item.automationTextDigest, &item.promptInputsDigest); err != nil {
			expiredRows.Close()
			return nil, errs.ErrUnavailable
		}
		expired = append(expired, item)
	}
	expiredRows.Close()
	if err := expiredRows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range expired {
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		expires := now.Add(30 * time.Second)
		if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, item.id, "EXPIRED", "SCHEDULE_LEASE_EXPIRED", item.attempt, item.generation); err != nil {
			return nil, errs.ErrUnavailable
		}
		var generation int64
		var attempt int32
		if err := tx.QueryRow(ctx, queryWorkersClaimdueschedulesReclaimExpiredOccurrence, item.id, leaseRef, hex.EncodeToString(digest[:]), instance, expires, item.generation).Scan(&generation, &attempt); errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrConflict
		} else if err != nil {
			return nil, errs.ErrUnavailable
		}
		attemptRef, _ := newRef("satt")
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertAttempt, attemptRef, scope.organizationID, item.id,
			attempt, generation, leaseRef, hex.EncodeToString(digest[:]), instance, item.inputDigest,
			item.scheduleRevisionDigest, expires, principal.CredentialRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, item.id); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"scheduleRef": item.scheduleRef, "occurrenceRef": item.ref, "scheduledFor": item.scheduledFor,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expires,
			"attempt": attempt, "targetRef": item.targetRef, "targetVersion": item.targetVersion,
			"targetDigest": item.targetDigest, "automationTextDigest": item.automationTextDigest,
			"promptInputsDigest": item.promptInputsDigest,
			"scheduleVersion":    item.scheduleVersion, "scheduleRevisionRef": item.scheduleRevisionRef,
			"scheduleRevision": item.scheduleRevision, "scheduleRevisionDigest": item.scheduleRevisionDigest,
			"inputDigest": item.inputDigest})
	}
	remaining := int(limit) - len(result)
	if remaining == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, errs.ErrConflict
		}
		return result, nil
	}
	rows, err := tx.Query(ctx, queryWorkersClaimdueschedulesSelectSchedulesOrganizationId, scope.organizationID, remaining)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// dueSchedule holds one due schedule and its exact current revision, timezone/DST/misfire/overlap
	// policy, target, input/prompt and initiating actor snapshot while query rows are closed before
	// writes.
	// dueSchedule хранит due schedule и точную current revision, timezone/DST/misfire/overlap policy,
	// target, input/prompt и снимок исходного субъекта; строки запроса закрываются до записей.
	type dueSchedule struct {
		id, ref, preset, cron, timezone, name, targetType, targetRef string
		currentRevisionID, currentRevisionRef, currentRevisionDigest string
		input, promptInputs                                          []byte
		dstGapPolicy, dstFoldPolicy, misfirePolicy, overlapPolicy    string
		targetDigest, automationText                                 string
		initiatedBy                                                  string
		scheduledFor                                                 time.Time
		version, currentRevision, targetVersion                      int64
	}
	due := make([]dueSchedule, 0, limit)
	for rows.Next() {
		var item dueSchedule
		if err := rows.Scan(&item.id, &item.ref, &item.scheduledFor, &item.version, &item.preset, &item.cron, &item.timezone,
			&item.name, &item.targetType, &item.targetRef, &item.input, &item.currentRevisionID,
			&item.currentRevisionRef, &item.currentRevision, &item.currentRevisionDigest,
			&item.dstGapPolicy, &item.dstFoldPolicy, &item.misfirePolicy, &item.overlapPolicy,
			&item.targetVersion, &item.targetDigest, &item.automationText, &item.promptInputs, &item.initiatedBy); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		due = append(due, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	for _, item := range due {
		occurrence, next, nextErr := scheduleservice.ResolveDue(scheduleservice.Spec{
			Preset: item.preset, CronExpression: item.cron, Timezone: item.timezone,
			DSTGapPolicy: item.dstGapPolicy, DSTFoldPolicy: item.dstFoldPolicy,
			MisfirePolicy: item.misfirePolicy, OverlapPolicy: item.overlapPolicy,
		}, item.scheduledFor, now)
		if nextErr != nil {
			return nil, errs.ErrUnavailable
		}
		tag, updateErr := tx.Exec(ctx, queryWorkersClaimdueschedulesUpdateSchedulesNextRunAt, item.id, next, item.scheduledFor)
		if updateErr != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		skipped := occurrence == nil
		if skipped {
			occurrence = &item.scheduledFor
		}
		occurrenceRef, _ := newRef("occ")
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		inputDigest := sha256.Sum256(item.input)
		automationTextDigest := sha256.Sum256([]byte(item.automationText))
		item.promptInputs, err = repository.captureSchedulePromptTx(ctx, tx, scope, item.ref, item.promptInputs)
		if err != nil {
			return nil, err
		}
		promptInputsDigest := schedulePromptDigest(1, item.promptInputs)
		expires := now.Add(30 * time.Second)
		var occurrenceID string
		if err := tx.QueryRow(ctx, queryWorkersClaimdueschedulesInsertScheduleOccurrencesRefScheduleIdState, occurrenceRef, scope.organizationID, item.id, *occurrence, item.version, item.targetType, item.targetRef, item.name, item.input, hex.EncodeToString(inputDigest[:]), leaseRef, hex.EncodeToString(digest[:]), instance, expires, item.currentRevisionID, item.targetVersion, item.targetDigest, item.automationText, hex.EncodeToString(automationTextDigest[:]), item.promptInputs, promptInputsDigest, item.initiatedBy, skipped).Scan(&occurrenceID); err != nil {
			return nil, mapWriteError(err)
		}
		if skipped {
			if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
				return nil, err
			}
			continue
		}
		attemptRef, _ := newRef("satt")
		if _, err := tx.Exec(ctx, queryWorkersClaimdueschedulesInsertAttempt, attemptRef, scope.organizationID, occurrenceID,
			int32(1), int64(1), leaseRef, hex.EncodeToString(digest[:]), instance,
			hex.EncodeToString(inputDigest[:]), item.currentRevisionDigest, expires, principal.CredentialRevision); err != nil {
			return nil, errs.ErrUnavailable
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"scheduleRef": item.ref, "occurrenceRef": occurrenceRef, "scheduledFor": *occurrence,
			"leaseRef": leaseRef, "fence": fence, "generation": int64(1), "expiresAt": expires,
			"attempt": int32(1), "targetRef": item.targetRef, "targetVersion": item.targetVersion,
			"targetDigest": item.targetDigest, "automationTextDigest": hex.EncodeToString(automationTextDigest[:]),
			"promptInputsDigest": promptInputsDigest,
			"scheduleVersion":    item.version, "scheduleRevisionRef": item.currentRevisionRef,
			"scheduleRevision": item.currentRevision, "scheduleRevisionDigest": item.currentRevisionDigest,
			"inputDigest": hex.EncodeToString(inputDigest[:])})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// RenewScheduleOccurrence validates nonempty occurrence/lease/fence and positive generation, then
// renews the exact organization and credential-revision-bound lease in Serializable. It returns
// the persisted lease/generation/expiry with the same fence only after commit; a rejected renewal
// is Forbidden and commit failure Conflict.
// RenewScheduleOccurrence проверяет непустые occurrence/lease/fence и положительное generation,
// затем продлевает точную lease организации и credential revision в Serializable. Возвращает
// сохранённые lease/generation/expiry с прежним fence лишь после commit; отказ продления даёт
// Forbidden, отказ commit — Conflict.
func (repository *Repository) RenewScheduleOccurrence(ctx context.Context, principal value.Principal, input command.OccurrenceInput) (map[string]any, error) {
	if input.OccurrenceRef == "" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 {
		return nil, errs.ErrInvalid
	}
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	digest := sha256.Sum256([]byte(input.Fence))
	var leaseRef string
	var generation int64
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleOccurrenceRenew, scope.organizationID, input.OccurrenceRef,
		input.LeaseRef, hex.EncodeToString(digest[:]), input.Generation, principal.CredentialRevision).Scan(&leaseRef, &generation, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return nil, errs.ErrForbidden
	} else if err != nil {
		return nil, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return map[string]any{"leaseRef": leaseRef, "fence": input.Fence, "generation": generation, "expiresAt": expiresAt}, nil
}

// changeOccurrence locks a credential-bound occurrence, verifies fence/generation/database-time
// expiry and immutable input/automation/prompt pins, then handles failure or materialization
// separately. Failure closes the attempt, transitions retry/dead-letter state and emits
// invalidation. Materialization rechecks active schedule/target pins, authorizes launch as the
// persisted initiator, creates the run and atomically marks the attempt materialized. It uses the
// caller transaction and preserves existing read-error conflation and readback.
// changeOccurrence блокирует occurrence с credential binding, проверяет fence/generation/expiry по
// времени БД и immutable input/automation/prompt pins, отдельно обрабатывает отказ и
// materialization. Отказ закрывает attempt, меняет retry/dead-letter state и создаёт invalidation.
// Materialization повторно проверяет active schedule/target pins, авторизует запуск от
// сохранённого инициатора, создаёт run и атомарно отмечает attempt materialized. Использует
// транзакцию вызывающего, сохраняет прежнее объединение ошибок чтения и readback.
func (repository *Repository) changeOccurrence(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.OccurrenceInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var occurrenceID, scheduleID, projectID, projectRef, state, storedDigest, targetType, targetRef, name, storedInputDigest string
	var revisionDigest, targetDigest, automationText, automationTextDigest, promptInputsDigest string
	var promptInputFormat int
	var initiatedBy, initiatorRef, initiatorName string
	var sessionPolicy, continueSessionRef string
	var scheduleVersion, generation, targetVersion int64
	var attempt int32
	var occurrenceInput, promptInputs []byte
	var expires time.Time
	err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectScheduleOccurrencesOrganizationIdRefLeaseRef, scope.organizationID, payload.OccurrenceRef, payload.LeaseRef, input.Principal.CredentialRevision).Scan(&occurrenceID, &scheduleID, &projectID, &projectRef, &state, &storedDigest, &generation, &expires, &targetType, &targetRef, &name, &occurrenceInput, &scheduleVersion, &storedInputDigest, &attempt, &revisionDigest, &targetVersion, &targetDigest, &automationText, &automationTextDigest, &promptInputs, &promptInputsDigest, &initiatedBy, &initiatorRef, &initiatorName, &sessionPolicy, &continueSessionRef, &promptInputFormat)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	var now time.Time
	if err := tx.QueryRow(ctx, queryWorkersScheduleClock).Scan(&now); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || !now.Before(expires) {
		return commandOutcome{}, errs.ErrForbidden
	}
	inputDigest := sha256.Sum256(occurrenceInput)
	storedAutomationDigest := sha256.Sum256([]byte(automationText))
	storedPromptInputsDigest := schedulePromptDigest(promptInputFormat, promptInputs)
	if storedInputDigest != hex.EncodeToString(inputDigest[:]) ||
		automationTextDigest != hex.EncodeToString(storedAutomationDigest[:]) ||
		promptInputsDigest != storedPromptInputsDigest ||
		scheduleVersion < 1 || targetVersion < 1 || revisionDigest == "" || initiatedBy == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if state != "CLAIMED" {
		return commandOutcome{}, errs.ErrConflict
	}
	if input.Kind == command.FailScheduleOccurrence {
		if !validScheduleErrorCode(payload.SafeErrorCode) {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, occurrenceID, "FAILED", payload.SafeErrorCode, attempt, generation); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		var nextState string
		var nextAttempt int32
		if err := tx.QueryRow(ctx, queryWorkersScheduleOccurrenceFail, occurrenceID, payload.Retryable, payload.SafeErrorCode).Scan(&nextState, &nextAttempt); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if err := repository.emitScheduleOccurrenceChange(ctx, tx, scope, occurrenceID); err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Runtime: map[string]any{"state": nextState, "attempt": nextAttempt}},
			projectID: projectID, projectRef: projectRef, resourceKind: "SCHEDULE_OCCURRENCE",
			resourceRef: payload.OccurrenceRef, summary: "i18n:SCHEDULE_OCCURRENCE_FAILED"}, nil
	}
	var schedule entity.Schedule
	var scheduleInput []byte
	if err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectSchedulesId, scheduleID).Scan(&schedule.Ref, &schedule.ProjectRef, &schedule.Name, &schedule.Target.Type, &schedule.Target.Ref, &schedule.Target.Name, &schedule.Preset, &schedule.CronExpression, &schedule.Timezone, &scheduleInput, &schedule.SessionPolicy, &schedule.NotificationPolicy, &schedule.State, &schedule.Enabled, &schedule.Version, &schedule.NextRunAt, &schedule.LastRunAt, &schedule.CreatedAt, &schedule.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if schedule.State != "ACTIVE" || !schedule.Enabled {
		return commandOutcome{}, errs.ErrConflict
	}
	if json.Unmarshal(scheduleInput, &schedule.Input) != nil || attachScheduleDisplay(&schedule) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule.NextActions = scheduleActions(schedule, true)
	currentTargetVersion, currentTargetDigest, err := repository.validateScheduleTarget(ctx, tx, scope.organizationID, projectID, entity.RunTarget{Type: targetType, Ref: targetRef})
	if err != nil || currentTargetVersion != targetVersion || currentTargetDigest != targetDigest {
		return commandOutcome{}, errs.ErrConflict
	}
	var immutableInput map[string]any
	if _, err := decodeSchedulePromptCapture(promptInputFormat, promptInputs); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if json.Unmarshal(occurrenceInput, &immutableInput) != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	nested := input
	nested.Kind = command.LaunchRun
	if sessionPolicy != "CONTINUE_ONE" {
		continueSessionRef = ""
	}
	nested.Payload = command.LaunchRunInput{ProjectRef: projectRef, Title: name, Task: automationText, Source: "SCHEDULE", SessionRef: continueSessionRef, Target: entity.RunTarget{Type: targetType, Ref: targetRef}, Input: immutableInput}
	ownerScope := scope
	ownerScope.actorID, ownerScope.actorRef, ownerScope.actorName = initiatedBy, initiatorRef, initiatorName
	if err := repository.authorizeCommand(ctx, tx, ownerScope, nested); err != nil {
		return commandOutcome{}, err
	}
	outcome, err := repository.launchRun(ctx, tx, ownerScope, nested)
	if err != nil {
		return commandOutcome{}, err
	}
	var runID string
	if err := tx.QueryRow(ctx, queryWorkersChangeoccurrenceSelectRunsRef, outcome.result.Run.Ref).Scan(&runID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag, updateErr := tx.Exec(ctx, queryWorkersChangeoccurrenceUpdateScheduleOccurrencesStateRunIdVersion, occurrenceID, runID); updateErr != nil || tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryWorkersScheduleAttemptFinish, occurrenceID, "MATERIALIZED", "", attempt, generation); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	schedule, err = scanSchedule(tx.QueryRow(ctx, queryQueriesGetscheduleSelectSchedulesOrganizationIdRef, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "schedule_ref": schedule.Ref,
		"role": scope.role, "actor_id": initiatedBy,
	}))
	if err != nil {
		return commandOutcome{}, err
	}
	outcome.resourceKind = "SCHEDULE_OCCURRENCE"
	outcome.resourceRef = payload.OccurrenceRef
	outcome.summary = "i18n:SCHEDULE_OCCURRENCE_MATERIALIZED"
	outcome.result.Schedule = &schedule
	return outcome, nil
}

// emitScheduleOccurrenceChange reads the exact organization occurrence event scope/version and
// emits a versioned SCHEDULE_CHANGED platform snapshot in the same transaction. A failed scope
// read becomes Unavailable; this helper does not commit or mutate the occurrence.
// emitScheduleOccurrenceChange читает точные event scope/version occurrence организации и создаёт
// версионный SCHEDULE_CHANGED platform snapshot в той же транзакции. Отказ чтения scope даёт
// Unavailable; helper не выполняет commit и не меняет occurrence.
func (repository *Repository) emitScheduleOccurrenceChange(ctx context.Context, tx pgx.Tx, scope scope, occurrenceID string) error {
	var scheduleRef, projectRef string
	var version int64
	if err := tx.QueryRow(ctx, queryWorkersScheduleEventScope, occurrenceID, scope.organizationID).Scan(&scheduleRef, &projectRef, &version); err != nil {
		return errs.ErrUnavailable
	}
	return repository.emitPlatformEventSnapshot(ctx, tx, scope, "SCHEDULE_CHANGED", projectRef, scheduleRef, "i18n:SCHEDULE_UPDATED", version, "")
}

// validScheduleErrorCode accepts a nonempty code of at most64 bytes containing only uppercase
// ASCII letters, digits and underscore; it validates safe syntax rather than a semantic
// provider-code allowlist.
// validScheduleErrorCode принимает непустой код до64 байт лишь с uppercase ASCII буквами, цифрами
// и underscore; проверяет безопасный синтаксис, не семантический список provider codes.
func validScheduleErrorCode(value string) bool {
	if value == "" || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'A' || character > 'Z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

// beginIntegrationClaimTransaction validates the closed workload route before scope resolution,
// then opens a Serializable transaction with the same caller context. Route/scope errors are
// preserved and begin failures map to Unavailable with no usable transaction. Test/invocation
// callers retain separate expiry/selection/lease policies and own rollback/commit; this shared
// preparation neither issues a claim nor grants resource authority.
// beginIntegrationClaimTransaction проверяет закрытый workload route до разрешения области, затем
// открывает Serializable транзакцию с прежним контекстом вызывающего. Route/scope errors
// сохраняет, отказ begin отображает в Unavailable без пригодной транзакции. Test/invocation
// callers сохраняют разные expiry/selection/lease policies и владение rollback/commit; общая
// подготовка не захватывает work и не предоставляет resource authority.
func (repository *Repository) beginIntegrationClaimTransaction(ctx context.Context, principal value.Principal) (scope, pgx.Tx, string, error) {
	route, err := integrationExecutionRoute(principal.CallerWorkload)
	if err != nil {
		return scope{}, nil, "", err
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return scope{}, nil, "", err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return scope{}, nil, "", errs.ErrUnavailable
	}
	return current, tx, route, nil
}

// ClaimIntegrationConnectionTests opens a workload-route-scoped Serializable claim, expires stale
// tests and enqueues managed-MCP refresh only for integration-gateway. It loads candidates,
// requires a pinned READ/no-approval health capability and assigns fresh30-second fenced leases,
// returning public configuration plus credential descriptors when applicable after commit.
// Test/provider effects are not executed here; optional configuration JSON errors remain ignored.
// ClaimIntegrationConnectionTests открывает workload-route-scoped Serializable claim, обрабатывает
// истёкшие tests и добавляет managed-MCP refresh лишь для integration-gateway. Читает candidates,
// требует pinned READ/no-approval health capability, назначает свежие30-секундные fenced leases,
// возвращает public configuration и допустимые credential descriptors после commit. Test/provider
// effects здесь не исполняются; ошибки optional configuration JSON по-прежнему игнорируются.
func (repository *Repository) ClaimIntegrationConnectionTests(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, tx, route, err := repository.beginIntegrationClaimTransaction(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationtestsExpireStaleTestLeases, scope.organizationID, principal.CallerWorkload); err != nil {
		return nil, errs.ErrUnavailable
	}
	if principal.CallerWorkload == "integration-gateway" {
		if err := enqueueManagedMCPHealthRefresh(ctx, tx, scope, limit); err != nil {
			return nil, err
		}
	}
	rows, err := tx.Query(ctx, queryWorkersClaimintegrationtestsSelectIntegrationConnectionTestsOrganizationIdState, scope.organizationID, limit, principal.CallerWorkload, route)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// candidate stores one selected connection-test row with internal/test identity, prior generation,
	// exact definition/configuration and optional credential metadata until rows are closed before
	// claiming. It is local to the health-test path and contains no invocation effect policy.
	// candidate хранит выбранную строку connection-test с internal/test identity, прежним generation,
	// точными definition/configuration и необязательными credential metadata до закрытия строк и
	// захвата. Локален health-test пути, не содержит invocation effect policy.
	type candidate struct {
		id, ref, connectionRef, definitionKey, definitionVersion, definitionDigest string
		generation                                                                 int64
		configuration                                                              []byte
		credential                                                                 entity.IntegrationCredentialRevision
		credentialCreatedAt                                                        *time.Time
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id, &item.ref, &item.generation, &item.connectionRef, &item.definitionKey,
			&item.configuration, &item.definitionVersion, &item.definitionDigest,
			&item.credential.Ref, &item.credential.Revision, &item.credential.SecretRef,
			&item.credential.SecretUID, &item.credential.SecretResourceVersion,
			&item.credential.ContentSHA256, &item.credentialCreatedAt,
		); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, item.connectionRef, item.definitionKey, item.definitionVersion, item.definitionDigest)
		if err != nil {
			return nil, err
		}
		health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
		if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
			return nil, errs.ErrForbidden
		}
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("fnc")
		digest := sha256.Sum256([]byte(fence))
		generation := item.generation + 1
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		tag, err := tx.Exec(ctx, queryWorkersClaimintegrationtestsClaimTestLease, item.id, leaseRef, hex.EncodeToString(digest[:]), generation, instance, expiresAt, principal.CallerWorkload)
		if err != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		configuration := map[string]any{}
		_ = json.Unmarshal(item.configuration, &configuration)
		claim := map[string]any{
			"testRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey,
			"definitionPackage": asJSON(definition),
			"definitionVersion": item.definitionVersion, "definitionDigest": item.definitionDigest,
			"configuration": configuration, "leaseRef": leaseRef, "fence": fence,
			"generation": generation, "expiresAt": expiresAt,
		}
		attachIntegrationClaimCredential(claim, definition.RequiresConnectionCredential(), &item.credential, item.credentialCreatedAt)
		result = append(result, claim)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// completeIntegrationConnectionTest validates success/error shape and optional email health
// summary, locks the exact workload test and checks its fence/generation/lease/expiry. Only a
// bounded exact owner READ-test projection/network delay is requeued; exhausted projection delay
// does not invalidate credentials. Otherwise it stores test outcome, preserves successful
// managed-refresh connection state or updates connection/credential/mailbox observation and
// current readback. Caller owns commit/audit/events.
// completeIntegrationConnectionTest проверяет success/error форму и необязательный email health
// summary, блокирует точный workload test, проверяет fence/generation/lease/expiry. Лишь
// ограниченная задержка projection/network точного owner READ-test возвращается в очередь;
// исчерпанная задержка projection не делает credentials невалидными. Иначе сохраняет test outcome,
// оставляет connection state при успешном managed-refresh либо обновляет
// connection/credential/mailbox observation и текущий readback. Commit/audit/events принадлежат
// вызывающему.
func (repository *Repository) completeIntegrationConnectionTest(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.IntegrationConnectionTestInput)
	if !ok || payload.Success && payload.SafeErrorCode != "" || !payload.Success &&
		!safeIntegrationErrorCode(payload.SafeErrorCode) && payload.SafeErrorCode != credentialProjectionPendingCode {
		return commandOutcome{}, errs.ErrInvalid
	}

	var healthSummary string
	var healthCredentialInvalid bool
	if strings.HasPrefix(payload.ResultSummary, emailbridgeapi.HealthSummaryPrefix) || payload.SafeErrorCode == emailbridgeapi.HealthNotReadyCode {
		report, err := emailbridgeapi.ParseHealthSummary(payload.ResultSummary)
		if err != nil || (report.Status == "ready") != payload.Success || !payload.Success && payload.SafeErrorCode != emailbridgeapi.HealthNotReadyCode {
			return commandOutcome{}, errs.ErrInvalid
		}
		healthSummary = payload.ResultSummary
		healthCredentialInvalid = report.CredentialInvalid()
	}
	var testID, connectionID, connectionRef, storedDigest, state, leaseRef string
	var definitionKey, connectionState, purpose, definitionVersion, definitionDigest string
	var connectionEnabled bool
	var attempt int
	var createdAt time.Time
	var generation int64
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestSelectIntegrationConnectionTestsOrganizationIdRef, scope.organizationID, payload.TestRef, input.Principal.CallerWorkload).Scan(
		&testID, &connectionID, &connectionRef, &storedDigest, &generation, &state, &leaseRef, &expiresAt,
		&attempt, &createdAt, &definitionKey, &connectionState, &connectionEnabled, &purpose, &definitionVersion, &definitionDigest,
	); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if state != "CLAIMED" || leaseRef != payload.LeaseRef || generation != payload.Generation || storedDigest != hex.EncodeToString(digest[:]) || time.Now().After(expiresAt) {
		return commandOutcome{}, errs.ErrForbidden
	}
	// Повторяется только exact owner READ-test при задержке проекции credential
	// либо прежней сетевой проекции OpenAPI, но не произвольный provider failure.
	projectionPending := payload.SafeErrorCode == credentialProjectionPendingCode
	if !payload.Success && (projectionPending || payload.SafeErrorCode == "INTEGRATION_UNAVAILABLE" && definitionKey == "openapi-mcp") &&
		purpose == "OWNER_TEST" && input.Principal.CallerWorkload == "integration-gateway" && connectionEnabled && connectionState == "TESTING" &&
		attempt < 8 && time.Now().Before(createdAt.Add(90*time.Second)) {
		definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, connectionRef, definitionKey, definitionVersion, definitionDigest)
		if err != nil {
			return commandOutcome{}, err
		}
		health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
		if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" ||
			definition.Spec.AdapterOwner != "integration-gateway" || definition.Spec.ExecutionRoute != "MANAGED_MCP" || definition.Spec.Readiness != "READY" {
			return commandOutcome{}, errs.ErrForbidden
		}
		var requeuedRef string
		if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestRequeueTransientHealth,
			testID, leaseRef, generation).Scan(&requeuedRef); err != nil || requeuedRef != payload.TestRef {
			return commandOutcome{}, errs.ErrConflict
		}
		item, err := readConnection(ctx, tx, scope, connectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION",
			resourceRef: connectionRef, summary: "i18n:INTEGRATION_CONNECTION_TEST_RETRY_SCHEDULED"}, nil
	}
	if projectionPending {
		// Исчерпанное ожидание проекции не доказывает невалидность credential.
		payload.SafeErrorCode = "INTEGRATION_UNAVAILABLE"
	}
	nextTest, nextConnection, credentials := "SUCCEEDED", "CONNECTED", "CONFIGURED"
	summary := "i18n:INTEGRATION_TEST_SUCCEEDED"
	if !payload.Success {
		nextTest, nextConnection = "FAILED", "DEGRADED"
		summary = "i18n:" + payload.SafeErrorCode
		if healthCredentialInvalid || payload.SafeErrorCode == "INTEGRATION_AUTH_REJECTED" || payload.SafeErrorCode == "INTEGRATION_CREDENTIAL_UNAVAILABLE" {
			credentials = "INVALID"
		}
	}
	if healthSummary != "" {
		summary = healthSummary
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionTestsStateResultSummarySafeErrorCode, testID, nextTest, summary, payload.SafeErrorCode); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if purpose == "MANAGED_MCP_REFRESH" && payload.Success && connectionState == "CONNECTED" {
		item, err := readConnection(ctx, tx, scope, connectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION",
			resourceRef: connectionRef, summary: "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED"}, nil
	}
	var item entity.IntegrationConnection
	if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationtestUpdateIntegrationConnectionsStateMaskedCredentialsStateLastTestSummary, connectionID, nextConnection, credentials, summary).Scan(&item.Ref, &item.DefinitionKey, &item.Name, &item.State, &item.MaskedCredentialsState, &item.LastTestSummary, &item.Enabled, &item.Version, &item.LastTestedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return commandOutcome{}, errs.ErrConflict
	}
	if err := advanceMailboxObservation(ctx, tx, scope.organizationID, connectionID, item.Version-1, item.Version); err != nil {
		return commandOutcome{}, err
	}
	item, err := readConnection(ctx, tx, scope, connectionRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Connection: &item}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: connectionRef, summary: "i18n:INTEGRATION_CONNECTION_TEST_COMPLETED", platformEvent: "INTEGRATION_CONNECTION_CHANGED"}, nil
}

// ResolveIntegrationInvocation runs the complete specialized invocation-resolution transaction
// through the existing bounded Serializable retry helper. Fresh attempts repeat
// owner/package/approval checks; it does not call the external provider or itself decide which
// error is safely retryable.
// ResolveIntegrationInvocation проводит полную специализированную транзакцию разрешения invocation
// через существующий ограниченный Serializable retry helper. Новая попытка повторяет
// owner/package/approval проверки; метод не вызывает провайдера и сам не решает, какую ошибку
// безопасно повторять.
func (repository *Repository) ResolveIntegrationInvocation(ctx context.Context, principal value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	return retrySerializableTransaction(ctx, func() (map[string]any, error) {
		return repository.resolveIntegrationInvocation(ctx, principal, input, boundedInput)
	})
}

// resolveIntegrationInvocation resolves exact run/node/connection/grant state in Serializable,
// rechecks originating-actor management, pinned package/capability/resource scope and canonical
// bounded input. It derives durable intent/effect keys, handles mailbox and HUMAN_SCOPED approval
// pins/reservation, reuses or creates an invocation and owner-gate graph/event before commit.
// Idempotency reuse is rejected; selected serialization errors retain retry classification and
// historical SQLSTATE/constraint diagnostics remain unchanged. No provider effect is sent here.
// resolveIntegrationInvocation разрешает точные run/node/connection/grant в Serializable, повторно
// проверяет управление исходного субъекта, pinned package/capability/resource scope и канонический
// ограниченный input. Вычисляет устойчивые intent/effect keys, обрабатывает mailbox и HUMAN_SCOPED
// approval pins/reservation, переиспользует либо создаёт invocation и owner-gate graph/event до
// commit. Idempotency reuse отклоняется; отдельные serialization errors сохраняют retry
// classification, исторические SQLSTATE/constraint diagnostics не меняются. Provider effect здесь
// не отправляется.
func (repository *Repository) resolveIntegrationInvocation(ctx context.Context, principal value.Principal, input map[string]string, boundedInput map[string]any) (map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	encodedInput, err := json.Marshal(boundedInput)
	if err != nil || len(encodedInput) > 512<<10 {
		return nil, errs.ErrInvalid
	}
	var runID, nodeID, connectionID, grantID, grantRef, projectID, rootRunID, initiatorRef, agentID string
	var definitionKey, definitionVersion, definitionDigest, connectionName, risk, approvalPolicy, resourceKind, resourceScopeDigest string
	var encodedScope []byte
	var grantVersion int64
	var approvalScopePaths []string
	err = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectRunsIdOrganizationIdRef,
		scope.organizationID, input["run_ref"], input["node_ref"], input["connection_ref"], input["capability_key"],
	).Scan(
		&runID, &nodeID, &connectionID, &grantID, &grantRef, &projectID, &rootRunID,
		&definitionKey, &definitionVersion, &definitionDigest, &connectionName, &risk, &approvalPolicy,
		&resourceKind, &encodedScope, &resourceScopeDigest, &initiatorRef,
		&grantVersion, &approvalScopePaths, &agentID,
	)
	if err != nil {
		if serializableTransactionConflict(err) {
			return nil, serializableTransactionError(err, errs.ErrUnavailable)
		}
		return nil, errs.ErrForbidden
	}
	initiatorScope := scope
	initiatorScope.actorRef = initiatorRef
	if err := repository.requireAccess(ctx, tx, initiatorScope, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: input["connection_ref"]}); err != nil {
		return nil, errs.ErrForbidden
	}
	definition, packageErr := repository.integrationPackage(ctx, tx, scope.organizationID, input["connection_ref"], definitionKey, definitionVersion, definitionDigest)
	capability, capabilityExists := definition.Capability(input["capability_key"])
	if packageErr != nil || !capabilityExists || definition.Metadata.Version != definitionVersion || definition.Digest != definitionDigest ||
		capability.Risk != risk || !capability.AllowsApprovalPolicy(approvalPolicy) || capability.ResourceScope.Kind != resourceKind || grantVersion < 1 {
		return nil, errs.ErrForbidden
	}
	if definitionKey == "context7" {
		if err := requireCurrentManagedMCPHealth(ctx, tx, scope.organizationID, nodeID, input["connection_ref"]); err != nil {
			return nil, err
		}
	}
	canonicalInput, err := capability.ValidateInput(encodedInput)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	if definition.ValidateInvocationApprovalPolicy(capability, approvalPolicy, canonicalInput) != nil {
		return nil, errs.ErrForbidden
	}
	if len(encodedInput) > 64<<10 && (definitionKey != "github" || capability.Operation != "github.repository.content.update") {
		return nil, errs.ErrInvalid
	}
	var resourceScope map[string]string
	if json.Unmarshal(encodedScope, &resourceScope) != nil {
		return nil, errs.ErrUnavailable
	}
	invocationRef, _ := newRef("inv")
	inputDigest := sha256.Sum256(canonicalInput)
	inputDigestHex := hex.EncodeToString(inputDigest[:])
	intentParts := []string{
		input["node_ref"], input["idempotency_key"], input["connection_ref"], input["capability_key"],
		inputDigestHex, definitionDigest, resourceScopeDigest, grantRef, strconv.FormatInt(grantVersion, 10), approvalPolicy,
	}
	mailboxGate := false
	if definitionKey == "email" {
		mailbox, err := repository.readEmailMailbox(ctx, tx, scope, resourceScope["mailbox_id"], 0)
		if err != nil {
			return nil, err
		}
		if mailbox.ConnectionRef != input["connection_ref"] {
			return nil, errs.ErrForbidden
		}
		intentParts = append(intentParts, mailbox.SourceDigest)
		mailboxGate, err = emailpolicy.CommandRequiresGate(mailbox, capability.Operation, "", canonicalInput)
		if err != nil {
			return nil, err
		}
	}
	intentDigest := sha256.Sum256([]byte(strings.Join(intentParts, "\x00")))
	intentDigestHex := hex.EncodeToString(intentDigest[:])
	effectKey := "eff_" + intentDigestHex[:32]
	state := "READY"
	if approvalPolicy == "HUMAN_EACH_EFFECT" || mailboxGate {
		state = "WAITING_APPROVAL"
	}
	var approvalScopeID string
	pinnedGrantVersion := grantVersion
	pinnedApprovalScopePaths := []string{}
	if approvalPolicy == string(integrationpackage.ApprovalHumanScoped) {
		if agentID == "" || grantVersion < 1 || mailboxGate {
			return nil, errs.ErrForbidden
		}
		approvalScope, scopeErr := capability.ResolveApprovalScope(approvalScopePaths, canonicalInput)
		if scopeErr != nil {
			return nil, errs.ErrInvalid
		}
		schemaDigest, digestErr := capability.InputSchemaDigest()
		if digestErr != nil {
			return nil, errs.ErrUnavailable
		}
		paths := sortedApprovalScopePaths(approvalScopePaths)
		pinnedGrantVersion, pinnedApprovalScopePaths = grantVersion, paths
		scopeErr = tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectApprovalScope,
			scope.organizationID, projectID, connectionID, grantID, rootRunID, agentID,
			input["capability_key"], grantVersion, definitionDigest, schemaDigest, paths, approvalScope.Digest,
		).Scan(&approvalScopeID)
		if scopeErr != nil && !errors.Is(scopeErr, pgx.ErrNoRows) {
			return nil, serializableTransactionError(scopeErr, errs.ErrUnavailable)
		}
		state = "WAITING_APPROVAL"
		if approvalScopeID != "" {
			state = "READY"
		}
	}
	var approvalScopeBinding any
	if approvalScopeID != "" {
		approvalScopeBinding = approvalScopeID
	}
	var invocationID, resolvedRef, resolvedState string
	if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertIntegrationInvocationsRefRunIdConnectionId,
		invocationRef, scope.organizationID, runID, nodeID, connectionID, grantID, input["capability_key"],
		capability.Operation, input["idempotency_key"], intentDigestHex, inputDigestHex, canonicalInput, state,
		definitionVersion, definitionDigest, risk, approvalPolicy, resourceKind, encodedScope, resourceScopeDigest, effectKey, mailboxGate, approvalScopeBinding,
		pinnedGrantVersion, pinnedApprovalScopePaths,
	).Scan(&invocationID, &resolvedRef, &resolvedState); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrIdempotencyReuse
		}
		var databaseError *pgconn.PgError
		if errors.As(err, &databaseError) {
			return nil, fmt.Errorf("insert integration invocation: sqlstate=%s constraint=%s: %w",
				databaseError.Code, databaseError.ConstraintName, mapWriteError(err))
		}
		return nil, serializableTransactionError(err, mapWriteError(err))
	}
	if approvalScopeID != "" && resolvedRef == invocationRef {
		tag, reserveErr := tx.Exec(ctx, queryWorkersResolveintegrationinvocationReserveApprovalScope, approvalScopeID)
		if reserveErr != nil || tag.RowsAffected() != 1 {
			return nil, serializableTransactionError(reserveErr, errs.ErrConflict)
		}
	}
	gateRef := ""
	if resolvedState == "WAITING_APPROVAL" {
		err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationSelectGate, invocationID).Scan(&gateRef)
		if errors.Is(err, pgx.ErrNoRows) {
			gateNodeRef, _ := newRef("nod")
			var gateNodeID string
			if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertGateNode,
				gateNodeRef, scope.organizationID, rootRunID, runID, nodeID,
			).Scan(&gateNodeID); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			edgeRef, _ := newRef("edg")
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationInsertGateEdge,
				edgeRef, scope.organizationID, rootRunID, nodeID, gateNodeID,
			); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			gateRef, _ = newRef("gat")
			var gateID string
			if err := tx.QueryRow(ctx, queryWorkersResolveintegrationinvocationInsertOwnerGate,
				gateRef, scope.organizationID, projectID, rootRunID, gateNodeID,
				integrationGateContextSummary(connectionName, capability.Name, capability.Operation), invocationID,
			).Scan(&gateID); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			if _, err := tx.Exec(ctx, queryWorkersResolveintegrationinvocationUpdateRunWaitingHuman, rootRunID); err != nil {
				return nil, serializableTransactionError(err, errs.ErrUnavailable)
			}
			if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, gateRef,
				"OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "i18n:INTEGRATION_EFFECT_OWNER_DECISION_REQUIRED",
				"WAITING_HUMAN", "WAITING",
			); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return map[string]any{
		"invocationRef": resolvedRef, "grantRef": grantRef, "operation": capability.Operation,
		"state": resolvedState, "gateRef": gateRef, "risk": risk, "resourceKind": resourceKind,
		"resourceScope": resourceScope, "resourceScopeDigest": resourceScopeDigest, "projectID": projectID,
	}, nil
}

// attachIntegrationClaimCredential attaches a credential descriptor only when the pinned
// definition requires it and both revision reference and creation time are present, updating the
// same caller-owned descriptor timestamp. Callers supply a valid descriptor pointer and a writable
// claim map for an attached credential; omission preserves any existing map entry. It neither
// reads secret values nor grants access, renews a lease or commits a transaction.
// attachIntegrationClaimCredential добавляет credential descriptor лишь при требовании pinned
// definition и наличии revision reference и времени создания, обновляя timestamp того же
// descriptor вызывающего. Caller передаёт допустимый указатель descriptor и доступную для записи
// claim map при добавлении; пропуск сохраняет возможную прежнюю запись карты. Helper не читает
// значения секретов, не даёт доступа, не продлевает lease и не выполняет commit.
func attachIntegrationClaimCredential(claim map[string]any, required bool, credential *entity.IntegrationCredentialRevision, createdAt *time.Time) {
	if required && credential.Ref != "" && createdAt != nil {
		credential.CreatedAt = *createdAt
		claim["credential"] = *credential
	}
}

// integrationExecutionRoute maps only integration-gateway to MANAGED_MCP and interaction-gateway
// to INTERACTION; every other workload is Forbidden. Route selection does not replace exact
// permission, tenant or invocation authorization.
// integrationExecutionRoute сопоставляет только integration-gateway с MANAGED_MCP и
// interaction-gateway с INTERACTION; остальные workload получают Forbidden. Выбор маршрута не
// заменяет точные permission, tenant и invocation authorization.
func integrationExecutionRoute(workload string) (string, error) {
	switch workload {
	case "integration-gateway":
		return "MANAGED_MCP", nil
	case "interaction-gateway":
		return "INTERACTION", nil
	default:
		return "", errs.ErrForbidden
	}
}

// ClaimIntegrationInvocations expires stale leases in the exact workload route, loads candidates
// and rechecks originating-actor access, current managed health and pinned capability/approval
// scope. It skips expired execution deadlines, caps lease expiry to a remaining deadline, assigns
// a new fence/generation and returns EXECUTE or RECOVER_READ_ONLY for UNKNOWN_OUTCOME after
// commit. Optional JSON decode and reference-generation behavior stay unchanged; recovery mode
// does not authorize resending an effect.
// ClaimIntegrationInvocations обрабатывает истёкшие leases точного workload route, читает
// candidates, повторно проверяет доступ исходного субъекта, current managed health и pinned
// capability/approval scope. Пропускает истёкшие execution deadlines, ограничивает expiry
// оставшимся deadline, назначает новые fence/generation, возвращает EXECUTE либо RECOVER_READ_ONLY
// для UNKNOWN_OUTCOME после commit. Optional JSON decode и generation refs сохраняют прежнее
// поведение; recovery mode не разрешает повторно отправить effect.
func (repository *Repository) ClaimIntegrationInvocations(ctx context.Context, principal value.Principal, instance string, limit int32) ([]map[string]any, error) {
	scope, tx, route, err := repository.beginIntegrationClaimTransaction(ctx, principal)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsExpireStaleInvocationLeases, scope.organizationID, principal.CallerWorkload); err != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryWorkersClaimintegrationinvocationsSelectIntegrationInvocationsOrganizationIdState, scope.organizationID, limit, principal.CallerWorkload, route)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// candidate pins one invocation candidate to run/node/initiator, grant/version, exact
	// package/capability/risk/approval/resource scope, input/effect digests and credential metadata.
	// It is distinct from health-test candidates and is revalidated before any lease claim.
	// candidate связывает кандидата invocation с run/node/initiator, grant/version, точными
	// package/capability/risk/approval/resource scope, input/effect digests и credential metadata.
	// Отличается от health-test candidate, повторно проверяется до захвата lease.
	type candidate struct {
		id, ref, state, connectionRef, definitionKey, capabilityKey, nodeID, runID string
		initiatorRef                                                               string
		definitionVersion, definitionDigest, operation, risk, approvalPolicy       string
		resourceKind, resourceScopeDigest, effectKey, inputDigest                  string
		approvalScopeDigest, approvalSchemaDigest                                  string
		approvalScopePaths                                                         []string
		grantRef                                                                   string
		grantVersion                                                               int64
		generation                                                                 int64
		configuration, boundedInput, resourceScope                                 []byte
		credential                                                                 entity.IntegrationCredentialRevision
		credentialCreatedAt                                                        *time.Time
	}
	candidates := make([]candidate, 0, limit)
	for rows.Next() {
		var item candidate
		if err := rows.Scan(
			&item.id, &item.ref, &item.generation, &item.state, &item.connectionRef, &item.definitionKey,
			&item.configuration, &item.capabilityKey, &item.boundedInput, &item.definitionVersion,
			&item.definitionDigest, &item.operation, &item.risk, &item.approvalPolicy,
			&item.resourceKind, &item.resourceScope, &item.resourceScopeDigest, &item.effectKey,
			&item.inputDigest, &item.credential.Ref, &item.credential.Revision, &item.credential.SecretRef,
			&item.credential.SecretUID, &item.credential.SecretResourceVersion, &item.credential.ContentSHA256,
			&item.credentialCreatedAt, &item.initiatorRef,
			&item.approvalScopePaths, &item.approvalScopeDigest, &item.approvalSchemaDigest,
			&item.grantRef, &item.grantVersion, &item.nodeID, &item.runID,
		); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]map[string]any, 0, len(candidates))
	for _, item := range candidates {
		initiatorScope := scope
		initiatorScope.actorRef = item.initiatorRef
		if err := repository.requireAccess(ctx, tx, initiatorScope, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: item.connectionRef}); err != nil {
			continue
		}
		if item.definitionKey == "context7" {
			if err := requireCurrentManagedMCPHealth(ctx, tx, scope.organizationID, item.nodeID, item.connectionRef); err != nil {
				return nil, err
			}
		}
		definition, err := repository.integrationPackage(ctx, tx, scope.organizationID, item.connectionRef, item.definitionKey, item.definitionVersion, item.definitionDigest)
		if err != nil {
			return nil, err
		}
		capability, capabilityOK := definition.Capability(item.capabilityKey)
		if !capabilityOK || item.grantVersion < 1 || item.grantRef == "" ||
			capability.Risk != item.risk || capability.Operation != item.operation ||
			definition.ValidateInvocationApprovalPolicy(capability, item.approvalPolicy, item.boundedInput) != nil {
			return nil, errs.ErrForbidden
		}
		if item.approvalPolicy == string(integrationpackage.ApprovalHumanScoped) {
			resolvedScope, scopeErr := capability.ResolveApprovalScope(item.approvalScopePaths, item.boundedInput)
			schemaDigest, digestErr := capability.InputSchemaDigest()
			if scopeErr != nil || digestErr != nil || resolvedScope.Digest != item.approvalScopeDigest ||
				schemaDigest != item.approvalSchemaDigest {
				return nil, errs.ErrForbidden
			}
		}
		leaseRef, _ := newRef("lea")
		fence, _ := newRef("eff")
		digest := sha256.Sum256([]byte(fence))
		generation := item.generation + 1
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		deadline, expired, deadlineErr := readRuntimeExecutionDeadline(ctx, tx, scope.organizationID, item.runID)
		if deadlineErr != nil {
			return nil, deadlineErr
		}
		if expired {
			continue
		}
		if deadline != nil && deadline.EffectiveDeadlineAt.Before(expiresAt) {
			expiresAt = deadline.EffectiveDeadlineAt
		}
		tag, err := tx.Exec(ctx, queryWorkersClaimintegrationinvocationsClaimInvocationLease, item.id, leaseRef, hex.EncodeToString(digest[:]), generation, instance, expiresAt, principal.CallerWorkload, item.state)
		if err != nil || tag.RowsAffected() != 1 {
			return nil, errs.ErrConflict
		}
		configuration, bounded := map[string]any{}, map[string]any{}
		resourceScope := map[string]string{}
		_ = json.Unmarshal(item.configuration, &configuration)
		_ = json.Unmarshal(item.boundedInput, &bounded)
		_ = json.Unmarshal(item.resourceScope, &resourceScope)
		workMode := "EXECUTE"
		if item.state == "UNKNOWN_OUTCOME" {
			workMode = "RECOVER_READ_ONLY"
		}
		claim := map[string]any{
			"invocationRef": item.ref, "connectionRef": item.connectionRef, "definitionKey": item.definitionKey,
			"definitionPackage": asJSON(definition),
			"capabilityKey":     item.capabilityKey, "configuration": configuration, "boundedInput": bounded,
			"definitionVersion": item.definitionVersion, "definitionDigest": item.definitionDigest,
			"operation": item.operation, "risk": item.risk, "approvalPolicy": item.approvalPolicy,
			"grantRef": item.grantRef, "grantVersion": item.grantVersion,
			"resourceKind": item.resourceKind, "resourceScope": resourceScope,
			"resourceScopeDigest": item.resourceScopeDigest, "effectKey": item.effectKey, "inputDigest": item.inputDigest,
			"workMode": workMode,
			"leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt,
		}
		attachIntegrationClaimCredential(claim, definition.RequiresConnectionCredential(), &item.credential, item.credentialCreatedAt)
		result = append(result, claim)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errs.ErrConflict
	}
	return result, nil
}

// GetIntegrationInvocation resolves the organization scope and reads only invocation
// state/result/error plus gate/effect-receipt references. NoRows becomes NotFound and other read
// failures Unavailable; it preserves this narrow pool-query path without adding per-resource
// authorization or a provider read.
// GetIntegrationInvocation разрешает область организации и читает лишь invocation
// state/result/error и gate/effect-receipt refs. NoRows даёт NotFound, прочие ошибки чтения —
// Unavailable; сохраняет узкий pool-query путь без добавления per-resource authorization и чтения
// провайдера.
func (repository *Repository) GetIntegrationInvocation(ctx context.Context, principal value.Principal, ref string) (map[string]any, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, err
	}
	var state, resultSummary, safeErrorCode, gateRef, receiptRef string
	if err := repository.pool.QueryRow(ctx, queryWorkersGetintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, ref).Scan(&state, &resultSummary, &safeErrorCode, &gateRef, &receiptRef); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrNotFound
		}
		return nil, errs.ErrUnavailable
	}
	return map[string]any{"state": state, "resultSummary": resultSummary, "safeErrorCode": safeErrorCode, "gateRef": gateRef, "effectReceiptRef": receiptRef}, nil
}

// completeIntegrationInvocation validates workload and outcome/receipt shape, including exact
// response digest, then locks the invocation. An exact successful stored receipt replays before
// fresh lease checks; a new outcome requires matching running lease/fence/generation/expiry ;
// success additionally matches effect/input pins. Success persists a receipt, terminal
// success/failure schedules continuation, UNKNOWN_OUTCOME does not; all paths return graph
// readback in the caller transaction without provider resend.
// completeIntegrationInvocation проверяет workload и outcome/receipt форму, включая точный
// response digest, затем блокирует invocation. Точная сохранённая successful receipt повторяется
// до свежих lease checks; новый исход требует совпавшие running lease/fence/generation/expiry ;
// success дополнительно сверяет effect/input pins. Success сохраняет receipt, terminal
// success/failure планирует продолжение, UNKNOWN_OUTCOME — нет; пути возвращают graph readback в
// транзакции вызывающего без повторной отправки провайдеру.
func (repository *Repository) completeIntegrationInvocation(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	if _, err := integrationExecutionRoute(input.Principal.CallerWorkload); err != nil {
		return commandOutcome{}, err
	}
	payload, ok := input.Payload.(command.IntegrationInvocationInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success && payload.UnknownOutcome || payload.UnknownOutcome != (payload.SafeErrorCode == "INTEGRATION_OUTCOME_UNKNOWN") ||
		len(payload.ResultSummary) > 64<<10 ||
		payload.Success && (payload.SafeErrorCode != "" || payload.EffectKey == "" || payload.InputDigest == "" ||
			payload.ProviderEffectRef == "" || len(payload.ResponseDigest) != sha256.Size*2 || payload.ResultSummary == "") ||
		!payload.Success && (!safeIntegrationErrorCode(payload.SafeErrorCode) || payload.EffectKey != "" ||
			payload.InputDigest != "" || payload.ProviderEffectRef != "" || payload.ResponseDigest != "") {
		return commandOutcome{}, errs.ErrInvalid
	}
	if payload.Success {
		digest := sha256.Sum256([]byte(payload.ResultSummary))
		if hex.EncodeToString(digest[:]) != payload.ResponseDigest {
			return commandOutcome{}, errs.ErrInvalid
		}
	}
	var invocationID, invocationRef, runID, rootRunID, projectID, projectRef, nodeRef, storedDigest, state, leaseRef string
	var effectKey, inputDigest, receiptRef, receiptEffectKey, receiptInputDigest string
	var receiptProviderRef, receiptResponseDigest, receiptResult string
	var generation int64
	var expiresAt *time.Time
	err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationSelectIntegrationInvocationsOrganizationIdRef, scope.organizationID, payload.InvocationRef, input.Principal.CallerWorkload).Scan(
		&invocationID, &invocationRef, &runID, &rootRunID, &projectID, &projectRef, &nodeRef, &storedDigest,
		&generation, &state, &leaseRef, &expiresAt, &effectKey, &inputDigest, &receiptRef,
		&receiptEffectKey, &receiptInputDigest, &receiptProviderRef, &receiptResponseDigest, &receiptResult,
	)
	if err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if state == "SUCCEEDED" && payload.Success && receiptRef != "" &&
		receiptEffectKey == payload.EffectKey && receiptInputDigest == payload.InputDigest &&
		receiptProviderRef == payload.ProviderEffectRef && receiptResponseDigest == payload.ResponseDigest &&
		receiptResult == payload.ResultSummary {
		runRef, runErr := mustRunRef(ctx, tx, runID)
		if runErr != nil {
			return commandOutcome{}, runErr
		}
		run, graph, readErr := repository.readRunGraphTx(ctx, tx, scope, runRef)
		if readErr != nil {
			return commandOutcome{}, readErr
		}
		return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Duplicate: true}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "i18n:INTEGRATION_INVOCATION_COMPLETED"}, nil
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || state != "RUNNING" || leaseRef != payload.LeaseRef ||
		generation != payload.Generation || expiresAt == nil || time.Now().After(*expiresAt) ||
		payload.Success && (effectKey != payload.EffectKey || inputDigest != payload.InputDigest) {
		return commandOutcome{}, errs.ErrForbidden
	}
	next := "SUCCEEDED"
	if payload.UnknownOutcome {
		next = "UNKNOWN_OUTCOME"
	} else if !payload.Success {
		next = "FAILED"
	}
	var receiptID any
	if payload.Success {
		generatedReceiptRef, _ := newRef("erc")
		var storedReceiptRef string
		var storedReceiptID string
		if err := tx.QueryRow(ctx, queryWorkersCompleteintegrationinvocationInsertEffectReceipt,
			generatedReceiptRef, scope.organizationID, invocationID, payload.EffectKey, payload.InputDigest,
			truncate(payload.ProviderEffectRef, 256), payload.ResponseDigest, payload.ResultSummary,
		).Scan(&storedReceiptID, &storedReceiptRef); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return commandOutcome{}, errs.ErrConflict
			}
			return commandOutcome{}, errs.ErrUnavailable
		}
		receiptID = storedReceiptID
	}
	if _, err := tx.Exec(ctx, queryWorkersCompleteintegrationinvocationUpdateIntegrationInvocationsStateResultSummarySafeErrorCode,
		invocationID, next, payload.ResultSummary, truncate(payload.SafeErrorCode, 100), receiptID,
	); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if next == "SUCCEEDED" || next == "FAILED" {
		if _, err := repository.scheduleIntegrationContinuation(ctx, tx, scope, invocationID, projectID); err != nil {
			return commandOutcome{}, err
		}
	}
	event, err := repository.emitIntegrationActionEvent(ctx, tx, scope, projectID, rootRunID, invocationRef, nodeRef, next)
	if err != nil {
		return commandOutcome{}, err
	}
	runRef, err := mustRunRef(ctx, tx, runID)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, runRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: projectID, projectRef: projectRef, resourceKind: "INTEGRATION_INVOCATION", resourceRef: payload.InvocationRef, summary: "i18n:INTEGRATION_INVOCATION_COMPLETED"}, nil
}

// integrationActionOutcomeMessage maps successful and failed integration states to their
// localization keys, using the unknown-outcome key for every other state. It formats presentation
// metadata without changing execution state.
// integrationActionOutcomeMessage сопоставляет успешное и неуспешное integration state ключам
// локализации, используя unknown-outcome для остальных. Формирует метаданные представления без
// изменения execution state.
func integrationActionOutcomeMessage(state string) string {
	switch state {
	case "SUCCEEDED":
		return "i18n:INTEGRATION_ACTION_SUCCEEDED"
	case "FAILED":
		return "i18n:INTEGRATION_ACTION_FAILED"
	default:
		return "i18n:INTEGRATION_ACTION_OUTCOME_UNKNOWN"
	}
}

// integrationGateContextSummary joins trimmed connection name and capability name, falling back to
// operation when capability name is blank, then truncates the presentation summary to1000. The
// summary is descriptive and never a source of scope or approval authority.
// integrationGateContextSummary объединяет очищенные имя connection и capability, подставляет
// operation при пустом capability name, ограничивает summary1000. Текст служит описанию, не
// источником scope и approval authority.
func integrationGateContextSummary(connectionName, capabilityName, operation string) string {
	name := strings.TrimSpace(capabilityName)
	if name == "" {
		name = strings.TrimSpace(operation)
	}
	return truncate(strings.TrimSpace(connectionName)+" · "+name, 1000)
}

// safeIntegrationErrorCode allows only the closed integration failure-code set plus the email
// health-not-ready code. Credential projection pending is intentionally handled by its separate
// bounded test path rather than becoming a general invocation error.
// safeIntegrationErrorCode допускает лишь закрытый набор integration failure codes и email
// health-not-ready. Credential projection pending намеренно обрабатывается отдельным ограниченным
// test path, не становится общей ошибкой invocation.
func safeIntegrationErrorCode(code string) bool {
	switch code {
	case emailbridgeapi.HealthNotReadyCode, "INTEGRATION_AUTH_REJECTED", "INTEGRATION_CREDENTIAL_UNAVAILABLE", "INTEGRATION_UNAVAILABLE", "INTEGRATION_RATE_LIMITED", "INTEGRATION_CONFIGURATION_INVALID", "INTEGRATION_CAPABILITY_UNSUPPORTED", "INTEGRATION_ROUTE_NOT_OWNED", "INTEGRATION_REQUEST_REJECTED", "INTEGRATION_RESPONSE_INVALID", "INTEGRATION_OUTCOME_UNKNOWN":
		return true
	default:
		return false
	}
}
