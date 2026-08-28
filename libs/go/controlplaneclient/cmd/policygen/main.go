// Команда policygen материализует единственную web-first machine policy для
// внутренних RPC. Списки операций импортируются из того же закрытого реестра,
// который используют generated clients.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

const (
	resolverMethod   = "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof"
	controlPlaneID   = "control-plane"
	controlPlanePeer = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	controlPlaneTLS  = "control-plane.kodex-system.svc.cluster.local"
	internalAudience = "urn:kodex:internal-rpc:control-plane"
)

type document struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         policy `json:"policy"`
}

type policy struct {
	TrustDomain             string     `json:"trust_domain"`
	DefaultDecision         string     `json:"default_decision"`
	TokenTTLSeconds         int64      `json:"token_ttl_seconds"`
	AllowedClockSkewSeconds int64      `json:"allowed_clock_skew_seconds"`
	MaxCompactJWSBytes      int        `json:"max_compact_jws_bytes"`
	ProofProducers          []producer `json:"authority_proof_producers"`
	OperationBindings       []binding  `json:"operation_bindings"`
}

type producer struct {
	ProducerID                         string   `json:"producer_id"`
	CallerWorkloadID                   string   `json:"caller_workload_id"`
	CallerSPIFFEID                     string   `json:"caller_spiffe_id"`
	OwnerWorkloadID                    string   `json:"owner_workload_id"`
	OwnerSPIFFEID                      string   `json:"owner_spiffe_id"`
	FullMethod                         string   `json:"full_method"`
	TLSServerName                      string   `json:"tls_server_name"`
	TransportTrustBundleID             string   `json:"transport_trust_bundle_id"`
	ApplicationCredential              string   `json:"application_credential"`
	ApplicationCredentialMetadata      string   `json:"application_credential_metadata"`
	ApplicationCredentialIssuer        string   `json:"application_credential_issuer"`
	ApplicationCredentialAudience      string   `json:"application_credential_audience"`
	ApplicationCredentialTrustBundleID string   `json:"application_credential_trust_bundle_id"`
	AuthorityProofIssuer               string   `json:"authority_proof_issuer"`
	AuthorityProofAudience             string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID        string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds        int64    `json:"authority_proof_max_age_seconds"`
	DeadlineMilliseconds               int64    `json:"deadline_milliseconds"`
	MaxAttempts                        int      `json:"max_attempts"`
	RetryableGRPCCodes                 []string `json:"retryable_grpc_codes"`
	IdempotencyScope                   string   `json:"idempotency_scope"`
	AuthoritySources                   []string `json:"authority_sources"`
	AllowedOperationIDs                []string `json:"allowed_operation_ids"`
	ServerResolvedFields               []string `json:"server_resolved_fields"`
}

type binding struct {
	OperationID         string    `json:"operation_id"`
	CallerWorkloadID    string    `json:"caller_workload_id"`
	CallerSPIFFEID      string    `json:"caller_spiffe_id"`
	Issuer              string    `json:"issuer"`
	TargetWorkloadID    string    `json:"target_workload_id"`
	TargetSPIFFEID      string    `json:"target_spiffe_id"`
	Audience            string    `json:"audience"`
	FullMethod          string    `json:"full_method"`
	TargetTLSServerName string    `json:"target_tls_server_name"`
	TargetTrustBundleID string    `json:"target_trust_bundle_id"`
	Permission          string    `json:"permission"`
	ProofProducerID     string    `json:"authority_proof_producer_id"`
	AuthoritySources    []string  `json:"authority_sources"`
	ProjectRequired     bool      `json:"project_required"`
	LocalCaller         localPeer `json:"local_caller"`
	LocalTarget         localPeer `json:"local_target"`
}

type localPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}

type profile struct {
	ProducerID, WorkloadID, Credential, CredentialIssuer, CredentialAudience, CredentialTrust string
	Operations                                                                                map[string]string
	ProjectRequired                                                                           map[string]struct{}
	AuthoritySources                                                                          []string
}

func main() {
	output := flag.String("output", "", "path to the resulting authority-policy.json")
	oidcIssuer := flag.String("oidc-issuer", "", "exact OIDC issuer for this installation")
	oidcAudience := flag.String("oidc-audience", "kodex-control-api", "exact Control Center OIDC audience")
	flag.Parse()
	if *output == "" || *oidcIssuer == "" || *oidcAudience == "" {
		fatal("output path, OIDC issuer and audience are required")
	}
	profiles := []profile{
		{
			ProducerID: "control-plane.oidc", WorkloadID: "control-api-gateway", Credential: "OIDC_BEARER",
			CredentialIssuer: *oidcIssuer, CredentialAudience: *oidcAudience,
			CredentialTrust: "kodex-oidc-signers-g1", Operations: controlplaneclient.ControlAPIGatewayOperations(),
			ProjectRequired: controlplaneclient.ControlAPIGatewayProjectRequiredOperations(), AuthoritySources: []string{"OIDC_SESSION", "DOMAIN_STATE"},
		},
		worker("runtime-controller", "control-plane.runtime-controller", controlplaneclient.RuntimeOperations()),
		worker("automation-scheduler", "control-plane.automation", controlplaneclient.AutomationSchedulerOperations()),
		worker("integration-gateway", "control-plane.integration-gateway", controlplaneclient.IntegrationGatewayOperations()),
		worker("interaction-gateway", "control-plane.interaction-gateway", controlplaneclient.InteractionGatewayOperations()),
		worker("role-image-builder", "control-plane.role-image-builder", controlplaneclient.RoleImageBuilderOperations()),
		worker("image-admission", "control-plane.image-admission", controlplaneclient.ImageAdmissionOperations()),
		worker("image-promotion", "control-plane.image-promotion", controlplaneclient.ImagePromotionOperations()),
	}
	value := document{Version: 1, PolicyRevision: 37, Policy: policy{
		TrustDomain: "kodex.local", DefaultDecision: "DENY", TokenTTLSeconds: 30,
		AllowedClockSkewSeconds: 5, MaxCompactJWSBytes: 8192,
	}}
	for _, item := range profiles {
		peer := workloadSPIFFE(item.WorkloadID)
		operations := sortedKeys(item.Operations)
		value.Policy.ProofProducers = append(value.Policy.ProofProducers, producer{
			ProducerID: item.ProducerID, CallerWorkloadID: item.WorkloadID, CallerSPIFFEID: peer,
			OwnerWorkloadID: controlPlaneID, OwnerSPIFFEID: controlPlanePeer, FullMethod: resolverMethod,
			TLSServerName: controlPlaneTLS, TransportTrustBundleID: "kodex-internal-ca-g1",
			ApplicationCredential: item.Credential, ApplicationCredentialMetadata: "authorization",
			ApplicationCredentialIssuer: item.CredentialIssuer, ApplicationCredentialAudience: item.CredentialAudience,
			ApplicationCredentialTrustBundleID: item.CredentialTrust,
			AuthorityProofIssuer:               controlPlanePeer,
			AuthorityProofAudience:             "urn:kodex:internal-rpc-authority-issuer:" + item.WorkloadID,
			AuthorityProofTrustBundleID:        "control-plane-authority-proof-g1", AuthorityProofMaxAgeSeconds: 15,
			DeadlineMilliseconds: 2000, MaxAttempts: 2, RetryableGRPCCodes: []string{"UNAVAILABLE", "DEADLINE_EXCEEDED"},
			IdempotencyScope: "credential-subject-digest+caller-workload+operation+idempotency-key",
			AuthoritySources: item.AuthoritySources, AllowedOperationIDs: operations,
			ServerResolvedFields: []string{"actor", "tenant", "project", "ownership", "provenance"},
		})
		for _, operationID := range operations {
			_, projectRequired := item.ProjectRequired[operationID]
			value.Policy.OperationBindings = append(value.Policy.OperationBindings, binding{
				OperationID: operationID, CallerWorkloadID: item.WorkloadID, CallerSPIFFEID: peer, Issuer: peer,
				TargetWorkloadID: controlPlaneID, TargetSPIFFEID: controlPlanePeer, Audience: internalAudience,
				FullMethod: item.Operations[operationID], TargetTLSServerName: controlPlaneTLS,
				TargetTrustBundleID: "kodex-internal-ca-g1", Permission: operationID,
				ProofProducerID: item.ProducerID, AuthoritySources: item.AuthoritySources, ProjectRequired: projectRequired,
				LocalCaller: localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000},
				LocalTarget: localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000},
			})
		}
	}
	sort.Slice(value.Policy.ProofProducers, func(left, right int) bool {
		return value.Policy.ProofProducers[left].ProducerID < value.Policy.ProofProducers[right].ProducerID
	})
	sort.Slice(value.Policy.OperationBindings, func(left, right int) bool {
		return value.Policy.OperationBindings[left].OperationID < value.Policy.OperationBindings[right].OperationID
	})
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fatal("encode policy: %v", err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(*output, raw, 0o600); err != nil {
		fatal("write policy: %v", err)
	}
}

func worker(workloadID, producerID string, operations map[string]string) profile {
	return profile{
		ProducerID: producerID, WorkloadID: workloadID, Credential: "PLATFORM_WORKER_GRANT",
		CredentialIssuer:   "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/" + workloadID,
		CredentialAudience: "urn:kodex:platform-worker:" + workloadID,
		CredentialTrust:    workloadID + "-platform-worker-grants-g1", Operations: operations,
		ProjectRequired: map[string]struct{}{}, AuthoritySources: []string{"DOMAIN_STATE"},
	}
}

func workloadSPIFFE(workloadID string) string {
	return "spiffe://kodex.local/ns/kodex-system/sa/" + workloadID
}

func sortedKeys(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key := range values {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func fatal(format string, values ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "authority policy generation failed: "+format+"\n", values...)
	os.Exit(1)
}
