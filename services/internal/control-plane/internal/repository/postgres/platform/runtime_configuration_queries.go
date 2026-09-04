package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) GetAgentRuntimeConfiguration(ctx context.Context, principal value.Principal, ref string) (entity.AgentRuntimeConfigurationView, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	row := repository.pool.QueryRow(ctx, queryRuntimeConfigurationGetAgentView, scope.organizationID, ref, scope.role, scope.actorID)
	view, err := repository.scanAgentRuntimeConfigurationView(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.AgentRuntimeConfigurationView{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, errs.ErrUnavailable
	}
	return view, nil
}

func (repository *Repository) ListAgentRuntimeConfigurations(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.AgentRuntimeConfiguration, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	before, err := versionCursor(filter.Page.Token)
	if err != nil || filter.ResourceRef == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryRuntimeConfigurationListAgentVersions, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "agent_ref": filter.ResourceRef, "before_version": before,
		"platform_role": scope.role, "actor_id": scope.actorID, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.AgentRuntimeConfiguration, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanAgentRuntimeConfiguration(rows)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatInt(items[len(items)-1].Version, 10)
	}
	return items, next, nil
}

func (repository *Repository) ListRuntimeEnvironments(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentSet, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if filter.ProjectRef == "" || (filter.Page.Token != "" && (!strings.HasPrefix(filter.Page.Token, "renv_") || len(filter.Page.Token) > 96)) {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryRuntimeConfigurationListEnvironments, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "project_ref": filter.ProjectRef, "query": strings.TrimSpace(filter.Query),
		"cursor_ref": filter.Page.Token, "platform_role": scope.role, "actor_id": scope.actorID, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RuntimeEnvironmentSet, 0, limit+1)
	for rows.Next() {
		item, scanErr := repository.scanRuntimeEnvironment(rows)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = items[len(items)-1].Ref
	}
	return items, next, nil
}

func (repository *Repository) GetRuntimeEnvironment(ctx context.Context, principal value.Principal, ref string) (entity.RuntimeEnvironmentSet, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, err
	}
	item, err := repository.scanRuntimeEnvironment(repository.pool.QueryRow(ctx, queryRuntimeConfigurationGetEnvironment, scope.organizationID, ref, scope.role, scope.actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RuntimeEnvironmentSet{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) ListRuntimeEnvironmentVersions(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.RuntimeEnvironmentVersion, string, error) {
	scope, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	before, err := versionCursor(filter.Page.Token)
	if err != nil || filter.ResourceRef == "" {
		return nil, "", errs.ErrInvalid
	}
	limit := boundedPage(filter.Page)
	rows, err := repository.pool.Query(ctx, queryRuntimeConfigurationListEnvironmentVersions, pgx.StrictNamedArgs{
		"organization_id": scope.organizationID, "environment_ref": filter.ResourceRef, "before_version": before,
		"platform_role": scope.role, "actor_id": scope.actorID, "page_size": limit + 1,
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	items := make([]entity.RuntimeEnvironmentVersion, 0, limit+1)
	for rows.Next() {
		item, scanErr := scanRuntimeEnvironmentVersion(rows)
		if scanErr != nil {
			return nil, "", errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, "", errs.ErrUnavailable
	}
	next := ""
	if len(items) > int(limit) {
		items = items[:limit]
		next = strconv.FormatInt(items[len(items)-1].Version, 10)
	}
	return items, next, nil
}

func (repository *Repository) ListTemplateVariables(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.TemplateVariable, string, error) {
	if _, err := repository.GetProject(ctx, principal, filter.ProjectRef); err != nil {
		return nil, "", err
	}
	catalog := templateVariableCatalog()
	needle := strings.ToLower(strings.TrimSpace(filter.Query))
	start := 0
	if filter.Page.Token != "" {
		for start < len(catalog) && catalog[start].Name <= filter.Page.Token {
			start++
		}
		if start == 0 {
			return nil, "", errs.ErrInvalid
		}
	}
	filtered := make([]entity.TemplateVariable, 0, len(catalog))
	for _, item := range catalog[start:] {
		if needle == "" || strings.Contains(strings.ToLower(item.Name+" "+item.Description), needle) {
			filtered = append(filtered, item)
		}
	}
	limit := int(boundedPage(filter.Page))
	next := ""
	if len(filtered) > limit {
		filtered = filtered[:limit]
		next = filtered[len(filtered)-1].Name
	}
	return filtered, next, nil
}

func templateVariableCatalog() []entity.TemplateVariable {
	return []entity.TemplateVariable{
		{Name: "agent.ref", Type: "reference", Description: "Ссылка текущего ИИ-сотрудника", Example: "{{ .agent.ref }}", Source: "AGENT"},
		{Name: "input.files", Type: "collection", Description: "Файлы текущего сообщения или запуска", Example: "{{ range .input.files }}{{ .path }}{{ end }}", Source: "INPUT"},
		{Name: "input.files_count", Type: "integer", Description: "Количество файлов текущего входа", Example: "{{ .input.files_count }}", Source: "INPUT"},
		{Name: "input.files_dir", Type: "string", Description: "Каталог файлов текущего входа в workspace", Example: "{{ .input.files_dir }}", Source: "INPUT"},
		{Name: "input.manifest_path", Type: "string", Description: "Путь к manifest текущего входа", Example: "{{ .input.manifest_path }}", Source: "INPUT"},
		{Name: "project.files", Type: "collection", Description: "Явно выбранные файлы знаний Проекта", Example: "{{ range .project.files }}{{ .path }}{{ end }}", Source: "PROJECT"},
		{Name: "project.files_count", Type: "integer", Description: "Количество выбранных файлов знаний Проекта", Example: "{{ .project.files_count }}", Source: "PROJECT"},
		{Name: "project.files_dir", Type: "string", Description: "Каталог выбранных файлов знаний Проекта", Example: "{{ .project.files_dir }}", Source: "PROJECT"},
		{Name: "project.manifest_path", Type: "string", Description: "Путь к manifest выбранных знаний Проекта", Example: "{{ .project.manifest_path }}", Source: "PROJECT"},
		{Name: "project.ref", Type: "reference", Description: "Ссылка текущего Проекта", Example: "{{ .project.ref }}", Source: "PROJECT"},
		{Name: "run.files", Type: "collection", Description: "Файлы текущего запуска, вошедшие в RuntimeRevision", Example: "{{ range .run.files }}{{ .path }}{{ end }}", Source: "RUN"},
		{Name: "run.files_count", Type: "integer", Description: "Количество файлов текущего запуска", Example: "{{ .run.files_count }}", Source: "RUN"},
		{Name: "run.files_dir", Type: "string", Description: "Каталог файлов текущего запуска", Example: "{{ .run.files_dir }}", Source: "RUN"},
		{Name: "run.manifest_path", Type: "string", Description: "Путь к manifest файлов текущего запуска", Example: "{{ .run.manifest_path }}", Source: "RUN"},
		{Name: "run.ref", Type: "reference", Description: "Ссылка текущего запуска", Example: "{{ .run.ref }}", Source: "RUN"},
		{Name: "runtime.environment.image.digest", Type: "string", Description: "Digest exact runtime image", Example: "{{ .runtime.environment.image.digest }}", Source: "RUNTIME"},
		{Name: "runtime.environment.image.reference", Type: "string", Description: "Exact reference runtime image", Example: "{{ .runtime.environment.image.reference }}", Source: "RUNTIME"},
		{Name: "runtime.environment.ref", Type: "reference", Description: "Ссылка опубликованного окружения", Example: "{{ .runtime.environment.ref }}", Source: "RUNTIME"},
		{Name: "runtime.environment.tools", Type: "collection", Description: "Проверенные инструменты exact runtime image", Example: "{{ range .runtime.environment.tools }}{{ .name }}: {{ .description }}{{ end }}", Source: "RUNTIME"},
		{Name: "session.files", Type: "collection", Description: "Immutable inputs текущей сессии", Example: "{{ range .session.files }}{{ .path }}{{ end }}", Source: "SESSION"},
		{Name: "session.files_count", Type: "integer", Description: "Количество доступных файлов текущей сессии", Example: "{{ .session.files_count }}", Source: "SESSION"},
		{Name: "session.files_dir", Type: "string", Description: "Корневой каталог файлов текущей сессии", Example: "{{ .session.files_dir }}", Source: "SESSION"},
		{Name: "session.manifest_path", Type: "string", Description: "Путь к manifest файлов текущей сессии", Example: "{{ .session.manifest_path }}", Source: "SESSION"},
		{Name: "session.ref", Type: "reference", Description: "Ссылка текущей сессии", Example: "{{ .session.ref }}", Source: "SESSION"},
		{Name: "turn.ref", Type: "reference", Description: "Ссылка текущего turn", Example: "{{ .turn.ref }}", Source: "SESSION"},
		{Name: "workflow.files", Type: "collection", Description: "Файлы входного snapshot текущего Workflow", Example: "{{ range .workflow.files }}{{ .path }}{{ end }}", Source: "WORKFLOW"},
		{Name: "workflow.files_count", Type: "integer", Description: "Количество файлов входного snapshot Workflow", Example: "{{ .workflow.files_count }}", Source: "WORKFLOW"},
		{Name: "workflow.files_dir", Type: "string", Description: "Каталог файлов входного snapshot Workflow", Example: "{{ .workflow.files_dir }}", Source: "WORKFLOW"},
		{Name: "workflow.manifest_path", Type: "string", Description: "Путь к manifest входного snapshot Workflow", Example: "{{ .workflow.manifest_path }}", Source: "WORKFLOW"},
	}
}

func scanAgentRuntimeConfiguration(scanner rowScanner) (entity.AgentRuntimeConfiguration, error) {
	var item entity.AgentRuntimeConfiguration
	var rawCandidates []byte
	err := scanner.Scan(&item.Ref, &item.Version, &item.AgentRef, &item.RuntimeProfileRef, &item.Provider,
		&item.Model, &item.Digest, &item.CreatedAt, &item.ProviderPolicy.Ref, &item.ProviderPolicy.Version,
		&item.ProviderPolicy.Mode, &rawCandidates, &item.ProviderPolicy.Digest, &item.ProviderPolicy.CreatedAt)
	if err != nil || decodeStrict(rawCandidates, &item.ProviderPolicy.AccountCandidates) != nil {
		return entity.AgentRuntimeConfiguration{}, errors.New("scan agent runtime configuration")
	}
	return item, nil
}

func (repository *Repository) scanAgentRuntimeConfigurationView(scanner rowScanner) (entity.AgentRuntimeConfigurationView, error) {
	var view entity.AgentRuntimeConfigurationView
	var rawCandidates, rawPublishedProblems, rawDraftProblems, rawValues, rawSecrets, rawTools []byte
	var rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte
	var coreDigest string
	var draftRef, draftState, draftContent, draftDigest *string
	var draftVersion *int64
	var draftCreatedAt, draftPublishedAt *time.Time
	err := scanner.Scan(&view.Configuration.Ref, &view.Configuration.Version, &view.Configuration.AgentRef,
		&view.Configuration.RuntimeProfileRef, &view.Configuration.Provider, &view.Configuration.Model,
		&view.Configuration.Digest, &view.Configuration.CreatedAt, &view.Configuration.ProviderPolicy.Ref,
		&view.Configuration.ProviderPolicy.Version, &view.Configuration.ProviderPolicy.Mode, &rawCandidates,
		&view.Configuration.ProviderPolicy.Digest, &view.Configuration.ProviderPolicy.CreatedAt,
		&view.PublishedOverlay.Ref, &view.PublishedOverlay.Revision, &view.PublishedOverlay.State,
		&view.PublishedOverlay.Content, &view.PublishedOverlay.Digest, &rawPublishedProblems,
		&view.PublishedOverlay.CreatedAt, &view.PublishedOverlay.PublishedAt,
		&draftRef, &draftVersion, &draftState, &draftContent, &draftDigest, &rawDraftProblems,
		&draftCreatedAt, &draftPublishedAt, &view.EnvironmentBinding.Ref, &view.EnvironmentBinding.Version,
		&view.EnvironmentBinding.Digest, &view.Environment.Ref, &view.Environment.Version, &view.Environment.ProjectRef,
		&view.Environment.Name, &view.Environment.Description, &view.Environment.State, &view.Environment.UpdatedAt,
		&view.Environment.CurrentVersion.Ref, &view.Environment.CurrentVersion.Revision, &rawValues, &rawSecrets,
		&view.Environment.CurrentVersion.Image.ArtifactRef, &view.Environment.CurrentVersion.Image.RecipeRef,
		&view.Environment.CurrentVersion.Image.RecipeGeneration, &view.Environment.CurrentVersion.Image.Reference,
		&view.Environment.CurrentVersion.Image.Digest, &view.Environment.CurrentVersion.Image.RoleRuntimeContractRevision,
		&view.Environment.CurrentVersion.Image.RoleRuntimeContractSHA256, &rawTools, &coreDigest,
		&rawResources, &rawVolumes, &rawNetwork, &rawKubernetesAccess,
		&view.Environment.CurrentVersion.Policy.ResourcesDigest, &view.Environment.CurrentVersion.Policy.VolumesDigest,
		&view.Environment.CurrentVersion.Policy.NetworkDigest, &view.Environment.CurrentVersion.Policy.RBACDigest,
		&view.Environment.CurrentVersion.Digest, &view.Environment.CurrentVersion.CreatedAt, &view.AgentVersion)
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	view.PublishedOverlay.Version = view.PublishedOverlay.Revision
	view.Environment.CurrentVersion.Version = view.Environment.CurrentVersion.Revision
	view.EnvironmentBinding.AgentRef = view.Configuration.AgentRef
	view.EnvironmentBinding.EnvironmentRef = view.Environment.Ref
	if decodeStrict(rawCandidates, &view.Configuration.ProviderPolicy.AccountCandidates) != nil ||
		decodeStrict(rawPublishedProblems, &view.PublishedOverlay.ValidationMessages) != nil ||
		decodeStrict(rawValues, &view.Environment.CurrentVersion.Values) != nil ||
		decodeStrict(rawSecrets, &view.Environment.CurrentVersion.SecretDescriptors) != nil ||
		decodeStrict(rawTools, &view.Environment.CurrentVersion.Tools) != nil {
		return entity.AgentRuntimeConfigurationView{}, errors.New("decode agent runtime configuration")
	}
	view.Environment.CurrentVersion.Policy, err = decodeRuntimeEnvironmentPolicy(rawResources, rawVolumes, rawNetwork,
		rawKubernetesAccess, view.Environment.CurrentVersion.Policy.ResourcesDigest, view.Environment.CurrentVersion.Policy.VolumesDigest,
		view.Environment.CurrentVersion.Policy.NetworkDigest, view.Environment.CurrentVersion.Policy.RBACDigest)
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	values, secrets := runtimeEnvironmentContract(view.Environment.CurrentVersion)
	if view.Environment.CurrentVersion.Image.ArtifactRef != "" {
		storedCore, storedDigest, digestErr := runtimeEnvironmentConfigurationDigests(values, secrets,
			view.Environment.CurrentVersion.Image, view.Environment.CurrentVersion.Tools, view.Environment.CurrentVersion.Policy)
		if digestErr != nil || storedCore != coreDigest || storedDigest != view.Environment.CurrentVersion.Digest {
			return entity.AgentRuntimeConfigurationView{}, errors.New("runtime environment digest mismatch")
		}
	} else if view.Environment.ProjectRef != "" {
		return entity.AgentRuntimeConfigurationView{}, errors.New("project runtime environment image is absent")
	}
	readiness := repository.runtimeEnvironmentReadiness(view.Environment)
	view.Environment.Ready = readiness.Ready
	view.Environment.ReadinessBlockers = readiness.Blockers
	view.Environment.CurrentVersion.Image.RoleRuntimeContractRevision = 0
	view.Environment.CurrentVersion.Image.RoleRuntimeContractSHA256 = ""
	if draftRef != nil {
		draft := entity.ConfigOverlayVersion{Ref: *draftRef, State: *draftState, Content: *draftContent,
			Digest: *draftDigest, Revision: *draftVersion, Version: *draftVersion, CreatedAt: *draftCreatedAt,
			PublishedAt: draftPublishedAt}
		if decodeStrict(rawDraftProblems, &draft.ValidationMessages) != nil {
			return entity.AgentRuntimeConfigurationView{}, errors.New("decode config overlay validation")
		}
		view.DraftOverlay = &draft
	}
	view.SafeEffectiveConfig, err = runtimecontract.RenderSafeEffectiveConfig(runtimecontract.SafeEffectiveConfigInput{
		Model: view.Configuration.Model, Provider: view.Configuration.Provider, RuntimeProfileRef: view.Configuration.RuntimeProfileRef,
		RuntimeConfigRef: view.Configuration.Ref, RuntimeConfigVersion: view.Configuration.Version, RuntimeConfigDigest: view.Configuration.Digest,
		ProviderPolicyRef: view.Configuration.ProviderPolicy.Ref, ProviderPolicyVersion: view.Configuration.ProviderPolicy.Version,
		ProviderPolicyMode: view.Configuration.ProviderPolicy.Mode, ProviderPolicyDigest: view.Configuration.ProviderPolicy.Digest,
		ConfigOverlayRef: view.PublishedOverlay.Ref, ConfigOverlayVersion: view.PublishedOverlay.Version, ConfigOverlayDigest: view.PublishedOverlay.Digest,
		RuntimeEnvironmentRef: view.Environment.Ref, RuntimeEnvironmentVersion: view.Environment.CurrentVersion.Version,
		RuntimeEnvironmentDigest: view.Environment.CurrentVersion.Digest, EnvironmentBindingRef: view.EnvironmentBinding.Ref,
		EnvironmentBindingVersion: view.EnvironmentBinding.Version, EnvironmentBindingDigest: view.EnvironmentBinding.Digest,
		Overlay: view.PublishedOverlay.Content, Values: values, Secrets: secrets,
	})
	if err != nil {
		return entity.AgentRuntimeConfigurationView{}, err
	}
	return view, nil
}

func (repository *Repository) scanRuntimeEnvironment(scanner rowScanner) (entity.RuntimeEnvironmentSet, error) {
	var item entity.RuntimeEnvironmentSet
	var rawValues, rawSecrets, rawTools, rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte
	var coreDigest string
	err := scanner.Scan(&item.Ref, &item.Version, &item.ProjectRef, &item.Name, &item.Description, &item.State,
		&item.UpdatedAt, &item.CurrentVersion.Ref, &item.CurrentVersion.Revision, &rawValues, &rawSecrets,
		&item.CurrentVersion.Image.ArtifactRef, &item.CurrentVersion.Image.RecipeRef,
		&item.CurrentVersion.Image.RecipeGeneration, &item.CurrentVersion.Image.Reference,
		&item.CurrentVersion.Image.Digest, &item.CurrentVersion.Image.RoleRuntimeContractRevision,
		&item.CurrentVersion.Image.RoleRuntimeContractSHA256, &rawTools,
		&coreDigest, &rawResources, &rawVolumes, &rawNetwork, &rawKubernetesAccess,
		&item.CurrentVersion.Policy.ResourcesDigest, &item.CurrentVersion.Policy.VolumesDigest,
		&item.CurrentVersion.Policy.NetworkDigest, &item.CurrentVersion.Policy.RBACDigest,
		&item.CurrentVersion.Digest, &item.CurrentVersion.CreatedAt)
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, err
	}
	item.CurrentVersion.Version = item.CurrentVersion.Revision
	if decodeStrict(rawValues, &item.CurrentVersion.Values) != nil ||
		decodeStrict(rawSecrets, &item.CurrentVersion.SecretDescriptors) != nil ||
		decodeStrict(rawTools, &item.CurrentVersion.Tools) != nil {
		return entity.RuntimeEnvironmentSet{}, errors.New("decode runtime environment")
	}
	item.CurrentVersion.Policy, err = decodeRuntimeEnvironmentPolicy(rawResources, rawVolumes, rawNetwork, rawKubernetesAccess,
		item.CurrentVersion.Policy.ResourcesDigest, item.CurrentVersion.Policy.VolumesDigest,
		item.CurrentVersion.Policy.NetworkDigest, item.CurrentVersion.Policy.RBACDigest)
	if err != nil {
		return entity.RuntimeEnvironmentSet{}, err
	}
	values, secrets := runtimeEnvironmentContract(item.CurrentVersion)
	storedCore, storedDigest, digestErr := runtimeEnvironmentConfigurationDigests(values, secrets, item.CurrentVersion.Image,
		item.CurrentVersion.Tools, item.CurrentVersion.Policy)
	if digestErr != nil || storedCore != coreDigest || storedDigest != item.CurrentVersion.Digest {
		return entity.RuntimeEnvironmentSet{}, errors.New("runtime environment digest mismatch")
	}
	readiness := repository.runtimeEnvironmentReadiness(item)
	item.Ready = readiness.Ready
	item.ReadinessBlockers = readiness.Blockers
	item.CurrentVersion.Image.RoleRuntimeContractRevision = 0
	item.CurrentVersion.Image.RoleRuntimeContractSHA256 = ""
	return item, nil
}

func scanRuntimeEnvironmentVersion(scanner rowScanner) (entity.RuntimeEnvironmentVersion, error) {
	var item entity.RuntimeEnvironmentVersion
	var rawValues, rawSecrets, rawTools, rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte
	var coreDigest string
	if err := scanner.Scan(&item.Ref, &item.Revision, &rawValues, &rawSecrets,
		&item.Image.ArtifactRef, &item.Image.RecipeRef, &item.Image.RecipeGeneration,
		&item.Image.Reference, &item.Image.Digest, &rawTools, &coreDigest,
		&rawResources, &rawVolumes, &rawNetwork, &rawKubernetesAccess,
		&item.Policy.ResourcesDigest, &item.Policy.VolumesDigest, &item.Policy.NetworkDigest, &item.Policy.RBACDigest,
		&item.Digest, &item.CreatedAt); err != nil {
		return entity.RuntimeEnvironmentVersion{}, err
	}
	item.Version = item.Revision
	if decodeStrict(rawValues, &item.Values) != nil || decodeStrict(rawSecrets, &item.SecretDescriptors) != nil ||
		decodeStrict(rawTools, &item.Tools) != nil {
		return entity.RuntimeEnvironmentVersion{}, errors.New("decode runtime environment version")
	}
	var err error
	item.Policy, err = decodeRuntimeEnvironmentPolicy(rawResources, rawVolumes, rawNetwork, rawKubernetesAccess,
		item.Policy.ResourcesDigest, item.Policy.VolumesDigest, item.Policy.NetworkDigest, item.Policy.RBACDigest)
	if err != nil {
		return entity.RuntimeEnvironmentVersion{}, err
	}
	values, secrets := runtimeEnvironmentContract(item)
	storedCore, storedDigest, digestErr := runtimeEnvironmentConfigurationDigests(values, secrets, item.Image, item.Tools, item.Policy)
	if digestErr != nil || storedCore != coreDigest || storedDigest != item.Digest {
		return entity.RuntimeEnvironmentVersion{}, errors.New("runtime environment version digest mismatch")
	}
	return item, nil
}

func decodeRuntimeEnvironmentPolicy(
	rawResources, rawVolumes, rawNetwork, rawKubernetesAccess []byte,
	resourcesDigest, volumesDigest, networkDigest, rbacDigest string,
) (runtimecontract.RuntimeEnvironmentPolicy, error) {
	policy := runtimecontract.RuntimeEnvironmentPolicy{}
	if decodeStrict(rawResources, &policy.Resources) != nil || decodeStrict(rawVolumes, &policy.Volumes) != nil ||
		decodeStrict(rawNetwork, &policy.Network) != nil || decodeStrict(rawKubernetesAccess, &policy.KubernetesAccess) != nil {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("decode runtime environment policy")
	}
	normalized, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil || normalized.ResourcesDigest != resourcesDigest || normalized.VolumesDigest != volumesDigest ||
		normalized.NetworkDigest != networkDigest || normalized.RBACDigest != rbacDigest {
		return runtimecontract.RuntimeEnvironmentPolicy{}, errors.New("runtime environment policy digest mismatch")
	}
	return normalized, nil
}

func runtimeEnvironmentContract(version entity.RuntimeEnvironmentVersion) ([]runtimecontract.RuntimeEnvironmentValue, []runtimecontract.RuntimeSecretProjection) {
	values := make([]runtimecontract.RuntimeEnvironmentValue, 0, len(version.Values))
	for _, item := range version.Values {
		values = append(values, runtimecontract.RuntimeEnvironmentValue{Name: item.Name, Value: item.Value})
	}
	secrets := make([]runtimecontract.RuntimeSecretProjection, 0, len(version.SecretDescriptors))
	for _, item := range version.SecretDescriptors {
		secrets = append(secrets, runtimecontract.RuntimeSecretProjection{Name: item.Name, SecretName: item.SecretName,
			SecretKey: item.SecretKey, SecretUID: item.SecretUID, SecretResourceVersion: item.SecretResourceVersion,
			ContentSHA256: item.ContentSHA256})
	}
	return values, secrets
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return errors.New("invalid stored JSON")
	}
	return nil
}

func versionCursor(token string) (int64, error) {
	if token == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(token, 10, 64)
	if err != nil || value < 1 {
		return 0, errors.New("invalid version cursor")
	}
	return value, nil
}
