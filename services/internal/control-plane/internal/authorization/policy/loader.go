// Package policy загружает versioned producer policy control-plane.
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
	producerID         = "control-plane.oidc"
	resolverFullMethod = "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof"
)

// Loaded содержит только server-owned proof producer view.
type Loaded struct {
	Revision                     uint64
	Digest                       string
	Issuer                       string
	ProofAudience                string
	AuthorizationContextAudience string
	CallerWorkload               string
	CallerSPIFFEID               string
	ApplicationIssuer            string
	ApplicationAudience          string
	ApplicationCredential        string
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

// Load связывает operations с одним exact proof producer.
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
	var producer proofProducer
	matches := 0
	for _, candidate := range policy.ProofProducers {
		if candidate.ID == producerID {
			producer = candidate
			matches++
		}
	}
	if matches != 1 ||
		producer.OwnerWorkload != "control-plane" ||
		producer.OwnerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane" ||
		producer.FullMethod != resolverFullMethod ||
		producer.CallerWorkload != "control-api-gateway" ||
		producer.CallerSPIFFEID != "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway" ||
		producer.Credential != "OIDC_BEARER" ||
		producer.CredentialMetadata != "authorization" ||
		producer.CredentialIssuer == "" ||
		producer.CredentialAudience == "" ||
		producer.ProofIssuer != producer.OwnerSPIFFEID ||
		producer.ProofAudience == "" {
		return Loaded{}, errors.New("control-plane proof producer is invalid")
	}
	allowed := make(map[string]struct{}, len(producer.AllowedOperationIDs))
	for _, operationID := range producer.AllowedOperationIDs {
		if _, duplicate := allowed[operationID]; duplicate {
			return Loaded{}, errors.New("proof producer operation is duplicated")
		}
		allowed[operationID] = struct{}{}
	}
	operations := make(map[string]authorityservice.Operation, len(expected))
	for _, candidate := range policy.Bindings {
		if candidate.ProofProducerID != producerID {
			continue
		}
		expectedMethod, ok := expected[candidate.OperationID]
		_, permitted := allowed[candidate.OperationID]
		if !ok || !permitted ||
			candidate.FullMethod != expectedMethod ||
			candidate.Permission == "" ||
			candidate.Audience == "" ||
			candidate.CallerWorkload != producer.CallerWorkload ||
			candidate.CallerSPIFFEID != producer.CallerSPIFFEID ||
			candidate.TargetWorkload != producer.OwnerWorkload ||
			candidate.TargetSPIFFEID != producer.OwnerSPIFFEID {
			return Loaded{}, errors.New("control-plane operation binding is invalid")
		}
		if _, duplicate := operations[candidate.OperationID]; duplicate {
			return Loaded{}, errors.New("control-plane operation binding is ambiguous")
		}
		operations[candidate.OperationID] = authorityservice.Operation{
			FullMethod:      candidate.FullMethod,
			Permission:      candidate.Permission,
			ProjectRequired: candidate.ProjectRequired,
			TenantOwnerOnly: candidate.OperationID == "control.project.create",
		}
		if candidate.Audience != "urn:mattercodex:internal-rpc:control-plane" {
			return Loaded{}, errors.New("control-plane operation audience is invalid")
		}
	}
	if len(operations) != len(expected) || len(allowed) != len(expected) {
		return Loaded{}, errors.New("control-plane proof producer operation set is incomplete")
	}
	return Loaded{
		Revision:                     parsed.Revision,
		Digest:                       digest,
		Issuer:                       producer.ProofIssuer,
		ProofAudience:                producer.ProofAudience,
		AuthorizationContextAudience: "urn:mattercodex:internal-rpc:control-plane",
		CallerWorkload:               producer.CallerWorkload,
		CallerSPIFFEID:               producer.CallerSPIFFEID,
		ApplicationIssuer:            producer.CredentialIssuer,
		ApplicationAudience:          producer.CredentialAudience,
		ApplicationCredential:        producer.Credential,
		Operations:                   operations,
	}, nil
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
