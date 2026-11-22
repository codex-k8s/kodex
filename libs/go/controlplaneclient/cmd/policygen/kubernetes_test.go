package main

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
)

// TestManagedKubernetesProfilesCoverActualRegistries rejects drift between executable client maps and request binding policy.
// TestManagedKubernetesProfilesCoverActualRegistries отклоняет расхождение исполняемых client maps и политики привязки запроса.
func TestManagedKubernetesProfilesCoverActualRegistries(t *testing.T) {
	want := requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}
	seen := make(map[string]string)
	for _, operations := range []map[string]string{
		controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.RuntimeOperations(),
		controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.SecretBrokerOperations(),
		controlplaneclient.ManagedKubernetesBrokerOperations(),
		controlplaneclient.ArtifactRetentionCaptureOperations(),
	} {
		for operation, method := range operations {
			if !strings.Contains(method, ".ManagedKubernetes") {
				continue
			}
			if previous := seen[operation]; previous != "" {
				t.Fatalf("Kubernetes operation assigned to multiple caller profiles: %s", operation)
			}
			seen[operation] = method
			if result, known := managedKubernetesRequestProfile(operation); !known || result != want || operationRequestProfile(operation, method) != want {
				t.Fatalf("Kubernetes request profile does not bind the full owner tuple: %s", operation)
			}
		}
	}
	if len(seen) != 56 {
		t.Fatalf("Kubernetes operation coverage changed: got %d", len(seen))
	}
	for _, unknown := range []string{"kubernetes.exec", "platform.runtime.kubernetes.invocation.read.foreign", "platform.runtime.kubernetes.claim", "platform.integrations.kubernetes.port_forward.open", "platform.artifacts.kubernetes_capture.finalize"} {
		if _, known := managedKubernetesRequestProfile(unknown); known {
			t.Fatalf("unregistered Kubernetes operation admitted: %s", unknown)
		}
	}
}

// TestManagedKubernetesFileProfilesUseExactOwnerMethods keeps metadata discovery public and materialization broker-only.
// TestManagedKubernetesFileProfilesUseExactOwnerMethods сохраняет public metadata discovery и broker-only materialization.
func TestManagedKubernetesFileProfilesUseExactOwnerMethods(t *testing.T) {
	public := controlplaneclient.ControlAPIGatewayOperations()
	broker := controlplaneclient.SecretBrokerOperations()
	for operation, method := range map[string]string{
		"platform.integrations.kubernetes.file.list": "/controlplane.v1.ManagedKubernetesFileService/ListManagedKubernetesFileInputs",
		"platform.integrations.kubernetes.file.get":  "/controlplane.v1.ManagedKubernetesFileService/GetManagedKubernetesFileInput",
	} {
		if public[operation] != method || broker[operation] != "" {
			t.Fatal("FILE metadata method escaped its public owner profile", operation)
		}
	}
	if public[controlplaneclient.KubernetesFileCleanupRepairOperation] != "" || broker[controlplaneclient.KubernetesFileCleanupRepairOperation] != "" {
		t.Fatal("unpublished FILE cleanup repair entered an active caller registry")
	}
	const prepare = "platform.runtime.kubernetes.file.prepare"
	if broker[prepare] != "/controlplane.v1.ManagedKubernetesOwnerService/PrepareManagedKubernetesFileInput" || public[prepare] != "" || controlplaneclient.IntegrationGatewayOperations()[prepare] != "" {
		t.Fatal("FILE materialization escaped protected broker profile")
	}
}

// TestManagedKubernetesBrokerProfilePinsCredentialAndTarget verifies all layers rather than only matching method names.
// TestManagedKubernetesBrokerProfilePinsCredentialAndTarget проверяет все слои, а не только совпадение имён методов.
func TestManagedKubernetesBrokerProfilePinsCredentialAndTarget(t *testing.T) {
	p := managedKubernetesBrokerProfile()
	if p.WorkloadID != "integration-gateway" || p.ProducerID != "secret-broker.managed-kubernetes" ||
		p.Credential != "PLATFORM_WORKER_GRANT" ||
		p.CredentialIssuer != "https://control-plane.kodex-system.svc.cluster.local/authority/platform-worker/integration-gateway" ||
		p.CredentialAudience != "urn:kodex:platform-worker:integration-gateway" ||
		p.CredentialTrust != "integration-gateway-platform-worker-grants-g1" ||
		p.TargetWorkloadID != secretBrokerID || p.TargetSPIFFEID != secretBrokerPeer ||
		p.TargetAudience != secretBrokerAudience || p.TargetTLSServerName != secretBrokerTLS ||
		p.Continuation != nil || len(p.ProjectRequired) != 0 || len(p.Operations) != 4 ||
		len(p.AuthoritySources) != 1 || p.AuthoritySources[0] != "DOMAIN_STATE" {
		t.Fatal("Kubernetes broker profile lost its exact protected transport boundary")
	}
}

// TestManagedKubernetesRuntimeReadRemainsControllerOnly excludes gateway and broker impersonation of an execution lease.
// TestManagedKubernetesRuntimeReadRemainsControllerOnly исключает подмену execution lease шлюзом либо broker.
func TestManagedKubernetesRuntimeReadRemainsControllerOnly(t *testing.T) {
	const operation = "platform.runtime.kubernetes.invocation.read"
	const method = "/controlplane.v1.ManagedKubernetesOwnerService/ReadManagedKubernetesInvocation"
	if controlplaneclient.RuntimeOperations()[operation] != method {
		t.Fatal("runtime result reader is missing from its exact controller profile")
	}
	for _, operations := range []map[string]string{controlplaneclient.ControlAPIGatewayOperations(), controlplaneclient.IntegrationGatewayOperations(), controlplaneclient.SecretBrokerOperations(), controlplaneclient.ManagedKubernetesBrokerOperations()} {
		for id, fullMethod := range operations {
			if id == operation || fullMethod == method {
				t.Fatal("runtime result reader leaked to a different caller profile")
			}
		}
	}
}
