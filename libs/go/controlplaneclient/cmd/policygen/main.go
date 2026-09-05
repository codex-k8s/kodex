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
	"strings"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

const (
	resolverMethod       = "/internalrpcauthority.v1.AuthorityProofResolverService/ResolveAuthorityProof"
	controlPlaneID       = "control-plane"
	controlPlanePeer     = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	controlPlaneTLS      = "control-plane.kodex-system.svc.cluster.local"
	controlPlaneAudience = "urn:kodex:internal-rpc:control-plane"
	secretBrokerID       = "secret-broker"
	secretBrokerPeer     = "spiffe://kodex.local/ns/kodex-system/sa/secret-broker"
	secretBrokerTLS      = "secret-broker.kodex-system.svc.cluster.local"
	secretBrokerAudience = "urn:kodex:internal-rpc:secret-broker"
	sttID                = "stt-tts-service"
	sttPeer              = "spiffe://kodex.local/ns/kodex-system/sa/stt-tts-service"
	sttTLS               = "stt-tts-service.kodex-system.svc.cluster.local"
	sttAudience          = "urn:kodex:internal-rpc:stt-tts-service"
	sttTranscribeMethod  = "/stt.v1.SpeechToTextService/Transcribe"
)

type document struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         policy `json:"policy"`
}

type policy struct {
	AuthorityABIVersion     uint32     `json:"authority_abi_version"`
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
	OperationID         string               `json:"operation_id"`
	CallerWorkloadID    string               `json:"caller_workload_id"`
	CallerSPIFFEID      string               `json:"caller_spiffe_id"`
	Issuer              string               `json:"issuer"`
	TargetWorkloadID    string               `json:"target_workload_id"`
	TargetSPIFFEID      string               `json:"target_spiffe_id"`
	Audience            string               `json:"audience"`
	FullMethod          string               `json:"full_method"`
	TargetTLSServerName string               `json:"target_tls_server_name"`
	TargetTrustBundleID string               `json:"target_trust_bundle_id"`
	Permission          string               `json:"permission"`
	ProofProducerID     string               `json:"authority_proof_producer_id,omitempty"`
	AuthoritySources    []string             `json:"authority_sources"`
	ProjectRequired     bool                 `json:"project_required"`
	LocalCaller         localPeer            `json:"local_caller"`
	LocalTarget         localPeer            `json:"local_target"`
	RequestProfile      requestProfile       `json:"request_profile"`
	Continuation        *continuationProfile `json:"continuation,omitempty"`
}

type requestProfile struct {
	Mode        string `json:"mode"`
	Resource    string `json:"resource"`
	Version     string `json:"version"`
	Attempt     string `json:"attempt"`
	Idempotency string `json:"idempotency"`
}

type continuationProfile struct {
	ParentOperationID string `json:"parent_operation_id"`
	ParentFullMethod  string `json:"parent_full_method"`
}

type localPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}

type profile struct {
	ProducerID, WorkloadID, Credential, CredentialIssuer, CredentialAudience, CredentialTrust  string
	TargetWorkloadID, TargetSPIFFEID, TargetAudience, TargetTLSServerName, TargetTrustBundleID string
	Operations                                                                                 map[string]string
	ProjectRequired                                                                            map[string]struct{}
	AuthoritySources                                                                           []string
	Continuation                                                                               *continuationProfile
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
		{
			ProducerID: "control-plane.oidc-stt", WorkloadID: "control-api-gateway", Credential: "OIDC_BEARER",
			CredentialIssuer: *oidcIssuer, CredentialAudience: *oidcAudience, CredentialTrust: "kodex-oidc-signers-g1",
			Operations:       controlplaneclient.STTGatewayOperations(),
			AuthoritySources: []string{"OIDC_SESSION", "DOMAIN_STATE"}, TargetWorkloadID: sttID,
			TargetSPIFFEID: sttPeer, TargetAudience: sttAudience, TargetTLSServerName: sttTLS,
		},
		worker("runtime-controller", "control-plane.runtime-controller", controlplaneclient.RuntimeOperations()),
		worker("automation-scheduler", "control-plane.automation", controlplaneclient.AutomationSchedulerOperations()),
		worker("session-archive", "control-plane.session-archive", controlplaneclient.SessionArchiveOperations()),
		worker("integration-gateway", "control-plane.integration-gateway", controlplaneclient.IntegrationGatewayOperations()),
		worker("interaction-gateway", "control-plane.interaction-gateway", controlplaneclient.InteractionGatewayOperations()),
		worker("email-bridge", "control-plane.email-bridge", controlplaneclient.EmailBridgeOperations()),
		worker("role-image-builder", "control-plane.role-image-builder", controlplaneclient.RoleImageBuilderOperations()),
		worker("image-admission", "control-plane.image-admission", controlplaneclient.ImageAdmissionOperations()),
		worker("image-promotion", "control-plane.image-promotion", controlplaneclient.ImagePromotionOperations()),
		worker("secret-broker", "control-plane.secret-broker", controlplaneclient.SecretBrokerOperations()),
		targetedWorker(
			controlPlaneID,
			"secret-broker.provider-credential-materializer",
			controlplaneclient.ProviderCredentialMaterializerOperations(),
			secretBrokerID,
			secretBrokerPeer,
			secretBrokerAudience,
			secretBrokerTLS,
		),
		delegatedTargetedWorker(
			"runtime-controller",
			"secret-broker.runtime-credential-projection",
			controlplaneclient.RuntimeCredentialProjectionOperations(),
			secretBrokerID,
			secretBrokerPeer,
			secretBrokerAudience,
			secretBrokerTLS,
		),
		continuationWorker("control-plane.stt-policy", controlplaneclient.STTPolicyProjectionOperations(), controlPlaneID, controlPlanePeer, controlPlaneAudience, controlPlaneTLS),
		continuationWorker("secret-broker.stt-credential", controlplaneclient.STTCredentialProjectionOperations(), secretBrokerID, secretBrokerPeer, secretBrokerAudience, secretBrokerTLS),
	}
	profiles = append(profiles, profile{
		ProducerID: "control-plane.oidc-secret-draft", WorkloadID: "control-api-gateway", Credential: "OIDC_BEARER",
		CredentialIssuer: *oidcIssuer, CredentialAudience: *oidcAudience, CredentialTrust: "kodex-oidc-signers-g1",
		Operations: controlplaneclient.SecretDraftGatewayOperations(), AuthoritySources: []string{"OIDC_SESSION", "DOMAIN_STATE"},
		TargetWorkloadID: secretBrokerID, TargetSPIFFEID: secretBrokerPeer, TargetAudience: secretBrokerAudience, TargetTLSServerName: secretBrokerTLS,
	})
	value := document{Version: 1, PolicyRevision: 60, Policy: policy{
		AuthorityABIVersion: 2,
		TrustDomain:         "kodex.local", DefaultDecision: "DENY", TokenTTLSeconds: 30,
		AllowedClockSkewSeconds: 5, MaxCompactJWSBytes: 8192,
	}}
	for _, item := range profiles {
		peer := workloadSPIFFE(item.WorkloadID)
		targetWorkloadID := item.TargetWorkloadID
		if targetWorkloadID == "" {
			targetWorkloadID = controlPlaneID
		}
		targetSPIFFEID := item.TargetSPIFFEID
		if targetSPIFFEID == "" {
			targetSPIFFEID = controlPlanePeer
		}
		targetAudience := item.TargetAudience
		if targetAudience == "" {
			targetAudience = controlPlaneAudience
		}
		targetTLSServerName := item.TargetTLSServerName
		if targetTLSServerName == "" {
			targetTLSServerName = controlPlaneTLS
		}
		targetTrustBundleID := item.TargetTrustBundleID
		if targetTrustBundleID == "" {
			targetTrustBundleID = "kodex-internal-ca-g1"
		}
		operations := sortedKeys(item.Operations)
		if item.Continuation == nil {
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
		}
		for _, operationID := range operations {
			_, projectRequired := item.ProjectRequired[operationID]
			proofProducerID := item.ProducerID
			if item.Continuation != nil {
				proofProducerID = ""
			}
			value.Policy.OperationBindings = append(value.Policy.OperationBindings, binding{
				OperationID: operationID, CallerWorkloadID: item.WorkloadID, CallerSPIFFEID: peer, Issuer: peer,
				TargetWorkloadID: targetWorkloadID, TargetSPIFFEID: targetSPIFFEID, Audience: targetAudience,
				FullMethod: item.Operations[operationID], TargetTLSServerName: targetTLSServerName,
				TargetTrustBundleID: targetTrustBundleID, Permission: permissionForOperation(operationID),
				ProofProducerID: proofProducerID, AuthoritySources: item.AuthoritySources, ProjectRequired: projectRequired,
				LocalCaller:    localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000},
				LocalTarget:    localPeer{UID: 10001, PrimaryGID: 10001, SharedFSGID: 29000},
				RequestProfile: operationRequestProfile(operationID, item.Operations[operationID]),
				Continuation:   item.Continuation,
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

func continuationWorker(producerID string, operations map[string]string, targetID, targetPeer, targetAudience, targetTLS string) profile {
	result := targetedWorker(sttID, producerID, operations, targetID, targetPeer, targetAudience, targetTLS)
	result.AuthoritySources = []string{"DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"}
	result.ProjectRequired = map[string]struct{}{}
	result.Continuation = &continuationProfile{ParentOperationID: "platform.stt.transcribe", ParentFullMethod: sttTranscribeMethod}
	return result
}

func requiredProjects(operations map[string]string) map[string]struct{} {
	result := make(map[string]struct{}, len(operations))
	for operation := range operations {
		result[operation] = struct{}{}
	}
	return result
}

func operationRequestProfile(operationID, fullMethod string) requestProfile {
	mode := "UNARY_PROTO_SHA256"
	if strings.HasPrefix(operationID, "platform.runtime-secret-drafts.") {
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
	}
	if operationID == "platform.command.runtime-secret-drafts.save" {
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	}
	if fullMethod == sttTranscribeMethod || strings.Contains(fullMethod, "/Upload") || strings.Contains(fullMethod, "/DownloadArtifact") {
		mode = "STREAM_SESSION"
	}
	required := func(value bool) string {
		if value {
			return "REQUIRED"
		}
		return "FORBIDDEN"
	}
	switch operationID {
	case "platform.stt.model-catalog.get", "platform.email.configuration.report",
		"platform.query.email-mailbox.configurations.list", "platform.query.email-mailbox.configurations.preview", "platform.query.email-mailbox.credentials.list":
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
	case "platform.query.email-mailbox.configurations.get", "platform.query.email-mailbox.credential-receipts.get":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
	case "platform.command.email-mailbox.drafts.create":
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.command.email-mailbox.drafts.save", "platform.command.email-mailbox.drafts.validate", "platform.command.email-mailbox.drafts.publish", "platform.command.email-mailbox.drafts.discard",
		"platform.command.email-mailbox.configurations.bind", "platform.command.email-mailbox.configurations.unbind":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.email.effect-receipts.report":
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.command.email-effects.reconcile":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.stt.transcribe":
		return requestProfile{Mode: mode, Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.stt.policy.resolve", "platform.stt.credential.project":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "REQUIRED", Attempt: "FORBIDDEN", Idempotency: "REQUIRED"}
	case "platform.provider-credentials.device-authorize.start":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "REQUIRED", Idempotency: "REQUIRED"}
	case "platform.provider-credentials.device-authorize.get":
		return requestProfile{Mode: mode, Resource: "REQUIRED", Version: "FORBIDDEN", Attempt: "REQUIRED", Idempotency: "FORBIDDEN"}
	}
	resource := operationID == "platform.command.runtime-secret-drafts.impact.prepare" || operationID == "platform.query.runtime-revisions.diff" || strings.Contains(operationID, ".get") || strings.Contains(operationID, ".update") || strings.Contains(operationID, ".delete") ||
		strings.Contains(operationID, ".save") || strings.Contains(operationID, ".discard") ||
		strings.Contains(operationID, ".validate") || strings.Contains(operationID, ".publish") || strings.Contains(operationID, ".rebind") ||
		strings.Contains(operationID, ".detach") || strings.Contains(operationID, ".copy") || strings.Contains(operationID, "device-")
	version := strings.HasPrefix(operationID, "platform.command.") && !strings.Contains(operationID, ".create") && !strings.Contains(operationID, ".upload")
	attempt := strings.Contains(operationID, ".claim") || strings.Contains(operationID, ".complete") || strings.Contains(operationID, ".recover")
	idempotency := strings.HasPrefix(operationID, "platform.command.") || strings.Contains(operationID, ".claim") || strings.Contains(operationID, ".complete")
	return requestProfile{Mode: mode, Resource: required(resource), Version: required(version), Attempt: required(attempt), Idempotency: required(idempotency)}
}

func permissionForOperation(operationID string) string {
	permissions := map[string]string{
		"platform.stt.model-catalog.get":                       "system.configuration.manage",
		"platform.stt.transcribe":                              "stt.transcribe",
		"platform.command.agents.avatar.upload":                "agent.avatar.manage",
		"platform.command.organization-artifacts.upload":       "platform.command.artifacts.upload",
		"platform.command.organization-attachment-sets.create": "platform.command.attachment-sets.create-draft",
		"platform.query.runtime-secrets.list":                  "secret.view",
		"platform.query.runtime-secrets.get":                   "secret.view",
		"platform.command.runtime-secrets.create":              "secret.create",
		"platform.command.runtime-secrets.rotate":              "secret.rotate",
		"platform.command.runtime-secrets.reveal":              "secret.reveal",
		"platform.command.runtime-secrets.revoke":              "secret.revoke",
		"platform.runtime-secrets.materialization.recover":     "platform.runtime-secrets.operations.recover",
	}
	if permission := permissions[operationID]; permission != "" {
		return permission
	}
	return operationID
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

func targetedWorker(
	workloadID string,
	producerID string,
	operations map[string]string,
	targetWorkloadID string,
	targetSPIFFEID string,
	targetAudience string,
	targetTLSServerName string,
) profile {
	result := worker(workloadID, producerID, operations)
	result.TargetWorkloadID = targetWorkloadID
	result.TargetSPIFFEID = targetSPIFFEID
	result.TargetAudience = targetAudience
	result.TargetTLSServerName = targetTLSServerName
	result.TargetTrustBundleID = "kodex-internal-ca-g1"
	return result
}

func delegatedTargetedWorker(
	workloadID string,
	producerID string,
	operations map[string]string,
	targetWorkloadID string,
	targetSPIFFEID string,
	targetAudience string,
	targetTLSServerName string,
) profile {
	result := targetedWorker(workloadID, producerID, operations, targetWorkloadID, targetSPIFFEID, targetAudience, targetTLSServerName)
	result.AuthoritySources = []string{"DOMAIN_STATE", "OIDC_SESSION", "RUNTIME_EXECUTION"}
	result.ProjectRequired = make(map[string]struct{}, len(operations))
	for operation := range operations {
		if operation != "platform.runtime.credentials.system-assistant.materialize" {
			result.ProjectRequired[operation] = struct{}{}
		}
	}
	return result
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
