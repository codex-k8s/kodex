package receipt

import api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

// ExternalDigest hashes schema, tenant/mailbox, immutable effect input and audit; mutable protocol outcome and resource coordinates are excluded.
// This deterministic identity correlation is not a signature or proof of authority.
// ExternalDigest хеширует схему, tenant/ящик, неизменяемый вход эффекта и audit; изменяемый исход протокола и координаты ресурса исключены.
// Детерминированная связь идентичности не является подписью или доказательством полномочий.
func (r Record) ExternalDigest(scope Scope) string {
	return api.Digest(struct {
		Schema        string        `json:"schema"`
		Tenant        string        `json:"tenant"`
		Mailbox       string        `json:"mailbox"`
		ID            string        `json:"id"`
		Key           string        `json:"effect_key"`
		Input         string        `json:"semantic_input_digest"`
		Resource      string        `json:"resource_digest"`
		Actor         string        `json:"actor"`
		Agent         string        `json:"agent"`
		Grant         string        `json:"grant"`
		Operation     api.Operation `json:"operation"`
		Configuration int64         `json:"configuration_revision"`
		Credential    int64         `json:"credential_generation"`
		Gate          bool          `json:"gate_approved"`
	}{"kodex.email.receipt.v1", scope.Tenant, scope.Mailbox, r.ID, r.Key, r.Digest, r.Resource, r.Audit.Actor, r.Audit.Agent, r.Audit.Grant, r.Audit.Operation, r.Audit.ConfigurationRevision, r.Audit.CredentialGeneration, r.Audit.GateApproved})
}

// Outcome maps accepted/deleted to confirmed effect and failed to confirmed absence; every other status remains unknown.
// Outcome сопоставляет accepted/deleted с подтверждённым эффектом, failed с его отсутствием; все иные статусы остаются неопределёнными.
func (r Record) Outcome() Outcome {
	switch r.Status {
	case "accepted", "deleted":
		return EffectConfirmed
	case "failed":
		return NoEffectConfirmed
	default:
		return Unknown
	}
}
