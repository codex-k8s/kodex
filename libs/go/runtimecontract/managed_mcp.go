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
	ManagedMCPContext7         = "CONTEXT7"
	ManagedMCPNamespace        = "kodex"
	ManagedMCPProfileVersion   = 1
	ManagedMCPHealthProbe      = "CONTEXT7_INITIALIZE_TOOLS_LIST"
	ManagedMCPHealthMaximumAge = 5 * time.Minute
	Context7ResolveTool        = "context7_resolve_library_id"
	Context7QueryTool          = "context7_query_docs"
	Context7ResolveCapability  = "context7.library.resolve"
	Context7QueryCapability    = "context7.docs.query"
)

var errManagedMCPProfile = errors.New("managed MCP profile binding is invalid")
var errManagedMCPHealth = errors.New("required managed MCP health proof is unavailable")

// ManagedMCPProfile связывает закрытые tools с exact grants immutable revision.
// Профиль не содержит transport, URL, command либо credential value.
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

// ManagedMCPHealthProof — owner-verified receipt реального adapter initialize/list.
// Owner сверяет snapshot с current configuration/credential/package до выдачи.
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

// DeriveContext7ManagedMCPProfile принимает уже выбранные owner grants. Любая
// неоднозначность, даже второй допустимый connection, не выбирается first-match.
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

// ValidateManagedMCPProfiles — deterministic structural проверка; время жизни
// проверяется отдельно, чтобы исторический revision digest не зависел от часов.
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

// ValidateManagedMCPReadiness проверяет свежесть owner receipt непосредственно
// перед provider startup, не выполняя внешних вызовов или выдачи новых grants.
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

// ManagedMCPToolSchemas возвращает schemas exact grants без authority полей.
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

// ManagedMCPSchemaDigest нормализует порядок JSON keys, но не допускает дубли.
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
