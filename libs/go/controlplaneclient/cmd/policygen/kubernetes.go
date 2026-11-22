package main

import "github.com/codex-k8s/kodex/libs/go/controlplaneclient"

// managedKubernetesBrokerProfile confines native transport to the integration gateway and the exact broker identity.
// It retains the application grant, owner proof and local issuer instead of granting authority from a target URL.
// managedKubernetesBrokerProfile ограничивает native transport integration gateway и точной broker identity.
// Сохраняет application grant, owner proof и local issuer; URL цели не выдаёт полномочий.
func managedKubernetesBrokerProfile() profile {
	return targetedWorker("integration-gateway", "secret-broker.managed-kubernetes",
		controlplaneclient.ManagedKubernetesBrokerOperations(), secretBrokerID, secretBrokerPeer, secretBrokerAudience, secretBrokerTLS)
}

// managedKubernetesRequestProfile explicitly binds the complete unary request, including nested leases and mutations.
// Separate authority headers cannot replace owner validation of original actor, resource, OCC and idempotency.
// Unknown operations are not registered by this classifier and retain the ordinary generator handling.
// managedKubernetesRequestProfile явно связывает полный unary request, включая вложенные leases и mutations.
// Отдельные authority headers не заменяют owner validation исходного actor, ресурса, OCC и идемпотентности.
// Неизвестные операции не регистрируются этим классификатором и остаются в обычном пути генератора.
func managedKubernetesRequestProfile(operation string) (requestProfile, bool) {
	switch operation {
	case "platform.artifacts.kubernetes-capture.finalize", "platform.artifacts.kubernetes-capture.get",
		"platform.artifacts.kubernetes-capture.renew", "platform.artifacts.kubernetes-capture.readiness":
		return requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}, true
	case controlplaneclient.KubernetesFileCleanupRepairOperation, "platform.integrations.kubernetes.file.list", "platform.integrations.kubernetes.file.get", "platform.runtime.kubernetes.file.prepare",
		"platform.integrations.kubernetes.capture.cleanup.repair", "platform.integrations.kubernetes.capture.get", "platform.integrations.kubernetes.capture.repair", "platform.integrations.kubernetes.capture.list",
		"platform.integrations.kubernetes.output.read", "kubernetes.execute", "kubernetes.stream.exchange", "kubernetes.cancel", "kubernetes.readiness",
		"platform.integrations.kubernetes.port-forward.open", "platform.integrations.kubernetes.port-forward.get", "platform.integrations.kubernetes.port-forward.list", "platform.integrations.kubernetes.port-forward.close",
		"platform.integrations.kubernetes.tool.list", "platform.integrations.kubernetes.tool.get", "platform.integrations.kubernetes.tool.stop",
		"platform.integrations.kubernetes.input.list", "platform.integrations.kubernetes.input.get", "platform.integrations.kubernetes.input.submit", "platform.integrations.kubernetes.input.stop",
		"platform.integrations.kubernetes.material.begin", "platform.integrations.kubernetes.material.get", "platform.integrations.kubernetes.material.cancel",
		"platform.integrations.kubernetes.setup.begin", "platform.integrations.kubernetes.setup.get", "platform.integrations.kubernetes.setup.cancel",
		"platform.runtime.kubernetes.resolve", "platform.runtime.kubernetes.children.prepare",
		"platform.runtime.kubernetes.effect.begin", "platform.runtime.kubernetes.effect.complete",
		"platform.runtime.kubernetes.stream.open", "platform.runtime.kubernetes.stream.close",
		"platform.runtime.kubernetes.frame.begin", "platform.runtime.kubernetes.frame.complete",
		"platform.runtime.kubernetes.health.begin", "platform.runtime.kubernetes.health.complete",
		"platform.runtime.kubernetes.setup.resolve", "platform.runtime.kubernetes.setup.claim", "platform.runtime.kubernetes.setup.complete",
		"platform.runtime.kubernetes.readiness", "platform.runtime.kubernetes.invocation.read",
		"platform.runtime.kubernetes.renew", "platform.runtime.kubernetes.complete", "platform.runtime.kubernetes.read", "platform.runtime.kubernetes.cancel",
		"platform.runtime.kubernetes.output.accept", "platform.runtime.kubernetes.output.read", "platform.runtime.kubernetes.input.claim",
		"platform.runtime.integrations.delivery.check":
		return requestProfile{Mode: "UNARY_PROTO_SHA256", Resource: "FORBIDDEN", Version: "FORBIDDEN", Attempt: "FORBIDDEN", Idempotency: "FORBIDDEN"}, true
	default:
		return requestProfile{}, false
	}
}
