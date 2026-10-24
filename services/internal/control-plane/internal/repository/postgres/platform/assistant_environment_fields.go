package platform

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// assistantEnvironmentTools Parses an optional tools list of at most 128 closed-shape records,
// validating bounded trimmed UTF-8/NUL-free labels, a restricted command token and unique commands
// while preserving order. Omitted tools return nil; an explicit empty list remains allocated. This
// parser does not prove that commands exist in an image.
// assistantEnvironmentTools Разбирает необязательный список до 128 инструментов с закрытым набором
// полей. Проверяет длины, пробелы, UTF-8 и отсутствие NUL, ограниченный токен команды и
// уникальность команд, сохраняя порядок. Отсутствие списка даёт nil, явный пустой список остаётся
// созданным. Наличие команды в образе не проверяет.
func assistantEnvironmentTools(input map[string]any) ([]entity.RuntimeEnvironmentTool, bool) {
	raw, supplied := input["tools"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	tools := make([]entity.RuntimeEnvironmentTool, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "command", "description", "usageHint") ||
			!hasAssistantFields(item, "name", "command", "description") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		command, commandOK := item["command"].(string)
		description, descriptionOK := item["description"].(string)
		usageHint, _ := item["usageHint"].(string)
		if _, supplied := item["usageHint"]; supplied {
			if _, valid := item["usageHint"].(string); !valid {
				return nil, false
			}
		}
		if !nameOK || !commandOK || !descriptionOK || name == "" || name != strings.TrimSpace(name) || len(name) > 160 ||
			command == "" || command != strings.TrimSpace(command) || len(command) > 160 ||
			description == "" || description != strings.TrimSpace(description) || len(description) > 500 || len(usageHint) > 500 ||
			!utf8.ValidString(name+command+description+usageHint) || strings.ContainsRune(name+command+description+usageHint, 0) {
			return nil, false
		}
		for index, character := range command {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || index > 0 && strings.ContainsRune("._+-", character) {
				continue
			}
			return nil, false
		}
		if _, duplicate := seen[command]; duplicate {
			return nil, false
		}
		seen[command] = struct{}{}
		tools = append(tools, entity.RuntimeEnvironmentTool{Name: name, Command: command, Description: description, UsageHint: usageHint})
	}
	return tools, true
}

// assistantEnvironmentToolsMatch Decodes the stored draft specification and compares tool records
// deeply, treating nil and empty tools as equivalent. Other fields and image command availability
// are outside this equality check.
// assistantEnvironmentToolsMatch Декодирует сохранённую спецификацию черновика и глубоко
// сравнивает инструменты, считая nil и пустой список равными. Остальные поля и наличие команд в
// образе не проверяет.
func assistantEnvironmentToolsMatch(snapshot any, tools []entity.RuntimeEnvironmentTool) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && (len(specification.Tools) == 0 && len(tools) == 0 || reflect.DeepEqual(specification.Tools, tools))
}

// assistantEnvironmentPolicy Parses a closed-shape optional runtime policy with numeric
// resources/workspace limits, bounded volumes/network destinations and optional web rules, then
// delegates normalization/validation to runtimecontract. Omitted policy returns a zero value; no
// Kubernetes resources or authority are created.
// assistantEnvironmentPolicy Разбирает необязательную policy с закрытым набором полей: ресурсы и
// пределы workspace, ограниченные тома, сетевые назначения и правила web-доступа. Нормализацию и
// проверку передаёт runtimecontract. Отсутствие policy даёт нулевое значение; ресурсы Kubernetes и
// полномочия не создаются.
func assistantEnvironmentPolicy(input map[string]any) (runtimecontract.RuntimeEnvironmentPolicy, bool) {
	raw, supplied := input["policy"]
	if !supplied {
		return runtimecontract.RuntimeEnvironmentPolicy{}, true
	}
	item, ok := raw.(map[string]any)
	if !ok || !onlyAssistantFields(item, "resources", "volumes", "networkDestinations", "webAccess", "kubernetesAccess") ||
		!hasAssistantFields(item, "resources", "volumes", "networkDestinations", "kubernetesAccess") {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	resources, ok := item["resources"].(map[string]any)
	fields := []string{"cpuRequestMilli", "cpuLimitMilli", "memoryRequestMib", "memoryLimitMib", "ephemeralStorageRequestMib", "ephemeralStorageLimitMib"}
	if !ok || !onlyAssistantFields(resources, append(append([]string{}, fields...), "workspaceLimits")...) || !hasAssistantFields(resources, fields...) {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	var workspaceLimits *runtimecontract.RuntimeWorkspaceLimits
	if rawLimits, supplied := resources["workspaceLimits"]; supplied {
		limits, valid := rawLimits.(map[string]any)
		if !valid || !onlyAssistantFields(limits, "maxBytes", "maxFiles") || !hasAssistantFields(limits, "maxBytes", "maxFiles") {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		maxBytes, bytesOK := assistantInt64(limits, "maxBytes")
		maxFiles, filesOK := assistantInt64(limits, "maxFiles")
		workspaceLimits = &runtimecontract.RuntimeWorkspaceLimits{MaxBytes: maxBytes, MaxFiles: maxFiles}
		if !bytesOK || !filesOK || workspaceLimits.Validate() != nil {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
	}
	numbers := make([]int64, len(fields))
	for index, field := range fields {
		value, valid := assistantInt64(resources, field)
		if !valid {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		numbers[index] = value
	}
	volumesRaw, ok := item["volumes"].([]any)
	if !ok || len(volumesRaw) > 16 {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	volumes := make([]runtimecontract.RuntimeVolume, 0, len(volumesRaw))
	for _, rawVolume := range volumesRaw {
		volume, valid := rawVolume.(map[string]any)
		if !valid || !onlyAssistantFields(volume, "name", "kind", "sizeMib") ||
			!hasAssistantFields(volume, "name", "kind", "sizeMib") {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		name, nameOK := volume["name"].(string)
		kind, kindOK := volume["kind"].(string)
		size, sizeOK := assistantInt64(volume, "sizeMib")
		if !nameOK || !kindOK || !sizeOK {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		volumes = append(volumes, runtimecontract.RuntimeVolume{Name: name, Kind: kind, SizeMiB: size})
	}
	destinationsRaw, ok := item["networkDestinations"].([]any)
	if !ok || len(destinationsRaw) > 4 {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	destinations := make([]string, 0, len(destinationsRaw))
	for _, rawDestination := range destinationsRaw {
		destination, valid := rawDestination.(string)
		if !valid {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		destinations = append(destinations, destination)
	}
	access, ok := item["kubernetesAccess"].(string)
	if !ok {
		return runtimecontract.RuntimeEnvironmentPolicy{}, false
	}
	webMode := runtimecontract.RuntimeWebAccessNone
	webRulesRaw := []any{}
	if rawWebAccess, supplied := item["webAccess"]; supplied {
		webAccess, valid := rawWebAccess.(map[string]any)
		if !valid || !onlyAssistantFields(webAccess, "mode", "rules") || !hasAssistantFields(webAccess, "mode", "rules") {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		var modeOK, rulesOK bool
		webMode, modeOK = webAccess["mode"].(string)
		webRulesRaw, rulesOK = webAccess["rules"].([]any)
		if !modeOK || !rulesOK || len(webRulesRaw) > 64 {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
	}
	webRules := make([]runtimecontract.RuntimeWebAccessRule, 0, len(webRulesRaw))
	for _, rawRule := range webRulesRaw {
		rule, valid := rawRule.(map[string]any)
		if !valid || !onlyAssistantFields(rule, "domainPattern", "protocol", "port", "httpMethods") ||
			!hasAssistantFields(rule, "domainPattern", "protocol", "port", "httpMethods") {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		domain, domainOK := rule["domainPattern"].(string)
		protocol, protocolOK := rule["protocol"].(string)
		port, portOK := assistantInt64(rule, "port")
		if !domainOK || !protocolOK || !portOK || port < 1 || port > 65535 {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		methodsRaw, methodsOK := rule["httpMethods"].([]any)
		if !methodsOK || len(methodsRaw) == 0 || len(methodsRaw) > 7 {
			return runtimecontract.RuntimeEnvironmentPolicy{}, false
		}
		methods := make([]string, 0, len(methodsRaw))
		for _, rawMethod := range methodsRaw {
			method, valid := rawMethod.(string)
			if !valid || !contains([]string{"DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"}, method) {
				return runtimecontract.RuntimeEnvironmentPolicy{}, false
			}
			methods = append(methods, method)
		}
		webRules = append(webRules, runtimecontract.RuntimeWebAccessRule{
			DomainPattern: domain, Protocol: protocol, Port: int32(port), HTTPMethods: methods,
		})
	}
	policy, err := runtimecontract.RuntimeEnvironmentPolicyFromInput(runtimecontract.RuntimeEnvironmentPolicyInput{
		Resources: runtimecontract.RuntimeResourcePolicy{
			WorkspaceLimits: workspaceLimits,
			CPURequestMilli: numbers[0], CPULimitMilli: numbers[1], MemoryRequestMiB: numbers[2], MemoryLimitMiB: numbers[3],
			EphemeralStorageRequestMiB: numbers[4], EphemeralStorageLimitMiB: numbers[5],
		},
		Volumes: volumes, NetworkDestinations: destinations,
		WebAccess: runtimecontract.RuntimeWebAccess{Mode: webMode, Rules: webRules}, KubernetesAccess: access,
	})
	return policy, err == nil
}

// assistantEnvironmentPolicyMatch Decodes the snapshot, normalizes its policy through
// runtimecontract and deeply compares it with the already prepared policy. Decode/normalization
// failure is a mismatch; no policy is applied.
// assistantEnvironmentPolicyMatch Декодирует снимок, нормализует его policy через runtimecontract
// и глубоко сравнивает с уже подготовленной policy. Ошибка декодирования или нормализации означает
// несовпадение; применение не выполняется.
func assistantEnvironmentPolicyMatch(snapshot any, policy runtimecontract.RuntimeEnvironmentPolicy) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return false
	}
	current, err := runtimecontract.NormalizeRuntimeEnvironmentPolicy(specification.Policy)
	return err == nil && reflect.DeepEqual(current, policy)
}

// assistantEnvironmentSecretSuggestions Validates up to eight optional unique secret suggestions
// with closed metadata fields, supported value type and bounded nonblank source help. Suggestions
// contain no value field and neither create secrets nor resolve their authority.
// assistantEnvironmentSecretSuggestions Проверяет до восьми необязательных уникальных предложений
// секретов с закрытыми полями метаданных, поддерживаемым типом и ограниченной непустой подсказкой
// источника. Поля значения нет; секреты не создаются, полномочия не разрешаются.
func assistantEnvironmentSecretSuggestions(input map[string]any) bool {
	raw, supplied := input["secretSuggestions"]
	if !supplied {
		return true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 8 {
		return false
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "description", "valueType", "sourceHelp") ||
			!hasAssistantFields(item, "name", "valueType", "sourceHelp") {
			return false
		}
		name, nameOK := item["name"].(string)
		valueType, typeOK := item["valueType"].(string)
		sourceHelp, helpOK := item["sourceHelp"].(string)
		if !nameOK || !typeOK || !helpOK || name != strings.TrimSpace(name) || len(name) < 1 || len(name) > 120 ||
			!contains([]string{"STRING", "JSON", "BINARY"}, valueType) || len(strings.TrimSpace(sourceHelp)) < 1 || len(sourceHelp) > 1000 {
			return false
		}
		if description, hasDescription := item["description"]; hasDescription {
			value, valid := description.(string)
			if !valid || len(value) > 1000 {
				return false
			}
		}
		if _, duplicate := seen[name]; duplicate {
			return false
		}
		seen[name] = struct{}{}
	}
	return true
}

// assistantEnvironmentSnapshotSpecification Converts an arbitrary snapshot through JSON
// marshal/unmarshal into the typed draft specification, returning false on either error. This
// compatibility decoder does not reject unknown JSON fields or independently validate
// ownership/policy.
// assistantEnvironmentSnapshotSpecification Преобразует произвольный снимок через JSON
// marshal/unmarshal в типизированную спецификацию черновика; ошибка любого этапа даёт false.
// Декодер совместимости не отклоняет неизвестные поля JSON и самостоятельно не проверяет владение
// или policy.
func assistantEnvironmentSnapshotSpecification(raw any) (entity.RuntimeEnvironmentDraftSpecification, bool) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return entity.RuntimeEnvironmentDraftSpecification{}, false
	}
	var specification entity.RuntimeEnvironmentDraftSpecification
	if json.Unmarshal(encoded, &specification) != nil {
		return entity.RuntimeEnvironmentDraftSpecification{}, false
	}
	return specification, true
}

// assistantEnvironmentValuesForUpdate Decodes the existing specification, parses a supplied
// publicValues replacement or returns its previous values when omitted. This distinguishes
// omission from explicit empty replacement and performs no persistence or secret lookup.
// assistantEnvironmentValuesForUpdate Декодирует прежнюю спецификацию, разбирает переданную замену
// publicValues либо сохраняет прежние значения при отсутствии поля. Отличает отсутствие от явной
// пустой замены; ничего не сохраняет и секреты не ищет.
func assistantEnvironmentValuesForUpdate(snapshot any, input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return nil, false
	}
	if _, supplied := input["publicValues"]; supplied {
		return assistantEnvironmentPublicValues(input)
	}
	return specification.Values, true
}

// assistantEnvironmentValuesMatch Requires successful snapshot decoding and deep equality of
// public values, preserving the nil-versus-empty distinction used by this check. It compares
// stored data without normalizing or mutating it.
// assistantEnvironmentValuesMatch Требует успешного декодирования снимка и глубокого равенства
// публичных значений, сохраняя различие nil и пустого среза. Сравнивает сохранённые данные без
// нормализации или изменения.
func assistantEnvironmentValuesMatch(snapshot any, values []entity.RuntimeEnvironmentValue) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && reflect.DeepEqual(specification.Values, values)
}

// assistantEnvironmentBindingsMatch Requires successful snapshot decoding and deep equality of
// secret reference/revision bindings, including their order and nil/empty shape. Equality neither
// reads secret values nor grants access to referenced secrets.
// assistantEnvironmentBindingsMatch Требует успешного декодирования снимка и глубокого равенства
// привязок ссылок и ревизий секретов, включая порядок и форму nil/empty. Равенство не читает
// значения секретов и не выдаёт доступ к ним.
func assistantEnvironmentBindingsMatch(snapshot any, bindings []entity.RuntimeSecretBinding) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && reflect.DeepEqual(specification.SecretBindings, bindings)
}

// assistantEnvironmentSnapshotBindingsCompatible Decodes the stored specification and checks its
// existing secret bindings against replacement public values for valid references and noncolliding
// names. It does not resolve secret ownership or freshness.
// assistantEnvironmentSnapshotBindingsCompatible Декодирует сохранённую спецификацию и проверяет
// её привязки секретов относительно новых публичных значений: допустимые ссылки и отсутствие
// совпадающих имён. Владение и свежесть секретов не разрешает.
func assistantEnvironmentSnapshotBindingsCompatible(snapshot any, values []entity.RuntimeEnvironmentValue) bool {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	return valid && assistantEnvironmentBindingsCompatible(values, specification.SecretBindings)
}

// assistantEnvironmentBindingsCompatible Checks at most 128 bindings for valid environment names,
// lexical secret refs, nonnegative revisions and uniqueness against all public-value names and
// earlier bindings. Public-value validity and actual secret access are owned by other validation
// stages.
// assistantEnvironmentBindingsCompatible Проверяет до 128 привязок: допустимые имена окружения,
// форму ссылок секретов, неотрицательные ревизии и уникальность относительно публичных значений и
// прежних привязок. Пригодность самих публичных значений и доступ к секретам проверяются на других
// этапах.
func assistantEnvironmentBindingsCompatible(values []entity.RuntimeEnvironmentValue, bindings []entity.RuntimeSecretBinding) bool {
	if len(bindings) > 128 {
		return false
	}
	seen := make(map[string]struct{}, len(values)+len(bindings))
	for _, value := range values {
		seen[value.Name] = struct{}{}
	}
	for _, binding := range bindings {
		if !runtimecontract.ValidRuntimeEnvironmentName(binding.Name) ||
			!assistantEnvironmentSecretRefValid(binding.SecretRef) || binding.Revision < 0 {
			return false
		}
		if _, duplicate := seen[binding.Name]; duplicate {
			return false
		}
		seen[binding.Name] = struct{}{}
	}
	return true
}

// assistantEnvironmentSecretRefValid Accepts only 8..96-byte sec_-prefixed ASCII
// alphanumeric/underscore/hyphen references. This lexical check neither loads a secret nor proves
// tenant ownership or a published revision.
// assistantEnvironmentSecretRefValid Допускает только ссылки длиной 8..96 байт с префиксом sec_ из
// ASCII-букв, цифр, подчёркивания и дефиса. Проверка формы не загружает секрет и не подтверждает
// организацию или опубликованную ревизию.
func assistantEnvironmentSecretRefValid(secretRef string) bool {
	if !strings.HasPrefix(secretRef, "sec_") || len(secretRef) < 8 || len(secretRef) > 96 {
		return false
	}
	for _, char := range secretRef {
		if !(char >= 'a' && char <= 'z') && !(char >= 'A' && char <= 'Z') &&
			!(char >= '0' && char <= '9') && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

// assistantSensitiveVariableFragments Defines the case-sensitive name fragments excluded from
// assistant-proposed public values and removals. It is a narrow lexical guard, not content
// inspection or a complete detector of secret values.
// assistantSensitiveVariableFragments Задаёт чувствительные к регистру фрагменты имён, запрещённые
// в предложенных ассистентом публичных значениях и удалениях. Это узкий лексический фильтр, а не
// просмотр содержимого или полный поиск значений секретов.
var assistantSensitiveVariableFragments = []string{
	"SECRET", "PASSWORD", "TOKEN", "CREDENTIAL", "PRIVATE_KEY", "API_KEY",
}

// assistantEnvironmentPublicValues Parses up to 128 optional closed name/value records, rejects
// sensitive name fragments and delegates final name/value/uniqueness validation to
// runtimecontract. Omitted input returns nil; explicit empty input remains allocated. Values are
// intended public configuration, never loaded from credential storage.
// assistantEnvironmentPublicValues Разбирает до 128 необязательных записей с закрытыми полями
// name/value, отклоняет чувствительные фрагменты имён и передаёт итоговую проверку имён, значений
// и уникальности runtimecontract. Отсутствие поля даёт nil, явный пустой список остаётся
// созданным. Это публичная конфигурация без чтения хранилищ credential.
func assistantEnvironmentPublicValues(input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	raw, supplied := input["publicValues"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	values := make([]entity.RuntimeEnvironmentValue, 0, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "value") || !hasAssistantFields(item, "name", "value") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		value, valueOK := item["value"].(string)
		if !nameOK || !valueOK || name != strings.TrimSpace(name) {
			return nil, false
		}
		if assistantEnvironmentSensitiveName(name) {
			return nil, false
		}
		values = append(values, entity.RuntimeEnvironmentValue{Name: name, Value: value})
	}
	contractValues := runtimeEnvironmentValuesContract(values)
	if runtimecontract.ValidateRuntimeEnvironment(contractValues, nil) != nil {
		return nil, false
	}
	return values, true
}

// assistantEnvironmentPatchedPublicValues Applies explicit sparse public-value updates/removals to
// a decoded snapshot, rejects duplicate/conflicting/sensitive removals, preserves surviving order
// and appends new updates in input order. Enforces 128 final values and runtime validation;
// returns false when no patch is supplied and never mutates the snapshot.
// assistantEnvironmentPatchedPublicValues Применяет явные частичные обновления или удаления к
// декодированному снимку публичных значений. Отклоняет повторные, конфликтующие и чувствительные
// удаления, сохраняет порядок оставшихся значений и добавляет новые по порядку входа. Проверяет
// предел 128 и runtime-контракт; отсутствие изменений даёт false, снимок не меняется.
func assistantEnvironmentPatchedPublicValues(snapshot any, input map[string]any) ([]entity.RuntimeEnvironmentValue, bool) {
	specification, valid := assistantEnvironmentSnapshotSpecification(snapshot)
	if !valid {
		return nil, false
	}
	updatesRaw, updatesSupplied := input["publicValueUpdates"]
	removalsRaw, removalsSupplied := input["publicValueRemovals"]
	if !updatesSupplied && !removalsSupplied {
		return nil, false
	}
	updates := []entity.RuntimeEnvironmentValue{}
	if updatesSupplied {
		var updatesValid bool
		updates, updatesValid = assistantEnvironmentPublicValues(map[string]any{"publicValues": updatesRaw})
		if !updatesValid {
			return nil, false
		}
	}
	updateByName := make(map[string]entity.RuntimeEnvironmentValue, len(updates))
	for _, update := range updates {
		if _, duplicate := updateByName[update.Name]; duplicate {
			return nil, false
		}
		updateByName[update.Name] = update
	}
	removals := make(map[string]struct{})
	if removalsSupplied {
		entries, ok := removalsRaw.([]any)
		if !ok || len(entries) > 128 {
			return nil, false
		}
		for _, entry := range entries {
			name, ok := entry.(string)
			if !ok || !runtimecontract.ValidRuntimeEnvironmentName(name) {
				return nil, false
			}
			if assistantEnvironmentSensitiveName(name) {
				return nil, false
			}
			if _, duplicate := removals[name]; duplicate {
				return nil, false
			}
			if _, conflicted := updateByName[name]; conflicted {
				return nil, false
			}
			removals[name] = struct{}{}
		}
	}
	result := make([]entity.RuntimeEnvironmentValue, 0, len(specification.Values)+len(updates))
	consumedUpdates := make(map[string]struct{}, len(updates))
	for _, current := range specification.Values {
		if _, removed := removals[current.Name]; removed {
			continue
		}
		if update, replaced := updateByName[current.Name]; replaced {
			result = append(result, update)
			consumedUpdates[current.Name] = struct{}{}
			continue
		}
		result = append(result, current)
	}
	for _, update := range updates {
		if _, consumed := consumedUpdates[update.Name]; !consumed {
			result = append(result, update)
		}
	}
	if len(result) > 128 {
		return nil, false
	}
	contractValues := runtimeEnvironmentValuesContract(result)
	if runtimecontract.ValidateRuntimeEnvironment(contractValues, nil) != nil {
		return nil, false
	}
	return result, true
}

// assistantEnvironmentPublicValuesInput Projects typed public values into an allocated ordered
// list of name/value maps, including empty input, for assistant command parameters. It does not
// copy or expose any secret binding/value.
// assistantEnvironmentPublicValuesInput Проецирует типизированные публичные значения в созданный
// упорядоченный список карт name/value для параметров команды, включая пустой вход. Привязки и
// значения секретов не копирует и не раскрывает.
func assistantEnvironmentPublicValuesInput(values []entity.RuntimeEnvironmentValue) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"name": value.Name, "value": value.Value})
	}
	return result
}

// assistantEnvironmentSecretBindings Parses up to 128 optional closed binding records, validates
// environment names and lexical secret refs, rejects collisions with public values/earlier
// bindings and accepts optional nonnegative revision. Omission returns nil; zero revision stays
// unresolved for later owner validation.
// assistantEnvironmentSecretBindings Разбирает до 128 необязательных привязок с закрытым набором
// полей, проверяет имена окружения и форму ссылок секретов, отклоняет совпадения с публичными
// значениями и прежними привязками. Допускает необязательную неотрицательную ревизию; отсутствие
// списка даёт nil, нулевая ревизия разрешается позднее владельцем.
func assistantEnvironmentSecretBindings(input map[string]any, values []entity.RuntimeEnvironmentValue) ([]entity.RuntimeSecretBinding, bool) {
	raw, supplied := input["secretBindings"]
	if !supplied {
		return nil, true
	}
	entries, ok := raw.([]any)
	if !ok || len(entries) > 128 {
		return nil, false
	}
	seen := make(map[string]struct{}, len(values)+len(entries))
	for _, value := range values {
		seen[value.Name] = struct{}{}
	}
	bindings := make([]entity.RuntimeSecretBinding, 0, len(entries))
	for _, entry := range entries {
		item, valid := entry.(map[string]any)
		if !valid || !onlyAssistantFields(item, "name", "secretRef", "revision") ||
			!hasAssistantFields(item, "name", "secretRef") {
			return nil, false
		}
		name, nameOK := item["name"].(string)
		secretRef, refOK := item["secretRef"].(string)
		if !nameOK || !refOK || !runtimecontract.ValidRuntimeEnvironmentName(name) ||
			!assistantEnvironmentSecretRefValid(secretRef) {
			return nil, false
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, false
		}
		seen[name] = struct{}{}
		var revision int64
		if _, supplied := item["revision"]; supplied {
			var revisionOK bool
			revision, revisionOK = assistantInt64(item, "revision")
			if !revisionOK || revision < 0 {
				return nil, false
			}
		}
		bindings = append(bindings, entity.RuntimeSecretBinding{Name: name, SecretRef: secretRef, Revision: revision})
	}
	return bindings, true
}

// assistantEnvironmentSensitiveName matches the existing case-sensitive forbidden substrings
// used by public-value insertion and removal. It checks only the name; callers retain environment
// name/value validation, and absence of a fragment does not prove that content is nonsecret.
// assistantEnvironmentSensitiveName ищет прежние чувствительные к регистру запрещённые фрагменты
// имени для добавления и удаления публичных значений. Проверяет только имя; вызывающие сохраняют
// проверку имён и значений, а отсутствие фрагмента не доказывает несекретность содержимого.
func assistantEnvironmentSensitiveName(name string) bool {
	for _, fragment := range assistantSensitiveVariableFragments {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}
