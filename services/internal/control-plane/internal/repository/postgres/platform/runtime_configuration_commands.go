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
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

type lockedRuntimeAgent struct {
	id, projectID, projectRef, overlayID, runtimeProfileRef string
	agentVersion, configVersion                             int64
	overlayVersion, bindingVersion                          int64
}

func (repository *Repository) bootstrapAgentRuntime(ctx context.Context, tx pgx.Tx, organizationID, agentID, projectID string, runtime entity.RuntimeSelection, createdBy string) error {
	imageArtifactID, image, selectedTools, err := repository.ensureBootstrapRuntimeEnvironmentImage(
		ctx, tx, organizationID, agentID, projectID, createdBy)
	if err != nil {
		return err
	}
	environmentDigest, err := runtimeEnvironmentConfigurationDigest(nil, nil, image, nil)
	if err != nil {
		return errors.New("compute bootstrap runtime environment digest")
	}
	policyRef, _ := newRef("ppol")
	configRef, _ := newRef("rconf")
	overlayRef, _ := newRef("cov")
	environmentRef, _ := newRef("renv")
	environmentVersionRef, _ := newRef("renvv")
	bindingRef, _ := newRef("aenv")
	var updatedAgentID, runtimeEnvironmentID, runtimeEnvironmentVersionID string
	err = tx.QueryRow(ctx, queryRuntimeConfigurationBootstrapAgent, pgx.StrictNamedArgs{
		"organization_id": organizationID, "agent_id": agentID, "project_id": projectID,
		"policy_ref": policyRef, "config_ref": configRef, "overlay_ref": overlayRef,
		"environment_ref": environmentRef, "environment_version_ref": environmentVersionRef,
		"binding_ref": bindingRef, "runtime_profile_ref": runtime.Ref, "provider": runtime.Provider,
		"model": runtime.Model, "created_by": createdBy,
		"environment_image_artifact_id": imageArtifactID, "environment_selected_tools": selectedTools,
		"environment_digest": environmentDigest,
	}).Scan(&updatedAgentID, &runtimeEnvironmentID, &runtimeEnvironmentVersionID)
	if err != nil {
		return fmt.Errorf("bootstrap agent runtime configuration: %w", err)
	}
	if updatedAgentID != agentID {
		return errors.New("bootstrap agent runtime configuration did not update the agent")
	}
	activation, err := tx.Exec(ctx, queryRuntimeConfigurationActivateEnvironment, runtimeEnvironmentID, runtimeEnvironmentVersionID)
	if err != nil || activation.RowsAffected() != 1 {
		return errors.New("activate bootstrap runtime environment version")
	}
	return nil
}

func (repository *Repository) ensureBootstrapRuntimeEnvironmentImage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, agentID, projectID, createdBy string,
) (string, entity.RuntimeEnvironmentImage, []byte, error) {
	emptyTools, _ := json.Marshal([]entity.RuntimeEnvironmentTool{})
	if projectID == "" {
		return "", entity.RuntimeEnvironmentImage{
			Reference: repository.roleImages.DefaultImageReference,
			Digest:    repository.roleImages.DefaultImageDigest,
		}, emptyTools, nil
	}
	image, err := scanBootstrapRuntimeEnvironmentImage(tx.QueryRow(ctx,
		queryRuntimeConfigurationResolveBootstrapImage, pgx.StrictNamedArgs{
			"organization_id": organizationID, "project_id": projectID,
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
		SourceRef:          "platform-owned:default-role-image",
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
	return image.id, image.image, emptyTools, nil
}

type bootstrapRuntimeEnvironmentImage struct {
	id    string
	image entity.RuntimeEnvironmentImage
}

func scanBootstrapRuntimeEnvironmentImage(row interface{ Scan(...any) error }) (bootstrapRuntimeEnvironmentImage, error) {
	var result bootstrapRuntimeEnvironmentImage
	err := row.Scan(&result.id, &result.image.ArtifactRef, &result.image.RecipeRef,
		&result.image.RecipeGeneration, &result.image.Reference, &result.image.Digest)
	return result, err
}

func (repository *Repository) selectProviderAccountForAgent(ctx context.Context, tx pgx.Tx, organizationID, agentRef string) (string, error) {
	var accountID, accountRef, configRef, configDigest, policyRef, policyDigest string
	var configVersion, policyVersion int64
	err := tx.QueryRow(ctx, queryRuntimeConfigurationSelectProviderAccount, organizationID, agentRef).Scan(
		&accountID, &accountRef, &configRef, &configVersion, &configDigest, &policyRef, &policyVersion, &policyDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errs.ErrConflict
	}
	if err != nil || accountRef == "" || configRef == "" || configVersion < 1 || len(configDigest) != 64 ||
		policyRef == "" || policyVersion < 1 || len(policyDigest) != 64 {
		return "", errs.ErrUnavailable
	}
	return accountID, nil
}

func (repository *Repository) changeRuntimeConfiguration(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.PublishAgentRuntimeConfig:
		return repository.publishAgentRuntimeConfiguration(ctx, tx, scope, input)
	case command.CreateConfigOverlayDraft, command.ValidateConfigOverlayDraft,
		command.PublishConfigOverlayDraft, command.RollbackConfigOverlay:
		return repository.changeConfigOverlay(ctx, tx, scope, input)
	case command.CreateRuntimeEnvironment, command.PublishRuntimeEnvironment, command.RollbackRuntimeEnvironment:
		return repository.changeRuntimeEnvironment(ctx, tx, scope, input)
	case command.BindAgentRuntimeEnvironment:
		return repository.bindRuntimeEnvironment(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) lockRuntimeAgent(ctx context.Context, tx pgx.Tx, scope scope, ref string) (lockedRuntimeAgent, error) {
	var agent lockedRuntimeAgent
	err := tx.QueryRow(ctx, queryRuntimeConfigurationLockAgent, scope.organizationID, ref).Scan(
		&agent.id, &agent.projectID, &agent.projectRef, &agent.agentVersion, &agent.configVersion,
		&agent.overlayID, &agent.overlayVersion, &agent.bindingVersion, &agent.runtimeProfileRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedRuntimeAgent{}, errs.ErrNotFound
	}
	if err != nil {
		return lockedRuntimeAgent{}, errs.ErrUnavailable
	}
	return agent, nil
}

func (repository *Repository) publishAgentRuntimeConfiguration(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.AgentRuntimeConfigurationInput)
	if !ok || payload.AgentRef == "" || payload.RuntimeProfileRef == "" || input.Mutation.ExpectedVersion == nil ||
		!validModel(payload.Model) || !validProviderPolicy(payload.ProviderPolicyMode, payload.ProviderAccounts) {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if agent.projectID == "" {
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

func (repository *Repository) changeConfigOverlay(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.ConfigOverlayInput)
	if !ok || payload.AgentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if agent.projectID == "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if *input.Mutation.ExpectedVersion != agent.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
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
		if _, _, parseErr := runtimecontract.CanonicalConfigOverlay(content); parseErr != nil {
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
	return runtimeConfigurationOutcome(view, agent, "i18n:AGENT_CONFIG_OVERLAY_CHANGED"), nil
}

func (repository *Repository) changeRuntimeEnvironment(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	values, secrets, contractValues, contractSecrets, err := validateEnvironmentPayload(payload.Values, payload.SecretDescriptors)
	if err != nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	if input.Kind == command.CreateRuntimeEnvironment {
		if payload.ProjectRef == "" || strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 160 || len(payload.Description) > 2000 {
			return commandOutcome{}, errs.ErrInvalid
		}
		projectID := mustProjectID(ctx, tx, scope.organizationID, payload.ProjectRef)
		if projectID == "" {
			return commandOutcome{}, errs.ErrNotFound
		}
		imageArtifactID, image, normalizedTools, selectedTools, resolveErr := repository.resolveRuntimeEnvironmentImage(
			ctx, tx, scope.organizationID, projectID, payload.ImageArtifactRef, payload.Tools)
		if resolveErr != nil {
			return commandOutcome{}, resolveErr
		}
		digest, digestErr := runtimeEnvironmentConfigurationDigest(contractValues, contractSecrets, image, normalizedTools)
		if digestErr != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		environmentRef, _ := newRef("renv")
		versionRef, _ := newRef("renvv")
		var environmentID, environmentVersionID, created string
		err := tx.QueryRow(ctx, queryRuntimeConfigurationCreateEnvironment, pgx.StrictNamedArgs{
			"environment_ref": environmentRef, "version_ref": versionRef, "organization_id": scope.organizationID,
			"project_id": projectID, "name": strings.TrimSpace(payload.Name), "description": strings.TrimSpace(payload.Description),
			"created_by": scope.actorID, "non_secret_values": values, "secret_descriptors": secrets, "digest": digest,
			"image_artifact_id": imageArtifactID, "selected_tools": selectedTools,
		}).Scan(&environmentID, &environmentVersionID, &created)
		if err != nil {
			return commandOutcome{}, fmt.Errorf("create runtime environment storage: %w", mapWriteError(err))
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
	err = tx.QueryRow(ctx, queryRuntimeConfigurationLockEnvironment, scope.organizationID, payload.Ref).Scan(
		&environmentID, &projectID, &projectRef, &environmentVersion, &currentVersionID, &currentRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrNotFound
	}
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if *input.Mutation.ExpectedVersion != environmentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	versionRef, _ := newRef("renvv")
	var changed string
	if input.Kind == command.PublishRuntimeEnvironment {
		if strings.TrimSpace(payload.Name) == "" || len(payload.Name) > 160 || len(payload.Description) > 2000 {
			return commandOutcome{}, errs.ErrInvalid
		}
		imageArtifactID, image, normalizedTools, selectedTools, resolveErr := repository.resolveRuntimeEnvironmentImage(
			ctx, tx, scope.organizationID, projectID, payload.ImageArtifactRef, payload.Tools)
		if resolveErr != nil {
			return commandOutcome{}, resolveErr
		}
		digest, digestErr := runtimeEnvironmentConfigurationDigest(contractValues, contractSecrets, image, normalizedTools)
		if digestErr != nil {
			return commandOutcome{}, errs.ErrInvalid
		}
		err = tx.QueryRow(ctx, queryRuntimeConfigurationPublishEnvironment, pgx.StrictNamedArgs{
			"version_ref": versionRef, "organization_id": scope.organizationID, "environment_id": environmentID,
			"version_number": currentRevision + 1, "parent_version_id": currentVersionID,
			"non_secret_values": values, "secret_descriptors": secrets, "digest": digest, "created_by": scope.actorID,
			"image_artifact_id": imageArtifactID, "selected_tools": selectedTools,
			"name": strings.TrimSpace(payload.Name), "description": strings.TrimSpace(payload.Description),
		}).Scan(&changed)
	} else if input.Kind == command.RollbackRuntimeEnvironment && payload.PublishedVersionRef != "" {
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

func (repository *Repository) resolveRuntimeEnvironmentImage(
	ctx context.Context,
	tx pgx.Tx,
	organizationID, projectID, artifactRef string,
	tools []entity.RuntimeEnvironmentTool,
) (string, entity.RuntimeEnvironmentImage, []entity.RuntimeEnvironmentTool, []byte, error) {
	if !strings.HasPrefix(artifactRef, "imgart_") || len(artifactRef) > 96 || len(tools) > 128 {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrInvalid
	}
	var artifactID, storedArtifactRef, recipeRef, reference, manifestDigest string
	var recipeGeneration int64
	var rawSpecification []byte
	err := tx.QueryRow(ctx, queryRuntimeConfigurationResolveImageArtifact, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "artifact_ref": artifactRef,
	}).Scan(&artifactID, &storedArtifactRef, &recipeRef, &recipeGeneration, &reference, &manifestDigest, &rawSpecification)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrNotFound
	}
	if err != nil || storedArtifactRef != artifactRef || recipeGeneration < 1 || reference == "" || manifestDigest == "" {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrUnavailable
	}
	var specification entity.RoleImageRecipeInput
	if json.Unmarshal(rawSpecification, &specification) != nil {
		return "", entity.RuntimeEnvironmentImage{}, nil, nil, errs.ErrUnavailable
	}
	available := make(map[string]struct{}, len(specification.Tools))
	for _, tool := range specification.Tools {
		available[tool.Name] = struct{}{}
	}
	normalized := append([]entity.RuntimeEnvironmentTool(nil), tools...)
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

func (repository *Repository) bindRuntimeEnvironment(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.RuntimeEnvironmentBindingInput)
	if !ok || payload.AgentRef == "" || payload.EnvironmentRef == "" || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	agent, err := repository.lockRuntimeAgent(ctx, tx, scope, payload.AgentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if agent.projectID == "" {
		return commandOutcome{}, errs.ErrProtected
	}
	if *input.Mutation.ExpectedVersion != agent.agentVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	digest := digestBytes([]byte(payload.AgentRef), []byte(payload.EnvironmentRef), []byte(strconvFormat(agent.bindingVersion+1)))
	var bindingRef string
	err = tx.QueryRow(ctx, queryRuntimeConfigurationBindEnvironment, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "environment_ref": payload.EnvironmentRef, "project_id": agent.projectID,
		"agent_id": agent.id, "expected_version": agent.bindingVersion, "digest": digest, "updated_by": scope.actorID,
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

func (repository *Repository) getRuntimeConfigurationViewTx(ctx context.Context, tx pgx.Tx, scope scope, ref string) (entity.AgentRuntimeConfigurationView, error) {
	view, err := scanAgentRuntimeConfigurationView(tx.QueryRow(ctx, queryRuntimeConfigurationGetAgentView,
		scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, errs.ErrUnavailable
	}
	return view, nil
}

func (repository *Repository) getRuntimeEnvironmentTx(ctx context.Context, tx pgx.Tx, scope scope, ref string) (entity.RuntimeEnvironmentSet, error) {
	item, err := scanRuntimeEnvironment(tx.QueryRow(ctx, queryRuntimeConfigurationGetEnvironment,
		scope.organizationID, ref, scope.role, scope.actorID))
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, errs.ErrUnavailable
	}
	return item, nil
}

func runtimeConfigurationOutcome(view entity.AgentRuntimeConfigurationView, agent lockedRuntimeAgent, summary string) commandOutcome {
	return commandOutcome{result: command.Result{RuntimeConfiguration: &view}, projectID: agent.projectID,
		projectRef: agent.projectRef, resourceKind: "AGENT_RUNTIME_CONFIGURATION", resourceRef: view.Configuration.AgentRef,
		summary: summary, platformEvent: "AGENT_CHANGED"}
}

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

func runtimeEnvironmentConfigurationDigest(
	values []runtimecontract.RuntimeEnvironmentValue,
	secrets []runtimecontract.RuntimeSecretProjection,
	image entity.RuntimeEnvironmentImage,
	tools []entity.RuntimeEnvironmentTool,
) (string, error) {
	contractTools := make([]runtimecontract.RuntimeEnvironmentTool, 0, len(tools))
	for _, tool := range tools {
		contractTools = append(contractTools, runtimecontract.RuntimeEnvironmentTool{
			Name: tool.Name, Command: tool.Command, Description: tool.Description, UsageHint: tool.UsageHint,
		})
	}
	return runtimecontract.RuntimeEnvironmentDigest(values, secrets, runtimecontract.RuntimeEnvironmentImage{
		ArtifactRef: image.ArtifactRef, RecipeRef: image.RecipeRef, RecipeGeneration: image.RecipeGeneration,
		Reference: image.Reference, Digest: image.Digest,
	}, contractTools)
}

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

func digestBytes(parts ...[]byte) string {
	digest := sha256.New()
	for _, part := range parts {
		_, _ = digest.Write(part)
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func strconvFormat(value int64) string {
	return strconv.FormatInt(value, 10)
}
