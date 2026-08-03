// Package policy загружает версионированную политику производителей control-plane.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	authorityservice "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/authority"
)

const (
	maximumPolicyBytes = 1 << 20
	resolverFullMethod = "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof"
)

// Producer — точный назначенный сервером производитель прикладных полномочий.
type Producer struct {
	ID                 string
	CallerWorkload     string
	CallerSPIFFEID     string
	Credential         string
	CredentialMetadata string
	CredentialIssuer   string
	CredentialAudience string
	ProofAudience      string
}

// Loaded содержит всех зарегистрированных сервером производителей доказательств.
type Loaded struct {
	Revision                     uint64
	Digest                       string
	Issuer                       string
	ProofAudience                string
	AuthorizationContextAudience string
	OIDC                         Producer
	Producers                    map[string]Producer
	Operations                   map[string]authorityservice.Operation
}

type document struct {
	Version  int             `json:"v"`
	Revision uint64          `json:"policy_revision"`
	Policy   json.RawMessage `json:"policy"`
}

type rawPolicy struct {
	DefaultDecision string          `json:"default_decision"`
	ProofProducers  []proofProducer `json:"authority_proof_producers"`
	Bindings        []binding       `json:"operation_bindings"`
}

type proofProducer struct {
	ID                  string   `json:"producer_id"`
	OwnerWorkload       string   `json:"owner_workload_id"`
	OwnerSPIFFEID       string   `json:"owner_spiffe_id"`
	FullMethod          string   `json:"full_method"`
	CallerWorkload      string   `json:"caller_workload_id"`
	CallerSPIFFEID      string   `json:"caller_spiffe_id"`
	Credential          string   `json:"application_credential"`
	CredentialMetadata  string   `json:"application_credential_metadata"`
	CredentialIssuer    string   `json:"application_credential_issuer"`
	CredentialAudience  string   `json:"application_credential_audience"`
	ProofIssuer         string   `json:"authority_proof_issuer"`
	ProofAudience       string   `json:"authority_proof_audience"`
	AllowedOperationIDs []string `json:"allowed_operation_ids"`
}

type binding struct {
	OperationID     string `json:"operation_id"`
	FullMethod      string `json:"full_method"`
	Permission      string `json:"permission"`
	Audience        string `json:"audience"`
	ProofProducerID string `json:"authority_proof_producer_id"`
	CallerWorkload  string `json:"caller_workload_id"`
	CallerSPIFFEID  string `json:"caller_spiffe_id"`
	TargetWorkload  string `json:"target_workload_id"`
	TargetSPIFFEID  string `json:"target_spiffe_id"`
	ProjectRequired bool   `json:"project_required"`
}

// Load связывает каждую операцию ровно с одним точным производителем доказательств.
func Load(path string, expected map[string]string) (Loaded, error) {
	raw, err := readBounded(path)
	if err != nil {
		return Loaded{}, err
	}
	var envelope map[string]json.RawMessage
	if err := decodeStrict(raw, &envelope); err != nil {
		return Loaded{}, fmt.Errorf("decode authority policy envelope: %w", err)
	}
	if len(envelope) != 3 {
		return Loaded{}, errors.New("authority policy envelope fields are invalid")
	}
	var parsed document
	if err := decodeStrict(raw, &parsed); err != nil {
		return Loaded{}, fmt.Errorf("decode authority policy: %w", err)
	}
	var policy rawPolicy
	if err := json.Unmarshal(parsed.Policy, &policy); err != nil {
		return Loaded{}, fmt.Errorf("decode authority policy body: %w", err)
	}
	if parsed.Version != 1 || parsed.Revision == 0 ||
		policy.DefaultDecision != "DENY" {
		return Loaded{}, errors.New("authority policy is not deny-by-default")
	}
	var policyValue any
	if err := json.Unmarshal(parsed.Policy, &policyValue); err != nil {
		return Loaded{}, err
	}
	digest, err := internalrpcauth.CanonicalJSONSHA256(policyValue)
	if err != nil {
		return Loaded{}, err
	}
	producers := make(map[string]Producer, len(policy.ProofProducers))
	allowed := make(map[string]string)
	var oidc Producer
	var issuer, proofAudience string
	for _, candidate := range policy.ProofProducers {
		if candidate.ID == "" ||
			candidate.OwnerWorkload != "control-plane" ||
			candidate.OwnerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane" ||
			candidate.FullMethod != resolverFullMethod ||
			candidate.CallerWorkload == "" || candidate.CallerSPIFFEID == "" ||
			candidate.CredentialMetadata != "authorization" ||
			candidate.CredentialIssuer == "" ||
			candidate.CredentialAudience == "" ||
			!supportedCredential(candidate.Credential) ||
			candidate.ProofIssuer != candidate.OwnerSPIFFEID ||
			candidate.ProofAudience == "" {
			return Loaded{}, errors.New("control-plane proof producer is invalid")
		}
		if _, duplicate := producers[candidate.ID]; duplicate {
			return Loaded{}, errors.New("control-plane proof producer is duplicated")
		}
		producer := Producer{
			ID:                 candidate.ID,
			CallerWorkload:     candidate.CallerWorkload,
			CallerSPIFFEID:     candidate.CallerSPIFFEID,
			Credential:         candidate.Credential,
			CredentialMetadata: candidate.CredentialMetadata,
			CredentialIssuer:   candidate.CredentialIssuer,
			CredentialAudience: candidate.CredentialAudience,
			ProofAudience:      candidate.ProofAudience,
		}
		producers[candidate.ID] = producer
		if candidate.ID == "control-plane.oidc" {
			if candidate.Credential != "OIDC_BEARER" {
				return Loaded{}, errors.New("control-plane OIDC producer is invalid")
			}
			oidc = producer
			issuer = candidate.ProofIssuer
			proofAudience = candidate.ProofAudience
		} else if candidate.Credential == "OIDC_BEARER" {
			return Loaded{}, errors.New("OIDC credential producer is ambiguous")
		}
		for _, operationID := range candidate.AllowedOperationIDs {
			if _, duplicate := allowed[operationID]; duplicate {
				return Loaded{}, errors.New("proof producer operation is duplicated")
			}
			allowed[operationID] = candidate.ID
		}
	}
	if oidc.ID == "" || issuer == "" || len(producers) < 2 {
		return Loaded{}, errors.New("control-plane producer registry is incomplete")
	}
	operations := make(map[string]authorityservice.Operation, len(expected))
	for _, candidate := range policy.Bindings {
		producer, producerExists := producers[candidate.ProofProducerID]
		expectedMethod, ok := expected[candidate.OperationID]
		permittedProducer := allowed[candidate.OperationID]
		if !producerExists || !ok || permittedProducer != candidate.ProofProducerID ||
			candidate.FullMethod != expectedMethod ||
			candidate.Permission == "" ||
			candidate.Audience == "" ||
			candidate.CallerWorkload != producer.CallerWorkload ||
			candidate.CallerSPIFFEID != producer.CallerSPIFFEID || !validTarget(candidate) {
			return Loaded{}, errors.New("control-plane operation binding is invalid")
		}
		if _, duplicate := operations[candidate.OperationID]; duplicate {
			return Loaded{}, errors.New("control-plane operation binding is ambiguous")
		}
		operations[candidate.OperationID] = authorityservice.Operation{
			FullMethod:                   candidate.FullMethod,
			Permission:                   candidate.Permission,
			ProjectRequired:              candidate.ProjectRequired,
			TenantOwnerOnly:              candidate.OperationID == "control.project.create",
			CallerWorkload:               producer.CallerWorkload,
			CallerSPIFFEID:               producer.CallerSPIFFEID,
			ActorKind:                    producerActorKind(producer),
			AuthoritySource:              producerAuthoritySource(producer),
			ProofAudience:                producer.ProofAudience,
			AuthorizationContextAudience: candidate.Audience,
		}
	}
	if len(operations) != len(expected) || len(allowed) != len(expected) {
		return Loaded{}, errors.New("control-plane proof producer operation set is incomplete")
	}
	return Loaded{
		Revision:                     parsed.Revision,
		Digest:                       digest,
		Issuer:                       issuer,
		ProofAudience:                proofAudience,
		AuthorizationContextAudience: "urn:mattercodex:internal-rpc:control-plane",
		OIDC:                         oidc,
		Producers:                    producers,
		Operations:                   operations,
	}, nil
}

func validTarget(candidate binding) bool {
	switch candidate.Audience {
	case "urn:mattercodex:internal-rpc:control-plane":
		return candidate.TargetWorkload == "control-plane" &&
			candidate.TargetSPIFFEID == "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane"
	case "urn:mattercodex:internal-rpc:integration-gateway":
		return candidate.TargetWorkload == "integration-gateway" &&
			candidate.TargetSPIFFEID == "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway"
	default:
		return false
	}
}

func producerActorKind(producer Producer) string {
	if producer.Credential == "OIDC_BEARER" {
		return "HUMAN"
	}
	return "WORKLOAD"
}

func producerAuthoritySource(producer Producer) string {
	switch producer.Credential {
	case "OIDC_BEARER":
		return "OIDC_SESSION"
	case "AGENT_SESSION_GRANT":
		return "AGENT_SESSION"
	case "AUTOMATION_OCCURRENCE_GRANT":
		return "AUTOMATION_OCCURRENCE"
	case "INTEGRATION_CONTINUATION_GRANT":
		return "INTEGRATION_CONTINUATION"
	default:
		return "PROCESS_RUN"
	}
}

func supportedCredential(credential string) bool {
	switch credential {
	case "OIDC_BEARER",
		"AGENT_SESSION_GRANT",
		"AUTOMATION_OCCURRENCE_GRANT",
		"PROCESS_RUN_GRANT",
		"OWNER_GATE_DELIVERY_GRANT",
		"RUNTIME_REVISION_GRANT",
		"RUNTIME_RESTORE_VERIFIER_GRANT",
		"RUNTIME_CLEANUP_AUTHORIZER_GRANT",
		"MEMORY_INDEX_GRANT":
		// server-owned capability следующего exact integration transition.
		return true
	case "INTEGRATION_CONTINUATION_GRANT":
		return true
	default:
		return false
	}
}

func readBounded(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open authority policy: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maximumPolicyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > maximumPolicyBytes {
		return nil, errors.New("authority policy file is invalid")
	}
	return raw, nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
