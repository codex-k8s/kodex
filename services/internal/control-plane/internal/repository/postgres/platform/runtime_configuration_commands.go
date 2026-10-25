package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// lockedRuntimeAgent holds the organization-locked agent identity plus configuration/overlay/environment-binding versions needed for exact runtime changes; it is not itself a permission grant.
// lockedRuntimeAgent хранит заблокированную в организации identity агента и версии конфигурации/overlay/привязки окружения для точных изменений runtime; само по себе полномочий не предоставляет.
type lockedRuntimeAgent struct {
	id, projectID, projectRef, systemKey, overlayID, runtimeProfileRef string
	agentVersion, configVersion                                        int64
	overlayVersion, bindingVersion                                     int64
}

// bootstrapAgentRuntime binds a newly created role to the configured platform image, canonical default environment policy, provider catalog candidates and initial config/overlay in the caller transaction. It preserves an existing published shared environment revision and activates only a newly initialized version; gpt-6.1-sol starts with the approved medium overlay.
// bootstrapAgentRuntime привязывает новую роль к настроенному платформенному образу, канонической policy окружения по умолчанию, кандидатам каталога провайдера и исходным config/overlay в транзакции вызывающего. Сохраняет существующую опубликованную ревизию общего окружения и активирует лишь новую; gpt-6.1-sol получает утверждённый medium overlay.
func (repository *Repository) bootstrapAgentRuntime(ctx context.Context, tx pgx.Tx, organizationID, agentID, projectID string, runtime entity.RuntimeSelection, createdBy string) error {
	imageArtifactID, image, selectedTools, err := repository.ensureBootstrapRuntimeEnvironmentImage(
		ctx, tx, organizationID, agentID, projectID, createdBy)
	if err != nil {
		return err
	}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	coreDigest, environmentDigest, err := runtimeEnvironmentConfigurationDigests(nil, nil, image, nil, policy)
	if err != nil {
		return errors.New("compute bootstrap runtime environment digest")
	}
	policyRef, _ := newRef("ppol")
	configRef, _ := newRef("rconf")
	overlayRef, _ := newRef("cov")
	environmentRef, _ := newRef("renv")
	environmentVersionRef, _ := newRef("renvv")
	bindingRef, _ := newRef("aenv")
	var updatedAgentID, runtimeEnvironmentID, runtimeEnvironmentVersionID, currentRuntimeEnvironmentVersionID string
	candidates, err := captureRuntimeCatalogPins(ctx, tx, scope{organizationID: organizationID}, runtime.Provider, runtime.Model, nil)
	if errors.Is(err, errs.ErrConflict) {
		candidates, err = bootstrapUnpinnedCatalogCandidates(ctx, tx, organizationID, runtime.Provider)
	}
	if err != nil {
		return err
	}
	rawCandidates, _ := json.Marshal(candidates)
	mode := "LEAST_USED"
	if len(candidates) == 1 {
		mode = "FIXED"
	}
	overlay := ""
	if runtime.Provider == "openai-codex" && runtime.Model == "gpt-6.1-sol" {
		overlay = "model_reasoning_effort = \"medium\"\n"
	}
	overlay, overlayDigest, err := runtimecontract.CanonicalConfigOverlay(overlay)
	if err != nil {
		return errors.New("compute bootstrap runtime overlay digest")
	}
	err = tx.QueryRow(ctx, queryRuntimeConfigurationBootstrapAgent, pgx.StrictNamedArgs{
		"account_candidates": rawCandidates, "policy_mode": mode, "policy_digest": digestBytes([]byte(mode), rawCandidates),
		"organization_id": organizationID, "agent_id": agentID, "project_id": projectID,
		"policy_ref": policyRef, "config_ref": configRef, "overlay_ref": overlayRef,
		"environment_ref": environmentRef, "environment_version_ref": environmentVersionRef,
		"binding_ref": bindingRef, "runtime_profile_ref": runtime.Ref, "provider": runtime.Provider,
		"model": runtime.Model, "created_by": createdBy,
		"overlay_content": overlay, "overlay_digest": overlayDigest,
		"environment_image_artifact_id": imageArtifactID, "environment_selected_tools": selectedTools,
		"environment_core_digest": coreDigest, "environment_digest": environmentDigest,
		"environment_resource_policy": asJSON(policy.Resources), "environment_volume_policy": asJSON(policy.Volumes),
		"environment_network_policy": asJSON(policy.Network), "environment_kubernetes_access_profile": asJSON(policy.KubernetesAccess),
		"environment_resources_digest": policy.ResourcesDigest, "environment_volumes_digest": policy.VolumesDigest,
		"environment_network_digest": policy.NetworkDigest, "environment_rbac_digest": policy.RBACDigest,
	}).Scan(&updatedAgentID, &runtimeEnvironmentID, &runtimeEnvironmentVersionID, &currentRuntimeEnvironmentVersionID)
	if err != nil {
		return fmt.Errorf("bootstrap agent runtime configuration: %w", err)
	}
	if updatedAgentID != agentID {
		return errors.New("bootstrap agent runtime configuration did not update the agent")
	}
	// CREATE привязывает новую роль к опубликованной ревизии, но не является
	// публикацией новой конфигурации существующего общего environment.
	if currentRuntimeEnvironmentVersionID != "" {
		if runtimeEnvironmentVersionID != currentRuntimeEnvironmentVersionID {
			return errors.New("bootstrap agent runtime changed a published environment revision")
		}
		return nil
	}
	activation, err := tx.Exec(ctx, queryRuntimeConfigurationAdvanceBootstrapEnvironment, pgx.StrictNamedArgs{
		"environment_id": runtimeEnvironmentID, "current_version_id": currentRuntimeEnvironmentVersionID,
		"next_version_id": runtimeEnvironmentVersionID,
	})
	if err != nil || activation.RowsAffected() != 1 {
		return errors.New("activate bootstrap runtime environment version")
	}
	return nil
}

// ensureBootstrapRuntimeEnvironmentImage reuses the exact configured default image metadata when present, otherwise materializes its platform-owned recipe/artifact metadata and activates the resulting exact generation. It derives immutable provenance inputs from configured digests, performs no external build here and returns an explicit empty selected-tools list.
// ensureBootstrapRuntimeEnvironmentImage повторно использует метаданные точного настроенного образа по умолчанию, иначе материализует его платформенные recipe/artifact метаданные и активирует полученное точное поколение. Выводит неизменяемые provenance inputs из настроенных digest, не выполняет внешнюю сборку и возвращает явно пустой список выбранных инструментов.
func (repository *Repository) ensureBootstrapRuntimeEnvironmentImage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, agentID, projectID, createdBy string,
) (string, entity.RuntimeEnvironmentImage, []byte, error) {
	emptyTools, _ := json.Marshal([]entity.RuntimeEnvironmentTool{})
	image, err := scanBootstrapRuntimeEnvironmentImage(tx.QueryRow(ctx,
		queryRuntimeConfigurationResolveBootstrapImage, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID,
			"default_image_reference": repository.roleImages.DefaultImageReference,
			"default_image_digest":    repository.roleImages.DefaultImageDigest,
		}))
	if err == nil {
		return image.id, image.image, emptyTools, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	separator := strings.LastIndex(repository.roleImages.DefaultImageReference, "@")
	if separator < 1 {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	contentSHA256 := strings.TrimPrefix(repository.roleImages.DefaultImageDigest, "sha256:")
	specification := entity.RoleImageRecipeInput{
		BaseImageReference: repository.roleImages.DefaultImageReference[:separator],
		BaseImageDigest:    repository.roleImages.DefaultImageDigest,
		SourceRef:          platformOwnedRoleImageSource,
		SourceRevision:     repository.roleImages.DefaultImageDigest,
		SourceSHA256:       contentSHA256,
		ContextRef:         repository.roleImages.DefaultImageReference,
		ContextSHA256:      contentSHA256,
		BuilderSHA256:      repository.roleImages.RoleRuntimeContractSHA256,
		FrontendSHA256:     repository.roleImages.RoleRuntimeContractSHA256,
		ToolchainSHA256:    repository.roleImages.RoleRuntimeContractSHA256,
		EnvironmentKey:     "system-base",
		Platforms:          []entity.RoleImagePlatform{{OS: "linux", Architecture: "amd64"}},
		Dockerfile:         "FROM " + repository.roleImages.DefaultImageReference + "\n",
	}
	specificationJSON := asJSON(specification)
	specificationSHA256 := roleImageDigest(specification)
	immutableBuildSHA256 := digestBytes(specificationJSON, []byte(repository.roleImages.RoleRuntimeContractSHA256))
	provenanceSHA256 := digestBytes([]byte("platform-owned-bootstrap"), []byte(repository.roleImages.DefaultImageReference))
	evidenceSHA256 := digestBytes([]byte("platform-owned-bootstrap-evidence"), []byte(repository.roleImages.DefaultImageDigest))
	recipeRef, refErr := newRef("imgrec")
	if refErr != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	buildRef, refErr := newRef("imgbld")
	if refErr != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	artifactRef, refErr := newRef("imgart")
	if refErr != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	image, err = scanBootstrapRuntimeEnvironmentImage(tx.QueryRow(ctx,
		queryRuntimeConfigurationMaterializeSystemImage, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID, "agent_id": agentID,
			"created_by": createdBy, "recipe_ref": recipeRef, "build_ref": buildRef,
			"artifact_ref": artifactRef, "specification": specificationJSON,
			"spec_sha256": specificationSHA256, "policy_revision": repository.roleImages.PolicyRevision,
			"policy_sha256":          repository.roleImages.PolicySHA256,
			"contract_revision":      repository.roleImages.RoleRuntimeContractRevision,
			"contract_sha256":        repository.roleImages.RoleRuntimeContractSHA256,
			"immutable_build_sha256": immutableBuildSHA256, "provenance_sha256": provenanceSHA256,
			"evidence_sha256": evidenceSHA256, "image_reference": repository.roleImages.DefaultImageReference,
			"manifest_digest": repository.roleImages.DefaultImageDigest,
		}))
	if err != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	var activatedRecipeID string
	err = tx.QueryRow(ctx, queryRuntimeConfigurationActivateSystemImage, pgx.StrictNamedArgs{
		"organization_id":   organizationID,
		"project_id":        projectID,
		"recipe_ref":        image.image.RecipeRef,
		"artifact_id":       image.id,
		"artifact_ref":      image.image.ArtifactRef,
		"recipe_generation": image.image.RecipeGeneration,
		"image_reference":   image.image.Reference,
		"manifest_digest":   image.image.Digest,
	}).Scan(&activatedRecipeID)
	if err != nil || activatedRecipeID == "" {
		return "", entity.RuntimeEnvironmentImage{}, nil, errs.ErrUnavailable
	}
	return image.id, image.image, emptyTools, nil
}

// bootstrapRuntimeEnvironmentImage pairs the database artifact ID with its immutable public image selection for bootstrap resolution and activation checks.
// bootstrapRuntimeEnvironmentImage связывает ID артефакта БД с неизменяемым публичным выбором образа для разрешения bootstrap и проверки активации.
type bootstrapRuntimeEnvironmentImage struct {
	id    string
	image entity.RuntimeEnvironmentImage
}

// scanBootstrapRuntimeEnvironmentImage reads the fixed artifact ID/ref, recipe ref/generation and reference/digest projection, returning the original scanner error for caller-specific absence handling.
// scanBootstrapRuntimeEnvironmentImage читает фиксированную проекцию artifact ID/ref, recipe ref/generation и reference/digest, возвращая исходную ошибку scanner для отдельной обработки отсутствия вызывающим.
func scanBootstrapRuntimeEnvironmentImage(row interface{ Scan(...any) error }) (bootstrapRuntimeEnvironmentImage, error) {
	var result bootstrapRuntimeEnvironmentImage
	err := row.Scan(&result.id, &result.image.ArtifactRef, &result.image.RecipeRef,
		&result.image.RecipeGeneration, &result.image.Reference, &result.image.Digest)
	return result, err
}

// selectProviderAccountForAgent locks the agent, validates stored account/config/policy pins and prefers a candidate whose fresh catalog marks the requested model eligible. If none qualifies it retains the first authorized selection for reauthorization; subsequent RuntimeRevision construction must still reject an unverified model snapshot.
// selectProviderAccountForAgent блокирует агента, проверяет сохранённые account/config/policy pins и предпочитает кандидата, чей свежий каталог допускает запрошенную модель. Если подходящего нет, сохраняет первый разрешённый выбор для повторной авторизации; последующая сборка RuntimeRevision всё равно должна отклонить непроверенный snapshot модели.
func (repository *Repository) selectProviderAccountForAgent(ctx context.Context, tx pgx.Tx, organizationID, agentRef string) (string, error) {
	var lockedAgentID string
	if err := tx.QueryRow(ctx, queryRuntimeCatalogLockAgent, organizationID, agentRef).Scan(&lockedAgentID); err != nil {
		return "", errs.ErrConflict
	}
	rows, err := tx.Query(ctx, queryRuntimeConfigurationSelectProviderAccount, organizationID, agentRef)
	if err != nil {
		return "", errs.ErrUnavailable
	}
	// selection holds one account candidate with its exact config/policy version/digest and provider/model for ordered catalog eligibility checks after SQL rows are closed.
	// selection хранит кандидата аккаунта с точными version/digest config/policy и provider/model для последовательной проверки каталога после закрытия SQL rows.
	type selection struct {
		accountID, accountRef, configRef, configDigest, policyRef, policyDigest, provider, model string
		configVersion, policyVersion                                                             int64
	}
	selections := []selection{}
	for rows.Next() {
		var item selection
		if rows.Scan(&item.accountID, &item.accountRef, &item.configRef, &item.configVersion, &item.configDigest,
			&item.policyRef, &item.policyVersion, &item.policyDigest, &item.provider, &item.model) != nil ||
			item.accountRef == "" || item.configRef == "" || item.configVersion < 1 || len(item.configDigest) != 64 ||
			item.policyRef == "" || item.policyVersion < 1 || len(item.policyDigest) != 64 {
			rows.Close()
			return "", errs.ErrUnavailable
		}
		selections = append(selections, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return "", errs.ErrUnavailable
	}
	for _, item := range selections {
		catalog, catalogErr := readModelCatalogTx(ctx, tx, scope{organizationID: organizationID}, item.provider, item.accountRef)
		if catalogErr != nil {
			return "", catalogErr
		}
		for _, capability := range catalog.Models {
			if capability.ID == item.model && capability.Available && slices.Contains(capability.EligibleProviderAccountRefs, item.accountRef) {
				return item.accountID, nil
			}
		}
	}
	// Сохраняем прежний выбор authorized account, когда свежего доступного
	// каталога нет ни у одного кандидата. Последующая сборка RuntimeRevision
	// всё равно закрыто отклонит непроверенный model snapshot; это позволяет
	// reauthorization завершиться до следующего catalog probe и не даёт
	// нездоровому первому кандидату вытеснить доступный следующий.
	if len(selections) > 0 {
		return selections[0].accountID, nil
	}
	return "", errs.ErrConflict
}

// changeRuntimeConfiguration dispatches only the explicitly registered runtime config, overlay, environment lifecycle and binding commands; unsupported kinds fail instead of reaching a generic resource mutation.
// changeRuntimeConfiguration направляет лишь явно зарегистрированные команды runtime config, overlay, жизненного цикла окружения и привязки; неподдержанный вид отклоняется без универсального изменения ресурса.
func (repository *Repository) changeRuntimeConfiguration(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.PublishAgentRuntimeConfig:
		return repository.publishAgentRuntimeConfiguration(ctx, tx, scope, input)
	case command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft,
		command.PublishConfigOverlayDraft, command.RollbackConfigOverlay:
		return repository.changeConfigOverlay(ctx, tx, scope, input)
	case command.CreateRuntimeEnvironment, command.PublishRuntimeEnvironment, command.RollbackRuntimeEnvironment:
		return repository.changeRuntimeEnvironment(ctx, tx, scope, input)
	case command.SetRuntimeEnvironmentEnabled, command.DeleteRuntimeEnvironment:
		return repository.changeRuntimeEnvironmentLifecycle(ctx, tx, scope, input)
	case command.BindAgentRuntimeEnvironment:
		return repository.bindRuntimeEnvironment(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

// changeRuntimeEnvironmentLifecycle locks organization-scoped environment metadata under the already authorized command, enforces expected version and explicit enable/disable/delete rules, and refuses deleting an enabled or bound environment. It returns the changed state/event projection for the enclosing owner transaction rather than committing itself.
// changeRuntimeEnvironmentLifecycle блокирует метаданные окружения организации внутри уже авторизованной команды, проверяет ожидаемую версию и явные правила enable/disable/delete, запрещая удаление включённого либо связанного окружения. Возвращает state/event проекцию внешней транзакции владельца без собственного commit.
func (repository *Repository) changeRuntimeEnvironmentLifecycle(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	input command.Command,
) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentLifecycleInput)
	if !ok || payload.EnvironmentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var environmentID, projectID, projectRef, state string
	var version int64
	if err := tx.QueryRow(ctx, queryRuntimeConfigurationLockEnvironmentLifecycle, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "environment_ref": payload.EnvironmentRef,
	}).Scan(&environmentID, &projectID, &projectRef, &state, &version); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if version != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	item, err := repository.scanRuntimeEnvironment(tx.QueryRow(ctx, queryRuntimeConfigurationGetEnvironmentLifecycleSnapshot,
		pgx.StrictNamedArgs{"organization_id": current.organizationID, "environment_ref": payload.EnvironmentRef}))
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	targetState := "DISABLED"
	summary := "i18n:RUNTIME_ENVIRONMENT_DISABLED"
	if input.Kind == command.SetRuntimeEnvironmentEnabled {
		if payload.Enabled {
			targetState, summary = "ACTIVE", "i18n:RUNTIME_ENVIRONMENT_ENABLED"
		}
		if state == targetState {
			return commandOutcome{}, errs.ErrConflict
		}
	} else {
		if state != "DISABLED" {
			return commandOutcome{}, errs.ErrConflict
		}
		var bindings int64
		if err := tx.QueryRow(ctx, queryRuntimeConfigurationCountEnvironmentBindings, pgx.StrictNamedArgs{
			"environment_id": environmentID,
		}).Scan(&bindings); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if bindings != 0 {
			return commandOutcome{}, errs.ErrConflict
		}
		targetState, summary = "DELETED", "i18n:RUNTIME_ENVIRONMENT_DELETED"
	}
	if err := tx.QueryRow(ctx, queryRuntimeConfigurationUpdateEnvironmentLifecycle, pgx.StrictNamedArgs{
		"environment_id": environmentID, "state": targetState, "expected_version": version,
	}).Scan(&item.State, &item.Version, &item.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	} else if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	item.NextActions = nil
	return commandOutcome{
		result: command.Result{RuntimeEnvironment: &item}, projectID: projectID, projectRef: projectRef,
		resourceKind: "RUNTIME_ENVIRONMENT", resourceRef: payload.EnvironmentRef, summary: summary,
		platformEvent: "RUNTIME_ENVIRONMENT_CHANGED", platformAggregateVersion: item.Version, platformState: item.State,
	}, nil
}

// lockRuntimeAgent locks one agent under the resolved organization and scans exact agent/config/overlay/binding counters, distinguishing missing metadata from infrastructure failure. Authorization belongs to the enclosing command path.
// lockRuntimeAgent блокирует агента разрешённой организации и читает точные счётчики agent/config/overlay/binding, различая отсутствие метаданных и инфраструктурный отказ. Авторизацией владеет внешний путь команды.
func (repository *Repository) lockRuntimeAgent(ctx context.Context, tx pgx.Tx, scope scope, ref string) (lockedRuntimeAgent, error) {
	var agent lockedRuntimeAgent
	err := tx.QueryRow(ctx, queryRuntimeConfigurationLockAgent, scope.organizationID, ref).Scan(
		&agent.id, &agent.projectID, &agent.projectRef, &agent.systemKey, &agent.agentVersion, &agent.configVersion,
		&agent.overlayID, &agent.overlayVersion, &agent.bindingVersion, &agent.runtimeProfileRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeAgent{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeAgent{}, errs.ErrUnavailable
	}
	return agent, nil
}

// validRuntimeConfigurationAgent accepts project agents with no protected system key or the projectless system-assistant key; it checks the editable target shape without proving caller access.
// validRuntimeConfigurationAgent принимает проектного агента без защищённого системного ключа либо системного помощника без проекта; проверяет форму изменяемой цели, а не доступ вызывающего.
func validRuntimeConfigurationAgent(agent lockedRuntimeAgent) bool {
	return agent.projectID != "" && agent.systemKey == "" ||
		agent.projectID == "" && agent.systemKey == "system-assistant"
}

// publishAgentRuntimeConfiguration uses the specialized publication path with the currently published overlay; prepared replacement overlay is reserved for the atomic assistant publication caller.
// publishAgentRuntimeConfiguration использует специализированную публикацию с текущим опубликованным overlay; подготовленная замена overlay предусмотрена лишь для атомарной публикации помощника.
func (repository *Repository) publishAgentRuntimeConfiguration(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	return repository.publishAgentRuntimeConfigurationWithOverlay(ctx, tx, scope, input, nil)
}

// publishAgentRuntimeConfigurationWithOverlay validates the editable agent and expected version, normalizes account order, verifies exact runtime/catalog eligibility and persists a new provider policy/config revision. Only its specialized caller can supply a prepared overlay; ordinary publication reads the current published overlay and returns owner event metadata without committing.
// publishAgentRuntimeConfigurationWithOverlay проверяет изменяемого агента и ожидаемую версию, упорядочивает аккаунты, сверяет точную доступность runtime/каталога и сохраняет новую policy/config ревизию. Подготовленный overlay передаёт лишь специализированный вызывающий; обычная публикация читает текущий overlay и возвращает метаданные события владельца без commit.
func (repository *Repository) publishAgentRuntimeConfigurationWithOverlay(ctx context.Context, tx pgx.Tx, scope scope, input command.Command, preparedOverlay *string) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentRuntimeConfigurationInput)
	if !ok || payload.AgentRef == "" || payload.RuntimeProfileRef == "" || input.Mutation.ExpectedVersion == nil ||
		!validModel(payload.Model) || !validProviderPolicy(payload.ProviderPolicyMode, payload.ProviderAccounts) {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if !validRuntimeConfigurationAgent(agent) {
		return commandOutcome{}, errs.ErrProtected
	}
	if *input.Mutation.ExpectedVersion != agent.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	accounts := append([]entity.ProviderAccountCandidate(nil), payload.ProviderAccounts...)
	sort.Slice(accounts, func(left, right int) bool { return accounts[left].AccountRef < accounts[right].AccountRef })
	accountRefs := make([]string, 0, len(accounts))
	for _, account := range accounts {
		accountRefs = append(accountRefs, account.AccountRef)
	}
	var provider, defaultModel, runtimeRevision string
	var eligible int32
	err = tx.QueryRow(ctx, queryRuntimeConfigurationValidateAccounts, scope.organizationID, payload.RuntimeProfileRef, accountRefs).
		Scan(&provider, &defaultModel, &runtimeRevision, &eligible)
	if errors.Is(err, pgx.ErrNoRows) || eligible != int32(len(accounts)) {
		return commandOutcome{}, errs.ErrConflict
	}
	if err != nil || defaultModel == "" || runtimeRevision == "" {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, overlay, err := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, payload.AgentRef, "")
	if err != nil {
		return commandOutcome{}, err
	}
	if preparedOverlay != nil {
		overlay = *preparedOverlay
	}
	accounts, _, err = validateRuntimeCatalogCandidates(ctx, tx, scope, provider, payload.Model, overlay, accounts, true)
	if err != nil {
		return commandOutcome{}, err
	}
	policyRef, _ := newRef("ppol")
	configRef, _ := newRef("rconf")
	rawAccounts, _ := json.Marshal(accounts)
	policyDigest := digestBytes([]byte(payload.ProviderPolicyMode), rawAccounts)
	version := agent.configVersion + 1
	configDigest := digestBytes([]byte(payload.RuntimeProfileRef), []byte(provider), []byte(payload.Model),
		[]byte(policyRef), []byte(strconvFormat(version)), []byte(policyDigest))
	var publishedRef string
	err = tx.QueryRow(ctx, queryRuntimeConfigurationPublish, pgx.StrictNamedArgs{
		"policy_ref": policyRef, "organization_id": scope.organizationID, "agent_id": agent.id,
		"version_number": version, "policy_mode": payload.ProviderPolicyMode,
		"account_candidates": rawAccounts, "policy_digest": policyDigest, "created_by": scope.actorID,
		"config_ref": configRef, "runtime_profile_ref": payload.RuntimeProfileRef, "provider": provider,
		"model": payload.Model, "config_digest": configDigest,
	}).Scan(&publishedRef)
	if err != nil || publishedRef != configRef {
		return commandOutcome{}, mapWriteError(err)
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return runtimeConfigurationOutcome(view, agent, "i18n:AGENT_RUNTIME_CONFIGURATION_PUBLISHED"), nil
}

// changeConfigOverlay handles create/validate/publish/rollback through explicit draft/published transitions under the locked agent version. Publication rechecks canonical content, catalog candidates and schema pins; rollback creates a new publication from the selected immutable source, while diagnostics and agent version update stay in the owner transaction.
// changeConfigOverlay обрабатывает create/validate/publish/rollback явными переходами draft/published под заблокированной версией агента. Публикация повторно проверяет каноническое содержимое, кандидатов каталога и schema pins; rollback создаёт новую публикацию из выбранного неизменяемого источника, диагностика и версия агента остаются в транзакции владельца.
func (repository *Repository) changeConfigOverlay(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ConfigOverlayInput)
	if !ok || payload.AgentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if !validRuntimeConfigurationAgent(agent) {
		return commandOutcome{}, errs.ErrProtected
	}
	if *input.Mutation.ExpectedVersion != agent.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	var publicationSchema runtimecontract.ConfigOverlaySchema
	if input.Kind == command.PublishConfigOverlayDraft || input.Kind == command.RollbackConfigOverlay {
		configuration, _, readErr := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, payload.AgentRef, "")
		if readErr != nil {
			return commandOutcome{}, readErr
		}
		publicationSchema, err = runtimeOverlaySchema(ctx, tx, scope, configuration)
		if err != nil {
			return commandOutcome{}, err
		}
	}
	switch input.Kind {
	case command.CreateConfigOverlayDraft:
		if runtimecontract.ValidateConfigOverlayDraftPayload(payload.Content) != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryRuntimeConfigurationSupersedeMutableOverlays, agent.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref, _ := newRef("cov")
		digest := sha256.Sum256([]byte(payload.Content))
		var created string
		if err := tx.QueryRow(ctx, queryRuntimeConfigurationCreateOverlayDraft, pgx.StrictNamedArgs{
			"agent_id": agent.id, "organization_id": scope.organizationID, "ref": ref,
			"parent_version_id": agent.overlayID, "content": payload.Content,
			"digest": hex.EncodeToString(digest[:]), "created_by": scope.actorID,
		}).Scan(&created); err != nil || created != ref {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.ValidateConfigOverlayDraft:
		draftID, _, _, _, content, err := lockOverlayDraft(ctx, tx, scope.organizationID, payload.AgentRef)
		if err != nil {
			return commandOutcome{}, err
		}
		state := "VALID"
		problems := []string{}
		configuration, _, err := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, payload.AgentRef, "")
		if err != nil {
			return commandOutcome{}, err
		}
		_, efforts, err := validateRuntimeCatalogCandidates(ctx, tx, scope, configuration.Provider, configuration.Model, "", configuration.ProviderPolicy.AccountCandidates, false)
		if err != nil {
			return commandOutcome{}, err
		}
		if len(runtimecontract.DiagnoseConfigOverlay(content, efforts)) != 0 {
			state = "INVALID"
			problems = []string{"i18n:CONFIG_OVERLAY_INVALID_OR_PROTECTED"}
		}
		if _, err := tx.Exec(ctx, queryRuntimeConfigurationValidateOverlay, draftID, state, asJSON(problems)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	case command.PublishConfigOverlayDraft:
		draftID, _, _, state, content, err := lockOverlayDraft(ctx, tx, scope.organizationID, payload.AgentRef)
		if err != nil {
			return commandOutcome{}, err
		}
		if state != "VALID" {
			return commandOutcome{}, errs.ErrConflict
		}
		canonical, digest, err := runtimecontract.CanonicalConfigOverlay(content)
		if err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		configuration, _, err := readRuntimeCatalogConfiguration(ctx, tx, scope.organizationID, payload.AgentRef, "")
		if err != nil {
			return commandOutcome{}, err
		}
		if _, _, err := validateRuntimeCatalogCandidates(ctx, tx, scope, configuration.Provider, configuration.Model, content, configuration.ProviderPolicy.AccountCandidates, false); err != nil {
			return commandOutcome{}, err
		}
		if _, err := tx.Exec(ctx, queryRuntimeConfigurationSupersedePublishedOverlay, agent.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		draftSuperseded, err := tx.Exec(ctx, queryRuntimeConfigurationSupersedeOverlayDraft, agent.id, draftID)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if draftSuperseded.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
		ref, _ := newRef("cov")
		var published string
		if err := tx.QueryRow(ctx, queryRuntimeConfigurationPublishOverlay, pgx.StrictNamedArgs{
			"agent_id": agent.id, "organization_id": scope.organizationID, "draft_id": draftID,
			"ref": ref, "content": canonical, "digest": digest, "created_by": scope.actorID,
			"schema_revision": publicationSchema.Revision, "schema_digest": publicationSchema.Digest,
		}).Scan(&published); err != nil || published != ref {
			return commandOutcome{}, mapWriteError(err)
		}
	case command.RollbackConfigOverlay:
		if payload.PublishedOverlayRef == "" {
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryRuntimeConfigurationSupersedePublishedOverlay, agent.id); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		ref, _ := newRef("cov")
		var published string
		if err := tx.QueryRow(ctx, queryRuntimeConfigurationRollbackOverlay, pgx.StrictNamedArgs{
			"agent_id": agent.id, "organization_id": scope.organizationID, "source_ref": payload.PublishedOverlayRef,
			"ref": ref, "created_by": scope.actorID,
			"schema_revision": publicationSchema.Revision, "schema_digest": publicationSchema.Digest,
		}).Scan(&published); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil || published != ref {
			return commandOutcome{}, mapWriteError(err)
		}
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt, agent.id); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if input.Kind == command.RollbackConfigOverlay {
		if _, _, err := validateRuntimeCatalogCandidates(ctx, tx, scope, view.Configuration.Provider, view.Configuration.Model, view.PublishedOverlay.Content, view.Configuration.ProviderPolicy.AccountCandidates, false); err != nil {
			return commandOutcome{}, err
		}
	}
	if err := saveRuntimeOverlayDiagnostics(ctx, tx, scope, &view, input.Kind == command.CreateConfigOverlayDraft || input.Kind == command.ValidateConfigOverlayDraft); err != nil {
		return commandOutcome{}, err
	}
	return runtimeConfigurationOutcome(view, agent, "i18n:AGENT_CONFIG_OVERLAY_CHANGED"), nil
}

// preparedRuntimeEnvironmentContents keeps the validated immutable content required by create/publish SQL: non-secret values, secret descriptors, admitted image/tool selection, canonical policy and their digests. It contains no plaintext secret material or independently granted owner access.
// preparedRuntimeEnvironmentContents хранит проверенное неизменяемое содержимое для SQL создания/публикации: несекретные значения, дескрипторы секретов, допущенные образ/инструменты, каноническую policy и digest. Открытого содержимого секретов и самостоятельно назначенных полномочий владельца не содержит.
type preparedRuntimeEnvironmentContents struct {
	values, secrets, tools      []byte
	imageID, coreDigest, digest string
	policy                      runtimecontract.RuntimeEnvironmentPolicy
}

// prepareRuntimeEnvironmentContents runs policy admission, scoped secret resolution, promoted-image/tool validation and digest construction in the same order for environment create and publish. The caller supplies owner-resolved scope/project identity and retains metadata validation, version locks, SQL effects, audit/event and commit; rollback uses its separate immutable-source path.
// prepareRuntimeEnvironmentContents выполняет допуск policy, разрешение scoped-секретов, проверку опубликованного образа/инструментов и построение digest в прежнем порядке для создания и публикации окружения. Вызывающий передаёт разрешённые владельцем scope/project identity и сохраняет проверку метаданных, version locks, SQL-действия, audit/event и commit; rollback использует отдельный путь неизменяемого источника.
func (repository *Repository) prepareRuntimeEnvironmentContents(ctx context.Context, tx pgx.Tx, current scope, projectID, projectRef, scopeKind, environmentRef string, input command.RuntimeEnvironmentInput) (preparedRuntimeEnvironmentContents, error) {
	policy, err := repository.admitRuntimeEnvironmentPolicy(ctx, tx, current, projectRef, environmentRef, input.Policy)
	if err != nil {
		return preparedRuntimeEnvironmentContents{}, err
	}
	values, secrets, contractValues, contractSecrets, err := repository.resolveScopedEnvironmentPayload(ctx, tx, current.organizationID, projectID, scopeKind, input.Values, input.SecretBindings)
	if err != nil {
		return preparedRuntimeEnvironmentContents{}, err
	}
	imageID, image, tools, encodedTools, err := repository.resolveScopedRuntimeEnvironmentImage(ctx, tx, current.organizationID, projectID, scopeKind, input.ImageArtifactRef, input.Tools)
	if err != nil {
		return preparedRuntimeEnvironmentContents{}, err
	}
	coreDigest, digest, err := runtimeEnvironmentConfigurationDigests(contractValues, contractSecrets, image, tools, policy)
	if err != nil {
		return preparedRuntimeEnvironmentContents{}, errs.ErrInvalid
	}
	return preparedRuntimeEnvironmentContents{values: values, secrets: secrets, tools: encodedTools, imageID: imageID, coreDigest: coreDigest, digest: digest, policy: policy}, nil
}

// changeRuntimeEnvironment keeps create, publish and immutable-source rollback as explicit owner transitions with separate metadata/scope/version checks. Create/publish share only ordered content preparation, then persist their own version/activation SQL and return the typed environment/event projection; this method never opens or commits a transaction itself.
// changeRuntimeEnvironment сохраняет create, publish и rollback из неизменяемого источника явными переходами владельца с раздельными проверками метаданных/области/версии. Create/publish делят лишь упорядоченную подготовку содержимого, затем сохраняют собственные version/activation SQL и возвращают типизированную environment/event проекцию; метод сам не открывает и не фиксирует транзакцию.
func (repository *Repository) changeRuntimeEnvironment(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateRuntimeEnvironment {
		scopeKind := "PROJECT"
		if payload.ScopeKind == "ORGANIZATION" {
			scopeKind = "ORGANIZATION"
			if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, scope, scopeKind, payload.ProjectRef); err != nil {
				return commandOutcome{}, err
			}
		}
		if scopeKind == "PROJECT" && payload.ProjectRef == "" || strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 120 || len(payload.Description) > 1000 {
			return commandOutcome{}, errs.ErrInvalid
		}
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if scopeKind == "PROJECT" && projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		prepared, prepareErr := repository.prepareRuntimeEnvironmentContents(ctx, tx, scope, projectID, payload.ProjectRef, scopeKind, "", payload)
		if prepareErr != nil {
			return commandOutcome{}, prepareErr
		}
		environmentRef, _ := newRef("renv")
		versionRef, _ := newRef("renvv")
		var environmentID, environmentVersionID, created string
		createErr := tx.QueryRow(ctx, queryRuntimeConfigurationCreateEnvironment, pgx.StrictNamedArgs{
			"environment_ref": environmentRef, "version_ref": versionRef, "organization_id": scope.organizationID,
			"scope_kind": scopeKind, "project_id": projectID, "name": strings.TrimSpace(payload.Name), "description": strings.TrimSpace(payload.Description),
			"created_by": scope.actorID, "non_secret_values": prepared.values, "secret_descriptors": prepared.secrets, "digest": prepared.digest,
			"image_artifact_id": prepared.imageID, "selected_tools": prepared.tools,
			"core_digest": prepared.coreDigest, "resource_policy": asJSON(prepared.policy.Resources), "volume_policy": asJSON(prepared.policy.Volumes),
			"network_policy": asJSON(prepared.policy.Network), "kubernetes_access_profile": asJSON(prepared.policy.KubernetesAccess),
			"resources_digest": prepared.policy.ResourcesDigest, "volumes_digest": prepared.policy.VolumesDigest,
			"network_digest": prepared.policy.NetworkDigest, "rbac_digest": prepared.policy.RBACDigest,
		}).Scan(&environmentID, &environmentVersionID, &created)
		if createErr != nil {
			return commandOutcome{}, fmt.Errorf("create runtime environment storage: %w", mapWriteError(createErr))
		}
		if created != environmentRef {
			return commandOutcome{}, errs.ErrUnavailable
		}
		activation, err := tx.Exec(ctx, queryRuntimeConfigurationActivateEnvironment, environmentID, environmentVersionID)
		if err != nil || activation.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrUnavailable
		}
		environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, scope, environmentRef)
		if err != nil {
			return commandOutcome{}, err
		}
		return commandOutcome{result: command.Result{RuntimeEnvironment: &environment}, projectID: projectID,
			projectRef: payload.ProjectRef, resourceKind: "RUNTIME_ENVIRONMENT", resourceRef: environmentRef,
			summary: "i18n:RUNTIME_ENVIRONMENT_CREATED", platformEvent: "AGENT_CHANGED"}, nil
	}
	if payload.Ref == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	var environmentID, projectID, projectRef, currentVersionID string
	var environmentVersion, currentRevision int64
	err := tx.QueryRow(ctx, queryRuntimeConfigurationLockEnvironment, scope.organizationID, payload.Ref).Scan(
		&environmentID, &projectID, &projectRef, &environmentVersion, &currentVersionID, &currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	existing, err := repository.getRuntimeEnvironmentTx(ctx, tx, scope, payload.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, scope, existing.ScopeKind, existing.ProjectRef); err != nil {
		return commandOutcome{}, err
	}
	if *input.Mutation.ExpectedVersion != environmentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	versionRef, _ := newRef("renvv")
	var changed string
	if input.Kind == command.PublishRuntimeEnvironment {
		if strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 120 || len(payload.Description) > 1000 {
			return commandOutcome{}, errs.ErrInvalid
		}
		prepared, prepareErr := repository.prepareRuntimeEnvironmentContents(ctx, tx, scope, projectID, projectRef, existing.ScopeKind, payload.Ref, payload)
		if prepareErr != nil {
			return commandOutcome{}, prepareErr
		}
		err = tx.QueryRow(ctx, queryRuntimeConfigurationPublishEnvironment, pgx.StrictNamedArgs{
			"version_ref": versionRef, "organization_id": scope.organizationID, "environment_id": environmentID,
			"version_number": currentRevision + 1, "parent_version_id": currentVersionID,
			"non_secret_values": prepared.values, "secret_descriptors": prepared.secrets, "digest": prepared.digest, "created_by": scope.actorID,
			"image_artifact_id": nullUUID(prepared.imageID), "selected_tools": prepared.tools,
			"core_digest": prepared.coreDigest, "resource_policy": asJSON(prepared.policy.Resources), "volume_policy": asJSON(prepared.policy.Volumes),
			"network_policy": asJSON(prepared.policy.Network), "kubernetes_access_profile": asJSON(prepared.policy.KubernetesAccess),
			"resources_digest": prepared.policy.ResourcesDigest, "volumes_digest": prepared.policy.VolumesDigest,
			"network_digest": prepared.policy.NetworkDigest, "rbac_digest": prepared.policy.RBACDigest,
			"name": strings.TrimSpace(payload.Name), "description": strings.TrimSpace(payload.Description),
		}).Scan(&changed)
	} else if input.Kind == command.RollbackRuntimeEnvironment && payload.PublishedVersionRef != "" {
		if err := requireRuntimeEnvironmentSecretVersion(ctx, tx, scope.organizationID, payload.Ref, "", payload.PublishedVersionRef); err != nil {
			return commandOutcome{}, err
		}
		err = tx.QueryRow(ctx, queryRuntimeConfigurationRollbackEnvironment, pgx.StrictNamedArgs{
			"version_ref": versionRef, "organization_id": scope.organizationID, "environment_id": environmentID,
			"version_number": currentRevision + 1, "source_ref": payload.PublishedVersionRef, "created_by": scope.actorID,
		}).Scan(&changed)
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil || changed != payload.Ref {
		return commandOutcome{}, mapWriteError(err)
	}
	environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, scope, payload.Ref)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{RuntimeEnvironment: &environment}, projectID: projectID,
		projectRef: projectRef, resourceKind: "RUNTIME_ENVIRONMENT", resourceRef: payload.Ref,
		summary: "i18n:RUNTIME_ENVIRONMENT_PUBLISHED", platformEvent: "AGENT_CHANGED"}, nil
}

// resolveRuntimeEnvironmentImage resolves the project-scoped image/tool selection through the shared scoped resolver without inferring organization scope from a caller field.
// resolveRuntimeEnvironmentImage разрешает проектный выбор образа/инструментов общим scoped-разрешителем без вывода области организации из поля вызывающего.
func (repository *Repository) resolveRuntimeEnvironmentImage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, projectID, artifactRef string,
	tools []entity.RuntimeEnvironmentTool,
) (string, entity.RuntimeEnvironmentImage, []entity.RuntimeEnvironmentTool, []byte, error) {
	return repository.resolveScopedRuntimeEnvironmentImage(ctx, tx, organizationID, projectID, "PROJECT", artifactRef, tools)
}

// resolveScopedRuntimeEnvironmentImage requires a consistent project/organization scope, exact promoted artifact/recipe generation and matching active image inventory. It copies and sorts selected tools, validates bounded UTF-8 metadata and rejects duplicates or commands absent from verified image inventory before returning canonical JSON and image pins.
// resolveScopedRuntimeEnvironmentImage требует согласованную область проекта/организации, точный опубликованный artifact/recipe generation и совпадающий активный inventory образа. Копирует и сортирует инструменты, проверяет ограничения UTF-8 метаданных и отклоняет дубли/команды вне проверенного inventory до возврата канонического JSON и image pins.
func (repository *Repository) resolveScopedRuntimeEnvironmentImage(ctx context.Context, tx pgx.Tx, organizationID, projectID, scopeKind, artifactRef string, tools []entity.RuntimeEnvironmentTool) (string, entity.RuntimeEnvironmentImage, []entity.RuntimeEnvironmentTool, []byte, error) {
	if scopeKind != "ORGANIZATION" && scopeKind != "PROJECT" || (scopeKind == "ORGANIZATION") != (projectID == "") {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
	}
	if !strings.HasPrefix(artifactRef, "imgart_") || len(artifactRef) > 96 || len(tools) > 128 {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
	}
	var artifactID, storedArtifactRef, recipeRef, reference, manifestDigest string
	var recipeGeneration int64
	err := tx.QueryRow(ctx, queryRuntimeConfigurationResolveImageArtifact, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "scope_kind": scopeKind, "artifact_ref": artifactRef,
	}).Scan(&artifactID, &storedArtifactRef, &recipeRef, &recipeGeneration, &reference, &manifestDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrNotFound
	}
	if err != nil || storedArtifactRef != artifactRef || recipeGeneration < 1 || reference == "" || manifestDigest == "" {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrUnavailable
	}
	artifact, inventoryErr := scanRoleImageArtifact(tx.QueryRow(ctx, queryRoleImagesGetActiveArtifact, organizationID, storedArtifactRef))
	if inventoryErr != nil || artifact.ManifestDigest != manifestDigest || artifact.PromotedReference != reference {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrUnavailable
	}
	available := verifiedImageToolCommands(artifact.ToolInventory)
	normalized := append([]entity.RuntimeEnvironmentTool(nil), tools...)
	if normalized == nil {
		normalized = []entity.RuntimeEnvironmentTool{}
	}
	sort.Slice(normalized, func(left, right int) bool { return normalized[left].Command < normalized[right].Command })
	for index, tool := range normalized {
		if strings.TrimSpace(tool.Name) != tool.Name || tool.Name == "" || len(tool.Name) > 160 ||
			strings.TrimSpace(tool.Command) != tool.Command || tool.Command == "" || len(tool.Command) > 160 ||
			strings.TrimSpace(tool.Description) != tool.Description || tool.Description == "" || len(tool.Description) > 500 ||
			len(tool.UsageHint) > 500 || !utf8.ValidString(tool.Name+tool.Command+tool.Description+tool.UsageHint) ||
			strings.ContainsRune(tool.Name+tool.Command+tool.Description+tool.UsageHint, 0) {
			return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
		}
		if _, ok := available[tool.Command]; !ok || index > 0 && normalized[index-1].Command == tool.Command {
			return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
	}
	image := entity.RuntimeEnvironmentImage{ArtifactRef: storedArtifactRef, RecipeRef: recipeRef,
		RecipeGeneration: recipeGeneration, Reference: reference, Digest: manifestDigest}
	return artifactID, image, normalized, encoded, nil
}

// bindRuntimeEnvironment locks and version-checks the editable agent, binds the exact environment/version through scoped SQL and advances agent metadata before reading back its runtime view. The binding digest includes the next binding generation; resource authorization and transaction commit remain outside.
// bindRuntimeEnvironment блокирует и сверяет версию изменяемого агента, связывает точные environment/version через scoped SQL и продвигает метаданные агента до повторного чтения runtime view. Digest привязки включает следующее поколение; авторизация ресурса и commit транзакции остаются снаружи.
func (repository *Repository) bindRuntimeEnvironment(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentBindingInput)
	if !ok || payload.AgentRef == "" || payload.EnvironmentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if !validRuntimeConfigurationAgent(agent) {
		return commandOutcome{}, errs.ErrProtected
	}
	if *input.Mutation.ExpectedVersion != agent.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	scopeKind := "PROJECT"
	if agent.projectID == "" {
		scopeKind = "ORGANIZATION"
	}
	target, err := scanRuntimeEnvironmentSecretTarget(tx.QueryRow(ctx, queryRuntimeEnvironmentSecretBindingTarget, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "project_id": agent.projectID, "scope_kind": scopeKind,
		"environment_ref": payload.EnvironmentRef, "version_ref": payload.VersionRef,
	}))
	if err != nil {
		return commandOutcome{}, err
	}
	if err := requireRuntimeEnvironmentSecretDescriptors(ctx, tx, scope.organizationID, target); err != nil {
		return commandOutcome{}, err
	}
	digest := digestBytes([]byte(payload.AgentRef), []byte(payload.EnvironmentRef), []byte(strconvFormat(agent.bindingVersion+1)))
	var bindingRef string
	err = tx.QueryRow(ctx, queryRuntimeConfigurationBindEnvironment, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "environment_ref": payload.EnvironmentRef, "project_id": agent.projectID,
		"version_ref": payload.VersionRef,
		"agent_id":    agent.id, "expected_version": agent.bindingVersion, "digest": digest, "updated_by": scope.actorID,
	}).Scan(&bindingRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil || bindingRef == "" {
		return commandOutcome{}, mapWriteError(err)
	}
	if _, err := tx.Exec(ctx, queryCommandsChangeinstructionsUpdateAgentsVersionUpdatedAt, agent.id); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return runtimeConfigurationOutcome(view, agent, "i18n:AGENT_RUNTIME_ENVIRONMENT_BOUND"), nil
}

// lockOverlayDraft locks the current agent draft within the organization and returns its exact identity/version/state/content, mapping NoRows separately from a driver failure.
// lockOverlayDraft блокирует текущий черновик агента в организации и возвращает точные identity/version/state/content, отдельно классифицируя NoRows и отказ драйвера.
func lockOverlayDraft(ctx context.Context, tx pgx.Tx, organizationID, agentRef string) (string, string, int64, string, string, error) {
	var id, ref, state, content string
	var version int64
	err := tx.QueryRow(ctx, queryRuntimeConfigurationGetOverlayDraft, organizationID, agentRef).Scan(&id, &ref, &version, &state, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", 0, "", "", errs.ErrNotFound
	}
	if err != nil {
		return "", "", 0, "", "", errs.ErrUnavailable
	}
	return id, ref, version, state, content, nil
}

// getRuntimeConfigurationViewTx reads the owner projection after its caller has authorized the exact agent, then populates context bindings and overlay schema within the same transaction. The OWNER query selector is an internal projection choice, not permission derived from request payload.
// getRuntimeConfigurationViewTx читает owner-проекцию после проверки точного агента вызывающим, затем наполняет context bindings и overlay schema в той же транзакции. Внутренний селектор OWNER выбирает проекцию, а не назначает полномочия из request payload.
func (repository *Repository) getRuntimeConfigurationViewTx(ctx context.Context, tx pgx.Tx, scope scope, ref string) (entity.AgentRuntimeConfigurationView, error) {
	// Caller уже проверил точное право Agent; legacy membership не заменяет эту policy.
	view, err := repository.scanAgentRuntimeConfigurationView(tx.QueryRow(ctx, queryRuntimeConfigurationGetAgentView,
		scope.organizationID, ref, "OWNER", scope.actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentRuntimeConfigurationView{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, errs.ErrUnavailable
	}
	if err := repository.populateContextBindings(ctx, tx, scope, ref, &view); err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	if err := populateRuntimeOverlaySchema(ctx, tx, scope, &view); err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	return view, nil
}

// getRuntimeEnvironmentTx reads the scoped environment snapshot and additionally checks organization-resource owner access for organization environments. It preserves existing scan-error mapping; project eligibility is enforced by the scoped query/enclosing owner path.
// getRuntimeEnvironmentTx читает scoped snapshot окружения и дополнительно проверяет доступ владельца ресурса организации для общих окружений. Сохраняет прежнюю классификацию scan-error; проектную доступность обеспечивает scoped-запрос/внешний путь владельца.
func (repository *Repository) getRuntimeEnvironmentTx(ctx context.Context, tx pgx.Tx, scope scope, ref string) (entity.RuntimeEnvironmentSet, error) {
	item, err := repository.scanRuntimeEnvironment(tx.QueryRow(ctx, queryRuntimeConfigurationGetEnvironment,
		scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, errs.ErrUnavailable
	}
	if item.ScopeKind == "ORGANIZATION" {
		if err := repository.requireRuntimeEnvironmentOwnerAccess(ctx, tx, scope, item.ScopeKind, item.ProjectRef); err != nil {
			return entity.RuntimeEnvironmentSet{}, err
		}
	}
	return item, nil
}

// runtimeConfigurationOutcome wraps the exact runtime configuration view with its agent/project coordinates, summary and AGENT_CHANGED event intent for the enclosing owner transaction; it emits nothing itself.
// runtimeConfigurationOutcome оборачивает точный runtime configuration view с agent/project координатами, сводкой и намерением события AGENT_CHANGED для внешней транзакции владельца; самостоятельно ничего не публикует.
func runtimeConfigurationOutcome(view entity.AgentRuntimeConfigurationView, agent lockedRuntimeAgent, summary string) commandOutcome {
	return commandOutcome{result: command.Result{RuntimeConfiguration: &view}, projectID: agent.projectID,
		projectRef: agent.projectRef, resourceKind: "AGENT_RUNTIME_CONFIGURATION", resourceRef: view.Configuration.AgentRef,
		summary: summary, platformEvent: "AGENT_CHANGED"}
}

// validateEnvironmentPayload copies values and secret descriptors without reordering caller slices, maps them to the runtime contract and validates the full environment before JSON encoding. Only descriptors, never plaintext secret bytes, enter the stored secret projection.
// validateEnvironmentPayload копирует значения и дескрипторы секретов без изменения порядка срезов вызывающего, переносит их в runtime-контракт и проверяет окружение до JSON-кодирования. В сохраняемую проекцию секретов попадают лишь дескрипторы, не открытые байты секретов.
func validateEnvironmentPayload(values []entity.RuntimeEnvironmentValue, secrets []entity.RuntimeSecretDescriptor) ([]byte, []byte, []runtimecontract.RuntimeEnvironmentValue, []runtimecontract.RuntimeSecretProjection, error) {
	normalizedValues := make([]entity.RuntimeEnvironmentValue, len(values))
	copy(normalizedValues, values)
	normalizedSecrets := make([]entity.RuntimeSecretDescriptor, len(secrets))
	copy(normalizedSecrets, secrets)
	contractValues := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(values))
	for _, item := range normalizedValues {
		contractValues = append(contractValues, runtimecontract.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	contractSecrets := make([]runtimecontract.RuntimeSecretProjection, 0, len(secrets))
	for _, item := range normalizedSecrets {
		contractSecrets = append(contractSecrets, runtimecontract.RuntimeSecretProjection{Name: item.Name,
			SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUID: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSHA256: item.ContentSHA256})
	}
	if err := runtimecontract.ValidateRuntimeEnvironment(contractValues, contractSecrets); err != nil {
		return nil, nil, nil, nil, err
	}
	rawValues, _ := json.Marshal(normalizedValues)
	rawSecrets, _ := json.Marshal(normalizedSecrets)
	return rawValues, rawSecrets, contractValues, contractSecrets, nil
}

// resolveEnvironmentPayload delegates the ordinary project environment values/secret bindings to the scoped resolver with a server-selected PROJECT kind.
// resolveEnvironmentPayload передаёт обычные значения/привязки секретов проектного окружения scoped-разрешителю с назначенным сервером видом PROJECT.
func (repository *Repository) resolveEnvironmentPayload(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, projectID string,
	values []entity.RuntimeEnvironmentValue,
	bindings []entity.RuntimeSecretBinding,
) ([]byte, []byte, []runtimecontract.RuntimeEnvironmentValue, []runtimecontract.RuntimeSecretProjection, error) {
	return repository.resolveScopedEnvironmentPayload(ctx, tx, organizationID, projectID, "PROJECT", values, bindings)
}

// resolveScopedEnvironmentPayload validates project/organization shape and bounded unique binding names, resolves each exact secret revision through owner-scoped SQL and validates the resulting values/descriptors with the runtime contract. Missing secrets and infrastructure failures remain distinct; the resolver never reads secret values.
// resolveScopedEnvironmentPayload проверяет форму проекта/организации и ограниченные уникальные имена привязок, разрешает точную ревизию каждого секрета через owner-scoped SQL и проверяет значения/дескрипторы runtime-контрактом. Отсутствие секретов и инфраструктурный отказ различаются; значения секретов не читаются.
func (repository *Repository) resolveScopedEnvironmentPayload(ctx context.Context, tx pgx.Tx, organizationID, projectID, scopeKind string, values []entity.RuntimeEnvironmentValue, bindings []entity.RuntimeSecretBinding) ([]byte, []byte, []runtimecontract.RuntimeEnvironmentValue, []runtimecontract.RuntimeSecretProjection, error) {
	if scopeKind != "ORGANIZATION" && scopeKind != "PROJECT" || (scopeKind == "ORGANIZATION") != (projectID == "") {
		return nil, nil, nil, nil, errs.ErrInvalid
	}
	if len(bindings) > 128 {
		return nil, nil, nil, nil, errs.ErrInvalid
	}
	seen := make(map[string]struct{}, len(bindings))
	descriptors := make([]entity.RuntimeSecretDescriptor, 0, len(bindings))
	for _, binding := range bindings {
		if !strings.HasPrefix(binding.SecretRef, "sec_") || len(binding.SecretRef) > 96 ||
			strings.TrimSpace(binding.Name) != binding.Name || binding.Name == "" || binding.Revision < 0 {
			return nil, nil, nil, nil, errs.ErrInvalid
		}
		if _, duplicate := seen[binding.Name]; duplicate {
			return nil, nil, nil, nil, errs.ErrInvalid
		}
		seen[binding.Name] = struct{}{}
		item := entity.RuntimeSecretDescriptor{Name: binding.Name}
		if err := tx.QueryRow(ctx, queryRuntimeSecretResolveBinding, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID, "scope_kind": scopeKind, "secret_ref": binding.SecretRef,
			"revision": binding.Revision,
		}).Scan(&item.SecretRef, &item.Namespace, &item.Revision, &item.SecretName, &item.SecretKey, &item.SecretUID, &item.SecretResourceVersion, &item.ContentSHA256); errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil, nil, errs.ErrNotFound
		} else if err != nil {
			return nil, nil, nil, nil, errs.ErrUnavailable
		}
		descriptors = append(descriptors, item)
	}
	encodedValues, encodedSecrets, contractValues, contractSecrets, err := validateEnvironmentPayload(values, descriptors)
	if err != nil {
		return nil, nil, nil, nil, errs.ErrInvalid
	}
	return encodedValues, encodedSecrets, contractValues, contractSecrets, nil
}

// runtimeEnvironmentConfigurationDigests maps image/tools into the shared runtime contract and computes separate core-content and complete policy-inclusive digests in their established order, propagating validation failure without changing caller data.
// runtimeEnvironmentConfigurationDigests переносит образ/инструменты в общий runtime-контракт и вычисляет отдельные digest базового содержимого и полного набора с policy в прежнем порядке, возвращая ошибку проверки без изменения данных вызывающего.
func runtimeEnvironmentConfigurationDigests(
	values []runtimecontract.RuntimeEnvironmentValue,
	secrets []runtimecontract.RuntimeSecretProjection,
	image entity.RuntimeEnvironmentImage,
	tools []entity.RuntimeEnvironmentTool,
	policy runtimecontract.RuntimeEnvironmentPolicy,
) (string, string, error) {
	contractTools := make([]runtimecontract.RuntimeEnvironmentTool, 0, len(tools))
	for _, tool := range tools {
		contractTools = append(contractTools, runtimecontract.RuntimeEnvironmentTool{
			Name: tool.Name, Command: tool.Command, Description: tool.Description, UsageHint: tool.UsageHint,
		})
	}
	contractImage := runtimecontract.RuntimeEnvironmentImage{
		ArtifactRef: image.ArtifactRef, RecipeRef: image.RecipeRef, RecipeGeneration: image.RecipeGeneration,
		Reference: image.Reference, Digest: image.Digest,
	}
	coreDigest, err := runtimecontract.RuntimeEnvironmentCoreDigest(values, secrets, contractImage, contractTools)
	if err != nil {
		return "", "", err
	}
	digest, err := runtimecontract.RuntimeEnvironmentDigest(values, secrets, contractImage, contractTools, policy)
	return coreDigest, digest, err
}

// admitRuntimeEnvironmentPolicy normalizes policy and refuses Kubernetes privileges, then requires privileged environment access plus fresh five-minute credential authentication when web access is enabled. Resource target derives from the resolved environment/project or organization scope; normalization alone does not authorize network access.
// admitRuntimeEnvironmentPolicy нормализует policy и отвергает привилегии Kubernetes, затем требует привилегированный доступ к окружению и свежую пятиминутную аутентификацию при включённом web-доступе. Цель ресурса выводится из разрешённого environment/project либо области организации; одной нормализации недостаточно для разрешения сети.
func (repository *Repository) admitRuntimeEnvironmentPolicy(
	ctx context.Context,
	tx pgx.Tx,
	current scope,
	projectRef string,
	environmentRef string,
	policy runtimecontract.RuntimeEnvironmentPolicy,
) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	normalized, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil || normalized.KubernetesAccess.Kind != runtimecontract.RuntimeKubernetesAccessNone {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errs.ErrInvalid
	}
	if normalized.Network.WebAccess.Mode != runtimecontract.RuntimeWebAccessNone {
		resourceKind, resourceRef := "RUNTIME_ENVIRONMENT", environmentRef
		if environmentRef == "" {
			resourceKind, resourceRef = "PROJECT", projectRef
		}
		target := resolvedAccessTarget{scope: organizationTarget(current.organizationRef)}
		if projectRef != "" {
			var resolveErr error
			target, resolveErr = repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{
				ProjectRef: projectRef, ResourceKind: resourceKind, ResourceRef: resourceRef,
			})
			if resolveErr != nil {
				return runtimecontract.RuntimeEnvironmentPolicy{}, resolveErr
			}
		}
		if accessErr := repository.requireAccess(ctx, tx, current, "environment.privileged.manage", target); accessErr != nil {
			return runtimecontract.RuntimeEnvironmentPolicy{}, accessErr
		}
		now := time.Now().UTC()
		if !runtimeEnvironmentAuthenticationIsFresh(current.credentialAuthenticatedAt, now) {
			return runtimecontract.RuntimeEnvironmentPolicy{}, errs.ErrFreshAuthenticationRequired
		}
	}
	return normalized, nil
}

// runtimeEnvironmentAuthenticationIsFresh applies the canonical Principal freshness rule with the environment-specific five-minute window; callers supply both the verified authentication time and current time.
// runtimeEnvironmentAuthenticationIsFresh применяет каноническое правило свежести Principal с пятиминутным окном окружения; вызывающий передаёт проверенное время аутентификации и текущее время.
func runtimeEnvironmentAuthenticationIsFresh(authenticatedAt, now time.Time) bool {
	return (value.Principal{CredentialAuthenticatedAt: authenticatedAt}).AuthenticationIsFresh(now, 5*time.Minute)
}

// validProviderPolicy accepts only FIXED/LEAST_USED/WEIGHTED with bounded unique account refs and valid weights, requiring exactly one fixed candidate and unit weights outside WEIGHTED. It validates syntax/cardinality, while current account/model authorization is checked separately.
// validProviderPolicy принимает лишь FIXED/LEAST_USED/WEIGHTED с ограниченными уникальными account refs и допустимыми весами, требуя одного фиксированного кандидата и единичные веса вне WEIGHTED. Проверяет синтаксис/количество, а актуальные права на аккаунт/модель проверяются отдельно.
func validProviderPolicy(mode string, candidates []entity.ProviderAccountCandidate) bool {
	if !contains([]string{"FIXED", "LEAST_USED", "WEIGHTED"}, mode) || len(candidates) < 1 || len(candidates) > 128 ||
		(mode == "FIXED" && len(candidates) != 1) {
		return false
	}
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !strings.HasPrefix(candidate.AccountRef, "pacc_") || len(candidate.AccountRef) > 96 ||
			candidate.Weight < 1 || candidate.Weight > 100 || (mode != "WEIGHTED" && candidate.Weight != 1) {
			return false
		}
		if _, duplicate := seen[candidate.AccountRef]; duplicate {
			return false
		}
		seen[candidate.AccountRef] = struct{}{}
	}
	return true
}

// validModel checks a nonempty trimmed UTF-8 model identifier within 128 bytes and the existing ASCII letter/digit/punctuation alphabet. It does not prove that the model exists or is eligible for an account.
// validModel проверяет непустой UTF-8 идентификатор модели без краевых пробелов в пределах 128 байтов и прежнего ASCII набора букв/цифр/знаков. Не доказывает существование модели и её доступность аккаунту.
func validModel(model string) bool {
	if strings.TrimSpace(model) != model || len(model) < 1 || len(model) > 128 || !utf8.ValidString(model) {
		return false
	}
	for _, character := range model {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || strings.ContainsRune("._:/-", character) {
			continue
		}
		return false
	}
	return true
}

// digestBytes hashes the ordered byte parts with a trailing NUL after each part and returns lowercase SHA-256 hex; it does not sort, normalize JSON or sign the input.
// digestBytes хеширует упорядоченные части байтов с завершающим NUL после каждой и возвращает строчный SHA-256 hex; не сортирует, не нормализует JSON и не подписывает вход.
func digestBytes(parts ...[]byte) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write(part)
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// strconvFormat encodes a signed version counter as base-10 text for the established digest inputs without changing counter range or generation semantics.
// strconvFormat кодирует знаковый счётчик версии десятичным текстом для прежних digest inputs без изменения диапазона и смысла поколения.
func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}
