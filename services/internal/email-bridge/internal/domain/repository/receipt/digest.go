package receipt

import api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"

// Дайджест фиксирует исходный receipt; результат протокола не меняет его identity.
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
