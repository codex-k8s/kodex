package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"
)

const (
	// ManagedMCPContext7 is the only managed provider in this profile revision.
	// ManagedMCPContext7 — единственный управляемый провайдер в этой версии профиля.
	ManagedMCPContext7 = "CONTEXT7"
	// ManagedMCPNamespace isolates managed tools in the agent tool registry.
	// ManagedMCPNamespace изолирует управляемые инструменты в реестре агента.
	ManagedMCPNamespace = "kodex"
	// ManagedMCPProfileVersion pins the managed profile schema.
	// ManagedMCPProfileVersion закрепляет схему управляемого профиля.
	ManagedMCPProfileVersion = 1
	// ManagedMCPHealthProbe names the required adapter initialization and tool-list proof.
	// ManagedMCPHealthProbe называет обязательную проверку инициализации адаптера и списка инструментов.
	ManagedMCPHealthProbe = "CONTEXT7_INITIALIZE_TOOLS_LIST"
	// ManagedMCPHealthMaximumAge bounds the age of the owner-verified health receipt.
	// ManagedMCPHealthMaximumAge ограничивает возраст подтверждённой владельцем проверки здоровья.
	ManagedMCPHealthMaximumAge = 5 * time.Minute
	// Context7ResolveTool names the tool exposed for library identification.
	// Context7ResolveTool называет инструмент определения библиотеки.
	Context7ResolveTool = "context7_resolve_library_id"
	// Context7QueryTool names the tool exposed for documentation lookup.
	// Context7QueryTool называет инструмент поиска документации.
	Context7QueryTool = "context7_query_docs"
	// Context7ResolveCapability binds the resolve operation to an exact grant.
	// Context7ResolveCapability связывает операцию определения с точным разрешением.
	Context7ResolveCapability = "context7.library.resolve"
	// Context7QueryCapability binds the documentation query to an exact grant.
	// Context7QueryCapability связывает запрос документации с точным разрешением.
	Context7QueryCapability = "context7.docs.query"
)

// errManagedMCPProfile denotes an invalid profile or grant binding.
// errManagedMCPProfile обозначает неверную привязку профиля или разрешения.
var errManagedMCPProfile = errors.New("managed MCP profile binding is invalid")

// errManagedMCPHealth denotes an absent or stale owner-verified health proof.
// errManagedMCPHealth обозначает отсутствие или устаревание подтверждённой владельцем проверки здоровья.
var errManagedMCPHealth = errors.New("required managed MCP health proof is unavailable")

// ManagedMCPProfile binds closed tools to exact grants in an immutable revision;
// it carries no transport, URL, command, or credential value.
// ManagedMCPProfile связывает закрытые инструменты с точными разрешениями неизменной версии;
// он не содержит transport, URL, command или значения учётных данных.
type ManagedMCPProfile struct {
	Provider        string                `json:"provider"`
	Version         int64                 `json:"version"`
	Namespace       string                `json:"namespace"`
	Required        bool                  `json:"required"`
	ScopeKind       string                `json:"scope_kind"`
	ScopeRef        string                `json:"scope_ref"`
	ResolveGrantRef string                `json:"resolve_grant_ref"`
	QueryGrantRef   string                `json:"query_grant_ref"`
	Digest          string                `json:"digest"`
	Health          ManagedMCPHealthProof `json:"health"`
}

// ManagedMCPHealthProof records a real adapter initialize/list receipt verified by the owner
// against the current configuration, credential revision, and package before grants are issued.
// ManagedMCPHealthProof хранит подтверждённый владельцем результат реальной инициализации и списка;
// перед выдачей разрешений он сверяется с текущими конфигурацией, версией учётных данных и пакетом.
type ManagedMCPHealthProof struct {
	TestRef               string    `json:"test_ref"`
	Generation            int64     `json:"generation"`
	ConnectionRef         string    `json:"connection_ref"`
	ConnectionVersion     int64     `json:"connection_version"`
	ConfigurationSHA256   string    `json:"configuration_sha256"`
	CredentialRevisionRef string    `json:"credential_revision_ref"`
	CredentialRevision    int64     `json:"credential_revision"`
	CredentialSHA256      string    `json:"credential_sha256"`
	DefinitionKey         string    `json:"definition_key"`
	DefinitionVersion     string    `json:"definition_version"`
	DefinitionDigest      string    `json:"definition_digest"`
	CheckedAt             time.Time `json:"checked_at"`
	Probe                 string    `json:"probe"`
}

// DeriveContext7ManagedMCPProfile binds already selected owner grants and rejects ambiguity,
// including a second otherwise valid connection, instead of choosing the first match.
// DeriveContext7ManagedMCPProfile связывает уже выбранные владельцем разрешения и отклоняет
// неоднозначность, включая второе допустимое подключение, вместо выбора первого совпадения.
func DeriveContext7ManagedMCPProfile(scopeKind, scopeRef string, grants []RunnerIntegrationGrant, health ManagedMCPHealthProof) (ManagedMCPProfile, error) {
	profile := ManagedMCPProfile{Provider: ManagedMCPContext7, Version: ManagedMCPProfileVersion, Namespace: ManagedMCPNamespace,
		Required: true, ScopeKind: scopeKind, ScopeRef: scopeRef, Health: health}
	for _, grant := range grants {
		if grant.DefinitionKey != "context7" {
			continue
		}
		switch grant.CapabilityKey {
		case Context7ResolveCapability:
			if profile.ResolveGrantRef != "" {
				return ManagedMCPProfile{}, errManagedMCPProfile
			}
			profile.ResolveGrantRef = grant.Ref
		case Context7QueryCapability:
			if profile.QueryGrantRef != "" {
				return ManagedMCPProfile{}, errManagedMCPProfile
			}
			profile.QueryGrantRef = grant.Ref
		default:
			return ManagedMCPProfile{}, errManagedMCPProfile
		}
	}
	resolve, query, err := profile.boundGrants(grants)
	if err != nil {
		return ManagedMCPProfile{}, err
	}
	profile.Digest, err = managedMCPDigest(profile, resolve, query)
	return profile, err
}

// boundGrants verifies exactly one read-only resolve grant and one read-only query grant
// on the same connection, then pins the owner health receipt to their definition.
// boundGrants проверяет ровно одно разрешение чтения для определения библиотеки и одно для запроса
// на общем подключении, затем привязывает проверку владельца к их определению.
func (profile ManagedMCPProfile) boundGrants(grants []RunnerIntegrationGrant) (RunnerIntegrationGrant, RunnerIntegrationGrant, error) {
	var resolve, query RunnerIntegrationGrant
	if profile.Provider != ManagedMCPContext7 || profile.Version != ManagedMCPProfileVersion ||
		profile.Namespace != ManagedMCPNamespace || !profile.Required ||
		!containsString([]string{"SYSTEM", "PROJECT", "AGENT"}, profile.ScopeKind) || !opaqueReferencePattern.MatchString(profile.ScopeRef) ||
		profile.ResolveGrantRef == profile.QueryGrantRef || !validIntegrationGrants(grants) {
		return resolve, query, errManagedMCPProfile
	}
	count := 0
	for _, grant := range grants {
		if grant.DefinitionKey != "context7" {
			continue
		}
		count++
		if grant.Ref == profile.ResolveGrantRef && grant.CapabilityKey == Context7ResolveCapability {
			resolve = grant
		}
		if grant.Ref == profile.QueryGrantRef && grant.CapabilityKey == Context7QueryCapability {
			query = grant
		}
	}
	if count != 2 || resolve.Ref == "" || query.Ref == "" || resolve.Risk != "READ" || query.Risk != "READ" ||
		resolve.ApprovalPolicy != "NONE" || query.ApprovalPolicy != "NONE" ||
		resolve.Operation != Context7ResolveCapability || query.Operation != Context7QueryCapability ||
		resolve.ConnectionRef != query.ConnectionRef || resolve.ConnectionVersion != query.ConnectionVersion ||
		resolve.DefinitionVersion != query.DefinitionVersion || resolve.DefinitionDigest != query.DefinitionDigest {
		return resolve, query, errManagedMCPProfile
	}
	health := profile.Health
	if !opaqueReferencePattern.MatchString(health.TestRef) || health.Generation < 1 || health.Probe != ManagedMCPHealthProbe ||
		health.ConnectionRef != resolve.ConnectionRef || health.ConnectionVersion != resolve.ConnectionVersion ||
		health.DefinitionKey != "context7" || health.DefinitionVersion != resolve.DefinitionVersion || health.DefinitionDigest != resolve.DefinitionDigest ||
		!sha256Pattern.MatchString(health.ConfigurationSHA256) || !sha256Pattern.MatchString(health.CredentialSHA256) ||
		!opaqueReferencePattern.MatchString(health.CredentialRevisionRef) || health.CredentialRevision < 1 || health.CheckedAt.IsZero() || health.CheckedAt.Location() != time.UTC {
		return resolve, query, errManagedMCPHealth
	}
	return resolve, query, nil
}

// managedMCPDigest hashes the profile and the two ordered grants without the digest field itself.
// managedMCPDigest хеширует профиль и два упорядоченных разрешения без самого поля дайджеста.
func managedMCPDigest(profile ManagedMCPProfile, resolve, query RunnerIntegrationGrant) (string, error) {
	profile.Digest = ""
	raw, err := json.Marshal(struct {
		Profile ManagedMCPProfile         `json:"profile"`
		Grants  [2]RunnerIntegrationGrant `json:"grants"`
	}{profile, [2]RunnerIntegrationGrant{resolve, query}})
	if err != nil {
		return "", errManagedMCPProfile
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// ValidateManagedMCPProfiles checks deterministic structure, scope, grants, and digest;
// freshness is checked separately so historical revision digests do not depend on a clock.
// ValidateManagedMCPProfiles проверяет детерминированную структуру, область, разрешения и дайджест;
// актуальность проверяется отдельно, чтобы исторический дайджест версии не зависел от часов.
func ValidateManagedMCPProfiles(input RunnerInput) error {
	if len(input.ManagedMCPProfiles) > 1 {
		return errManagedMCPProfile
	}
	if len(input.ManagedMCPProfiles) == 0 {
		for _, grant := range input.IntegrationGrants {
			if grant.DefinitionKey == "context7" {
				return errManagedMCPProfile
			}
		}
	}
	for _, profile := range input.ManagedMCPProfiles {
		resolve, query, err := profile.boundGrants(input.IntegrationGrants)
		if err != nil {
			return err
		}
		matched := profile.ScopeKind == "SYSTEM" && input.IsSystemAssistant() && input.AssistantProfileRef == "" && profile.ScopeRef == input.AgentRef ||
			profile.ScopeKind == "PROJECT" && input.AssistantScope == AssistantScopeProject && opaqueReferencePattern.MatchString(input.ProjectRef) && profile.ScopeRef == input.AssistantProfileRef ||
			profile.ScopeKind == "AGENT" && !input.IsAssistant() && input.AssistantScope == AssistantScopeNone && input.AssistantProfileRef == "" && opaqueReferencePattern.MatchString(input.ProjectRef) && profile.ScopeRef == input.AgentRef
		digest, err := managedMCPDigest(profile, resolve, query)
		if !matched || err != nil || !sha256Pattern.MatchString(profile.Digest) || digest != profile.Digest {
			return errManagedMCPProfile
		}
	}
	return nil
}

// ValidateManagedMCPReadiness checks owner-receipt freshness just before provider startup
// without making external calls or issuing new grants.
// ValidateManagedMCPReadiness проверяет актуальность подтверждения владельца перед запуском
// провайдера, не выполняя внешних вызовов и не выдавая новых разрешений.
func ValidateManagedMCPReadiness(input RunnerInput, now time.Time) error {
	if err := ValidateManagedMCPProfiles(input); err != nil {
		return err
	}
	for _, profile := range input.ManagedMCPProfiles {
		age := now.Sub(profile.Health.CheckedAt)
		if age < 0 || age > ManagedMCPHealthMaximumAge {
			return errManagedMCPHealth
		}
	}
	return nil
}

// ManagedMCPToolSchemas returns schemas from the exact validated grants, excluding authority fields.
// ManagedMCPToolSchemas возвращает схемы точных проверенных разрешений без полей полномочий.
func ManagedMCPToolSchemas(input RunnerInput) (map[string]string, error) {
	if err := ValidateManagedMCPProfiles(input); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for _, profile := range input.ManagedMCPProfiles {
		resolve, query, _ := profile.boundGrants(input.IntegrationGrants)
		result[Context7ResolveTool], result[Context7QueryTool] = resolve.InputSchema, query.InputSchema
	}
	return result, nil
}

// ManagedMCPSchemaDigest canonicalizes JSON key order for the closed object schema and rejects duplicates.
// ManagedMCPSchemaDigest канонизирует порядок ключей JSON закрытой схемы объекта и отклоняет повторы.
func ManagedMCPSchemaDigest(raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return "", errManagedMCPProfile
	}
	unique := json.NewDecoder(bytes.NewReader(raw))
	if promptJSONUnique(unique, 0) != nil || unique.Decode(&struct{}{}) != io.EOF {
		return "", errManagedMCPProfile
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var schema map[string]any
	if decoder.Decode(&schema) != nil || schema["type"] != "object" || schema["additionalProperties"] != false {
		return "", errManagedMCPProfile
	}
	canonical, err := json.Marshal(schema)
	if err != nil {
		return "", errManagedMCPProfile
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
