package platform

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// assistantEnvironmentTextFields lists the three trimmed textual environment edits used for change
// detection: name, description and image-artifact ref. Other form values use their specialized
// typed parsers; this list grants no image or environment permission.
// assistantEnvironmentTextFields перечисляет три текстовых изменения environment с обрезкой
// пробелов для определения изменений: name, description и image-artifact ref. Остальные значения
// формы используют специализированные typed parsers; список не выдаёт image или environment
// permission.
var assistantEnvironmentTextFields = []string{"name", "description", "imageArtifactRef"}

// assistantEnvironmentEditableFields defines the closed edited-plan values copied onto a pinned
// environment snapshot, including public values, secret-binding descriptors, tools and policy.
// Environment/project/SYSTEM identities are checked and seeded separately; descriptors reference
// secret revisions without containing their payload bytes.
// assistantEnvironmentEditableFields задаёт закрытые edited-plan values для копирования поверх
// закреплённого environment snapshot, включая public values, secret-binding descriptors, tools и
// policy. Environment/project/SYSTEM identities проверяются и подготавливаются отдельно;
// descriptors ссылаются на secret revisions без байтов их payload.
var assistantEnvironmentEditableFields = []string{"name", "description", "imageArtifactRef", "publicValues", "secretBindings", "tools", "policy"}

// assistantEnvironmentProposalFields extends the initial environment proposal shape with
// public-value patch/removal operations and a SYSTEM assistant locator. Full replacement versus
// patch/removal is validated later and is not an implicit edit of project identity or a secret
// value.
// assistantEnvironmentProposalFields расширяет форму исходного environment proposal операциями
// patch/removal public values и locator SYSTEM assistant. Полная замена против patch/removal
// проверяется далее и не является неявным изменением project identity или значения секрета.
var assistantEnvironmentProposalFields = []string{
	"name", "description", "imageArtifactRef", "publicValues", "publicValueUpdates", "publicValueRemovals",
	"secretBindings", "tools", "policy", "systemAssistantRef",
}

// readAssistantEnvironmentSnapshot uses the project ENVIRONMENT context and exact prepare
// permission, or resolves the current SYSTEM assistant and its exact organization-scoped bound
// environment. It rejects wrong project/deleted/missing-version state, builds safe revisioned
// secret-binding descriptors and JSON-round-trips the public draft specification. The snapshot
// carries environment/version/image/policy identity and a canonical SYSTEM ref when applicable. It
// uses the caller transaction and reads no secret payload; SYSTEM lookup/configuration mismatches
// remain Forbidden.
// readAssistantEnvironmentSnapshot использует project ENVIRONMENT context с точным prepare
// permission либо разрешает текущий SYSTEM assistant и его точное связанное organization-scoped
// environment. Отклоняет чужой project, удалённое состояние и отсутствие version, строит
// безопасные revisioned secret-binding descriptors и выполняет JSON round-trip public draft
// specification. Snapshot несёт environment/version/image/policy identity и канонический SYSTEM
// ref при необходимости. Использует транзакцию вызывающего без чтения secret payload; несовпадения
// SYSTEM lookup/configuration остаются Forbidden.
func (repository *Repository) readAssistantEnvironmentSnapshot(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef, environmentRef, systemAssistantRef string,
) (map[string]any, int64, error) {
	if systemAssistantRef == "" {
		projection, err := repository.resolveAssistantContext(ctx, tx, actorScope,
			entity.AssistantContextDescriptor{EntityKind: "ENVIRONMENT", EntityRef: environmentRef}, projectRef)
		if err != nil {
			return nil, 0, err
		}
		if !contains(projection.AllowedOperations, "PREPARE_RUNTIME_ENVIRONMENT_REVISION") {
			return nil, 0, errs.ErrForbidden
		}
	} else {
		assistant, err := repository.getAssistantTx(ctx, tx, actorScope)
		if err != nil || systemAssistantRef != assistant.Ref && systemAssistantRef != assistant.StableKey {
			return nil, 0, errs.ErrForbidden
		}
		view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, actorScope, assistant.Ref)
		if err != nil || view.Environment.Ref != environmentRef || view.Environment.ProjectRef != "" {
			return nil, 0, errs.ErrForbidden
		}
		systemAssistantRef = assistant.Ref
		projectRef = ""
	}
	environment, err := repository.getRuntimeEnvironmentTx(ctx, tx, actorScope, environmentRef)
	if err != nil {
		return nil, 0, err
	}
	if environment.ProjectRef != projectRef || environment.State == "DELETED" ||
		environment.CurrentVersion.Ref == "" || environment.CurrentVersion.Revision < 1 {
		return nil, 0, errs.ErrNotFound
	}
	bindings := make([]entity.RuntimeSecretBinding, 0, len(environment.CurrentVersion.SecretDescriptors))
	for _, descriptor := range environment.CurrentVersion.SecretDescriptors {
		bindings = append(bindings, entity.RuntimeSecretBinding{
			Name: descriptor.Name, SecretRef: descriptor.SecretRef, Revision: descriptor.Revision,
		})
	}
	specification := entity.RuntimeEnvironmentDraftSpecification{
		Name: environment.Name, Description: environment.Description,
		ImageArtifactRef: environment.CurrentVersion.Image.ArtifactRef,
		Values:           environment.CurrentVersion.Values, SecretBindings: bindings,
		Tools: environment.CurrentVersion.Tools, Policy: environment.CurrentVersion.Policy,
	}
	raw, err := json.Marshal(specification)
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	var safeSpecification map[string]any
	if json.Unmarshal(raw, &safeSpecification) != nil {
		return nil, 0, errs.ErrUnavailable
	}
	snapshot := map[string]any{
		"environmentRef": environment.Ref, "projectRef": environment.ProjectRef,
		"name": environment.Name, "description": environment.Description,
		"imageArtifactRef": environment.CurrentVersion.Image.ArtifactRef,
		"versionRef":       environment.CurrentVersion.Ref, "versionDigest": environment.CurrentVersion.Digest,
		"specification": safeSpecification,
		"policyInput":   assistantEnvironmentPolicyInput(environment.CurrentVersion.Policy),
	}
	if systemAssistantRef != "" {
		snapshot["systemAssistantRef"] = systemAssistantRef
	}
	return snapshot, environment.Version, nil
}

// assistantEnvironmentPolicyInput projects resources, optional workspace limits, volumes, web
// rules and Kubernetes access kind into fresh form maps. HTTP methods are copied and core network
// destinations are the fixed DNS/provider-proxy/runtime-callback set. This compact editor input is
// not a serialization of every policy field and does not validate, admit or grant
// network/Kubernetes access.
// assistantEnvironmentPolicyInput проецирует resources, optional workspace limits, volumes, web
// rules и Kubernetes access kind в новые maps формы. HTTP methods копируются, core network
// destinations заданы фиксированным набором DNS/provider-proxy/runtime-callback. Этот компактный
// editor input не сериализует каждое поле policy и не валидирует, не допускает и не выдаёт
// network/Kubernetes access.
func assistantEnvironmentPolicyInput(policy runtimecontract.RuntimeEnvironmentPolicy) map[string]any {
	resources := policy.Resources
	resourceInput := map[string]any{
		"cpuRequestMilli": resources.CPURequestMilli, "cpuLimitMilli": resources.CPULimitMilli,
		"memoryRequestMib": resources.MemoryRequestMiB, "memoryLimitMib": resources.MemoryLimitMiB,
		"ephemeralStorageRequestMib": resources.EphemeralStorageRequestMiB,
		"ephemeralStorageLimitMib":   resources.EphemeralStorageLimitMiB,
	}
	if resources.WorkspaceLimits != nil {
		resourceInput["workspaceLimits"] = map[string]any{"maxBytes": resources.WorkspaceLimits.MaxBytes, "maxFiles": resources.WorkspaceLimits.MaxFiles}
	}
	volumes := make([]map[string]any, 0, len(policy.Volumes))
	for _, volume := range policy.Volumes {
		volumes = append(volumes, map[string]any{"name": volume.Name, "kind": volume.Kind, "sizeMib": volume.SizeMiB})
	}
	destinations := []string{runtimecontract.RuntimeEgressDNS, runtimecontract.RuntimeEgressProviderProxy, runtimecontract.RuntimeEgressRuntimeCallback}
	webRules := make([]map[string]any, 0, len(policy.Network.WebAccess.Rules))
	for _, rule := range policy.Network.WebAccess.Rules {
		webRules = append(webRules, map[string]any{
			"domainPattern": rule.DomainPattern,
			"protocol":      rule.Protocol,
			"port":          rule.Port,
			"httpMethods":   append([]string(nil), rule.HTTPMethods...),
		})
	}
	if webRules == nil {
		webRules = []map[string]any{}
	}
	return map[string]any{
		"resources": resourceInput,
		"volumes":   volumes, "networkDestinations": destinations,
		"webAccess": map[string]any{
			"mode": policy.Network.WebAccess.Mode, "rules": webRules,
		},
		"kubernetesAccess": policy.KubernetesAccess.Kind,
	}
}

// hydrateAssistantEnvironmentOperation accepts the closed environment proposal shape, requires
// environmentRef and either project or SYSTEM locator, then reads the corresponding authoritative
// snapshot/version in the supplied transaction. It delegates typed pure hydration; a caller
// locator does not by itself authorize an organization or project environment.
// hydrateAssistantEnvironmentOperation принимает закрытую форму environment proposal, требует
// environmentRef и project либо SYSTEM locator, затем читает соответствующие авторитетные
// snapshot/version в переданной транзакции. Делегирует чистую typed hydration; locator вызывающего
// сам не авторизует organization или project environment.
func (repository *Repository) hydrateAssistantEnvironmentOperation(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	if !onlyAssistantFields(operation.Parameters,
		append([]string{"environmentRef"}, assistantEnvironmentProposalFields...)...) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	ref := assistantString(operation.Parameters, "environmentRef")
	if ref == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	systemAssistantRef := assistantString(operation.Parameters, "systemAssistantRef")
	if systemAssistantRef == "" && projectRef == "" {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	before, version, err := repository.readAssistantEnvironmentSnapshot(ctx, tx, actorScope, projectRef, ref, systemAssistantRef)
	if err != nil {
		return entity.AssistantPlanOperation{}, err
	}
	return hydrateAssistantEnvironmentFields(before, version, operation)
}

// hydrateAssistantEnvironmentFields builds a fresh pinned ref/name/image map, validates mutually
// exclusive public-value replacement versus patch/removal and checks resulting values against
// secret bindings. Typed tools/policy changes use their existing parsers and comparison rules.
// Unchanged or invalid byte-bounded name/description state is Conflict; malformed image ref is
// Invalid. On success it restores Before/target/version, selects the operation and clears Input.
// It neither publishes an environment nor reads secret payloads or admits the referenced image.
// hydrateAssistantEnvironmentFields строит новую map с закреплёнными ref/name/image, проверяет
// несовместимость полной замены public values с patch/removal и совместимость итоговых values с
// secret bindings. Typed tools/policy changes используют прежние parsers и comparison rules.
// Неизменённое либо недопустимое по byte bounds состояние name/description даёт Conflict; неверный
// image ref даёт Invalid. При успехе восстанавливает Before/target/version, выбирает operation и
// очищает Input. Environment не публикует, secret payloads не читает и referenced image не
// допускает.
func hydrateAssistantEnvironmentFields(before map[string]any, version int64,
	operation entity.AssistantPlanOperation,
) (entity.AssistantPlanOperation, error) {
	after := map[string]any{
		"environmentRef": before["environmentRef"], "projectRef": before["projectRef"],
		"name": before["name"], "description": before["description"],
		"imageArtifactRef": before["imageArtifactRef"],
	}
	if systemAssistantRef := assistantString(before, "systemAssistantRef"); systemAssistantRef != "" {
		after["systemAssistantRef"] = systemAssistantRef
	}
	changed := false
	for _, field := range assistantEnvironmentTextFields {
		value, supplied := operation.Parameters[field]
		if !supplied {
			continue
		}
		text, ok := value.(string)
		if !ok {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		text = strings.TrimSpace(text)
		changed = changed || after[field] != text
		after[field] = text
	}
	_, valuesSupplied := operation.Parameters["publicValues"]
	_, updatesSupplied := operation.Parameters["publicValueUpdates"]
	_, removalsSupplied := operation.Parameters["publicValueRemovals"]
	if valuesSupplied && (updatesSupplied || removalsSupplied) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	var effectiveValues []entity.RuntimeEnvironmentValue
	valuesChanged := valuesSupplied || updatesSupplied || removalsSupplied
	if valuesChanged {
		var valid bool
		if valuesSupplied {
			effectiveValues, valid = assistantEnvironmentPublicValues(operation.Parameters)
		} else {
			effectiveValues, valid = assistantEnvironmentPatchedPublicValues(before["specification"], operation.Parameters)
		}
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["publicValues"] = assistantEnvironmentPublicValuesInput(effectiveValues)
		changed = changed || !assistantEnvironmentValuesMatch(before["specification"], effectiveValues)
	}
	if _, bindingsSupplied := operation.Parameters["secretBindings"]; bindingsSupplied {
		values := effectiveValues
		if !valuesChanged {
			var valid bool
			values, valid = assistantEnvironmentValuesForUpdate(before["specification"], operation.Parameters)
			if !valid {
				return entity.AssistantPlanOperation{}, errs.ErrInvalid
			}
		}
		bindings, valid := assistantEnvironmentSecretBindings(operation.Parameters, values)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["secretBindings"] = operation.Parameters["secretBindings"]
		changed = changed || !assistantEnvironmentBindingsMatch(before["specification"], bindings)
	}
	if _, toolsSupplied := operation.Parameters["tools"]; toolsSupplied {
		tools, valid := assistantEnvironmentTools(operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["tools"] = operation.Parameters["tools"]
		changed = changed || !assistantEnvironmentToolsMatch(before["specification"], tools)
	}
	if _, policySupplied := operation.Parameters["policy"]; policySupplied {
		policy, valid := assistantEnvironmentPolicy(operation.Parameters)
		if !valid {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
		after["policy"] = operation.Parameters["policy"]
		changed = changed || !assistantEnvironmentPolicyMatch(before["specification"], policy)
	}
	if valuesChanged {
		if _, bindingsSupplied := operation.Parameters["secretBindings"]; !bindingsSupplied &&
			!assistantEnvironmentSnapshotBindingsCompatible(before["specification"], effectiveValues) {
			return entity.AssistantPlanOperation{}, errs.ErrInvalid
		}
	}
	if !changed || assistantString(after, "name") == "" || len(assistantString(after, "name")) > 120 ||
		len(assistantString(after, "description")) > 1000 {
		return entity.AssistantPlanOperation{}, errs.ErrConflict
	}
	imageRef := assistantString(after, "imageArtifactRef")
	if imageRef != "" && (!strings.HasPrefix(imageRef, "imgart_") || len(imageRef) > 96) {
		return entity.AssistantPlanOperation{}, errs.ErrInvalid
	}
	operation.Action = "UPDATE"
	operation.Target = entity.AssistantPlanTarget{Kind: "ENVIRONMENT", Ref: assistantString(before, "environmentRef"),
		Name: assistantString(before, "name"), Version: &version}
	operation.Before = cloneAssistantFields(before)
	operation.After = cloneAssistantFields(after)
	operation.Parameters = after
	operation.ExpectedVersion = &version
	operation.Selected = true
	operation.Input = nil
	return operation, nil
}

// rehydrateEditedAssistantEnvironment requires original environment kind/type/key/ref/version and
// unchanged project/SYSTEM identities in the closed edited shape. It explicitly seeds the original
// environment ref and optional stored SYSTEM ref before calling the shared field-copy/hydration
// helper. Only supplied editable values are forwarded; original Before/version and edited
// selection survive, without broadening project or organization authority.
// rehydrateEditedAssistantEnvironment требует исходные environment kind/type/key/ref/version и
// неизменённые project/SYSTEM identities в закрытой edited shape. Явно подготавливает исходный
// environment ref и optional сохранённый SYSTEM ref до общего helper копирования полей/hydration.
// Передаются только заданные editable values; исходные Before/version и edited selection
// сохраняются без расширения project или organization authority.
func rehydrateEditedAssistantEnvironment(original, edited entity.AssistantPlanOperation) (entity.AssistantPlanOperation, error) {
	if original.Type != "PREPARE_RUNTIME_ENVIRONMENT_REVISION" || original.Key != edited.Key ||
		original.Target.Kind != "ENVIRONMENT" || original.Target.Ref == "" ||
		original.ExpectedVersion == nil || *original.ExpectedVersion < 1 || edited.Parameters == nil ||
		!onlyAssistantFields(edited.Parameters, append([]string{"environmentRef", "projectRef", "systemAssistantRef"}, assistantEnvironmentEditableFields...)...) ||
		assistantString(edited.Parameters, "environmentRef") != original.Target.Ref ||
		assistantString(edited.Parameters, "projectRef") != assistantString(original.Before, "projectRef") ||
		assistantString(edited.Parameters, "systemAssistantRef") != assistantString(original.Before, "systemAssistantRef") {
		return entity.AssistantPlanOperation{}, errs.ErrForbidden
	}
	parameters := map[string]any{"environmentRef": original.Target.Ref}
	if systemAssistantRef := assistantString(original.Before, "systemAssistantRef"); systemAssistantRef != "" {
		parameters["systemAssistantRef"] = systemAssistantRef
	}
	return rehydrateAssistantEditableFields(original, edited, parameters, assistantEnvironmentEditableFields, hydrateAssistantEnvironmentFields)
}

// assistantEnvironmentSnapshotMatches delegates project-assistant configuration to its specialized
// checker; otherwise it re-reads project or SYSTEM environment state and compares both version
// pins, target/name, snapshot identity, Parameters/After and scope refs. SYSTEM mode forces an
// empty effective project. Read errors propagate and mismatch is false; this check does not create
// an environment revision, grant or external runtime.
// assistantEnvironmentSnapshotMatches делегирует project-assistant configuration
// специализированному checker; иначе повторно читает project или SYSTEM environment state и
// сравнивает обе version pins, target/name, snapshot identity, Parameters/After и scope refs.
// SYSTEM mode требует пустой effective project. Ошибки чтения передаются выше, несовпадение даёт
// false; проверка не создаёт environment revision, grant или внешний runtime.
func (repository *Repository) assistantEnvironmentSnapshotMatches(ctx context.Context, tx pgx.Tx, actorScope scope,
	projectRef string, operation entity.AssistantPlanOperation,
) (bool, error) {
	if assistantProjectConfigurationOperation(operation) {
		return repository.projectAssistantConfigurationSnapshotMatches(ctx, tx, actorScope, operation)
	}
	systemAssistantRef := assistantString(operation.Parameters, "systemAssistantRef")
	effectiveProjectRef := projectRef
	if systemAssistantRef != "" {
		effectiveProjectRef = ""
	}
	before, version, err := repository.readAssistantEnvironmentSnapshot(ctx, tx, actorScope, effectiveProjectRef, operation.Target.Ref, systemAssistantRef)
	if err != nil {
		return false, err
	}
	return operation.ExpectedVersion != nil && *operation.ExpectedVersion == version &&
		operation.Target.Version != nil && *operation.Target.Version == version &&
		operation.Target.Name == assistantString(before, "name") &&
		assistantEnvironmentSnapshotIdentityMatches(operation.Before, before) &&
		reflect.DeepEqual(operation.Parameters, operation.After) &&
		assistantString(operation.Parameters, "environmentRef") == operation.Target.Ref &&
		assistantString(operation.Parameters, "projectRef") == effectiveProjectRef &&
		assistantString(operation.Parameters, "systemAssistantRef") == systemAssistantRef, nil
}

// assistantEnvironmentSnapshotIdentityMatches rejects unknown snapshot keys, compares the exact
// textual environment/project/name/description/image/version/SYSTEM identity tuple and requires a
// nonempty current version ref/digest. It deliberately does not deeply compare specification or
// policyInput; their surrounding version/digest and command checks remain separate, so this
// predicate alone is not content validation or authority.
// assistantEnvironmentSnapshotIdentityMatches отклоняет неизвестные snapshot keys, сравнивает
// точный текстовый environment/project/name/description/image/version/SYSTEM identity tuple и
// требует непустые текущие version ref/digest. Намеренно не сравнивает specification или
// policyInput глубоко; окружающие version/digest и command checks остаются отдельными, поэтому
// predicate сам не валидирует content и не даёт полномочий.
func assistantEnvironmentSnapshotIdentityMatches(stored, current map[string]any) bool {
	if !onlyAssistantFields(stored, "environmentRef", "projectRef", "name", "description", "imageArtifactRef",
		"versionRef", "versionDigest", "specification", "policyInput", "systemAssistantRef") ||
		!onlyAssistantFields(current, "environmentRef", "projectRef", "name", "description", "imageArtifactRef",
			"versionRef", "versionDigest", "specification", "policyInput", "systemAssistantRef") {
		return false
	}
	for _, field := range []string{"environmentRef", "projectRef", "name", "description", "imageArtifactRef", "versionRef", "versionDigest", "systemAssistantRef"} {
		if assistantString(stored, field) != assistantString(current, field) {
			return false
		}
	}
	return assistantString(current, "versionRef") != "" && assistantString(current, "versionDigest") != ""
}

// assistantEnvironmentRevisionCommand requires a closed typed Input with refs matching Before and
// a positive version, reconstructs the saved draft specification and overlays validated public
// values, binding descriptors, tools and policy. It checks binding compatibility and returns the
// specialized project draft command, or an organization draft command only with SYSTEM ref and
// empty project. It prepares a command without applying it, authorizing a scope or copying secret
// payload bytes.
// assistantEnvironmentRevisionCommand требует закрытый typed Input с refs, совпадающими с Before,
// и положительной version, восстанавливает сохранённую draft specification и накладывает
// допустимые public values, binding descriptors, tools и policy. Проверяет совместимость bindings
// и возвращает специализированную project draft command либо organization draft command только с
// SYSTEM ref и пустым project. Подготавливает команду без применения, авторизации scope или
// копирования байтов secret payload.
func assistantEnvironmentRevisionCommand(operation entity.AssistantPlanOperation) (command.Command, error) {
	input := operation.Input
	if !onlyAssistantFields(input, "environmentRef", "projectRef", "name", "description", "imageArtifactRef", "publicValues", "secretBindings", "tools", "policy", "systemAssistantRef", "expectedVersion") ||
		!hasAssistantFields(input, "environmentRef", "projectRef", "name", "description", "imageArtifactRef", "expectedVersion") ||
		assistantString(input, "environmentRef") != assistantString(operation.Before, "environmentRef") ||
		assistantString(input, "projectRef") != assistantString(operation.Before, "projectRef") {
		return command.Command{}, errs.ErrInvalid
	}
	version, ok := assistantInt64(input, "expectedVersion")
	if !ok || version < 1 || assistantString(input, "name") == "" ||
		len(assistantString(input, "name")) > 120 || len(assistantString(input, "description")) > 1000 {
		return command.Command{}, errs.ErrInvalid
	}
	imageRef := assistantString(input, "imageArtifactRef")
	if imageRef != "" && (!strings.HasPrefix(imageRef, "imgart_") || len(imageRef) > 96) {
		return command.Command{}, errs.ErrInvalid
	}
	raw, err := json.Marshal(operation.Before["specification"])
	if err != nil {
		return command.Command{}, errs.ErrInvalid
	}
	var specification entity.RuntimeEnvironmentDraftSpecification
	if json.Unmarshal(raw, &specification) != nil {
		return command.Command{}, errs.ErrInvalid
	}
	specification.Name = assistantString(input, "name")
	specification.Description = assistantString(input, "description")
	specification.ImageArtifactRef = imageRef
	if _, supplied := input["publicValues"]; supplied {
		values, valid := assistantEnvironmentPublicValues(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Values = values
	}
	if _, supplied := input["secretBindings"]; supplied {
		bindings, valid := assistantEnvironmentSecretBindings(input, specification.Values)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.SecretBindings = bindings
	}
	if _, supplied := input["tools"]; supplied {
		tools, valid := assistantEnvironmentTools(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Tools = tools
	}
	if _, supplied := input["policy"]; supplied {
		policy, valid := assistantEnvironmentPolicy(input)
		if !valid {
			return command.Command{}, errs.ErrInvalid
		}
		specification.Policy = policy
	}
	if !assistantEnvironmentBindingsCompatible(specification.Values, specification.SecretBindings) {
		return command.Command{}, errs.ErrInvalid
	}
	if assistantString(input, "systemAssistantRef") != "" {
		if assistantString(input, "projectRef") != "" {
			return command.Command{}, errs.ErrInvalid
		}
		return command.Command{Kind: command.CreateOrganizationRuntimeEnvironmentDraft, Payload: command.RuntimeEnvironmentDraftInput{
			ScopeKind: "ORGANIZATION", EnvironmentRef: assistantString(input, "environmentRef"), ExpectedEnvironmentVersion: version, Specification: specification,
		}}, nil
	}
	return command.Command{Kind: command.CreateRuntimeEnvironmentDraft, Payload: command.RuntimeEnvironmentDraftInput{
		ScopeKind:  "PROJECT",
		ProjectRef: assistantString(input, "projectRef"), EnvironmentRef: assistantString(input, "environmentRef"),
		ExpectedEnvironmentVersion: version, Specification: specification,
	}}, nil
}
