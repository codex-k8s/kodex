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
	policyRef, _ := newRef("ppol")
	configRef, _ := newRef("rconf")
	overlayRef, _ := newRef("cov")
	environmentRef, _ := newRef("renv")
	environmentVersionRef, _ := newRef("renvv")
	bindingRef, _ := newRef("aenv")
	var updatedAgentID, runtimeEnvironmentID, runtimeEnvironmentVersionID string
	err := tx.QueryRow(ctx, queryRuntimeConfigurationBootstrapAgent, pgx.StrictNamedArgs{
		"organization_id": organizationID, "agent_id": agentID, "project_id": projectID,
		"policy_ref": policyRef, "config_ref": configRef, "overlay_ref": overlayRef,
		"environment_ref": environmentRef, "environment_version_ref": environmentVersionRef,
		"binding_ref": bindingRef, "runtime_profile_ref": runtime.Ref, "provider": runtime.Provider,
		"model": runtime.Model, "created_by": createdBy,
	}).Scan(&updatedAgentID, &runtimeEnvironmentID, &runtimeEnvironmentVersionID)
	if err != nil {
		return fmt.Errorf("bootstrap agent runtime configuration: %w", err)
	}
	if updatedAgentID != agentID {
		return errors.New("bootstrap agent runtime configuration did not update the agent")
	}
	if _, err := tx.Exec(ctx, queryRuntimeConfigurationActivateEnvironment, runtimeEnvironmentID, runtimeEnvironmentVersionID); err != nil {
		return errors.New("activate bootstrap runtime environment version")
	}
	return nil
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
	values, secrets, digest, err := validateEnvironmentPayload(payload.Values, payload.SecretDescriptors)
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
		environmentRef, _ := newRef("renv")
		versionRef, _ := newRef("renvv")
		var created string
		err := tx.QueryRow(ctx, queryRuntimeConfigurationCreateEnvironment, pgx.StrictNamedArgs{
			"environment_ref": environmentRef, "version_ref": versionRef, "organization_id": scope.organizationID,
			"project_id": projectID, "name": strings.TrimSpace(payload.Name), "description": strings.TrimSpace(payload.Description),
			"created_by": scope.actorID, "non_secret_values": values, "secret_descriptors": secrets, "digest": digest,
		}).Scan(&created)
		if err != nil || created != environmentRef {
			return commandOutcome{}, mapWriteError(err)
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
		err = tx.QueryRow(ctx, queryRuntimeConfigurationPublishEnvironment, pgx.StrictNamedArgs{
			"version_ref": versionRef, "organization_id": scope.organizationID, "environment_id": environmentID,
			"version_number": currentRevision + 1, "parent_version_id": currentVersionID,
			"non_secret_values": values, "secret_descriptors": secrets, "digest": digest, "created_by": scope.actorID,
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

func validateEnvironmentPayload(values []entity.RuntimeEnvironmentValue, secrets []entity.RuntimeSecretDescriptor) ([]byte, []byte, string, error) {
	contractValues := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(values))
	for _, item := range values {
		contractValues = append(contractValues, runtimecontract.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	contractSecrets := make([]runtimecontract.RuntimeSecretProjection, 0, len(secrets))
	for _, item := range secrets {
		contractSecrets = append(contractSecrets, runtimecontract.RuntimeSecretProjection{Name: item.Name,
			SecretName: item.SecretName, SecretKey: item.SecretKey, SecretUID: item.SecretUID,
			SecretResourceVersion: item.SecretResourceVersion, ContentSHA256: item.ContentSHA256})
	}
	digest, err := runtimecontract.RuntimeEnvironmentDigest(contractValues, contractSecrets)
	if err != nil {
		return nil, nil, "", err
	}
	rawValues, _ := json.Marshal(values)
	rawSecrets, _ := json.Marshal(secrets)
	return rawValues, rawSecrets, digest, nil
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
