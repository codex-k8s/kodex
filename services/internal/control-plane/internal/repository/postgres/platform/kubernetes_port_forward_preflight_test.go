package platform

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"testing"
)

// TestPortForwardPreflightRequiresOwnerDerivedUnattemptedLegacyProfile proves a caller hint cannot fail an unknown/valid/attempted operation as unsupported.
// TestPortForwardPreflightRequiresOwnerDerivedUnattemptedLegacyProfile доказывает, что caller hint не объявляет unknown/valid/attempted operation unsupported.
func TestPortForwardPreflightRequiresOwnerDerivedUnattemptedLegacyProfile(t *testing.T) {
	for _, variant := range []string{"valid", "spdy", "unknown", "nil-node", "foreign-op", "attempted", "open-gate", "output", "stream", "not-current", "not-ready"} {
		t.Run(variant, func(t *testing.T) {
			op := "kubernetes.port-forward"
			original := []entity.ManagedKubernetesChildEffect{{Operation: op, State: "READY", NodeTransport: &value.ManagedKubernetesNodeTransport{Protocol: managedkubernetes.PortForwardWebSocketV4}}}
			facts := []kubernetesInvocationChildResult{{operation: op, current: true}}
			switch variant {
			case "spdy":
				original[0].NodeTransport.Protocol = managedkubernetes.PortForwardSPDYV1
			case "unknown":
				original[0].NodeTransport.Protocol = "UNKNOWN"
			case "nil-node":
				original[0].NodeTransport = nil
			case "foreign-op":
				op = "kubernetes.exec"
			case "attempted":
				facts[0].state = "ATTEMPTING"
			case "open-gate":
				facts[0].openGate = true
			case "output":
				facts[0].hasOutput = true
			case "stream":
				facts[0].streamRef = "stream_synthetic"
			case "not-current":
				facts[0].current = false
			case "not-ready":
				original[0].State = "WAITING_OWNER"
			}
			if validKubernetesPortForwardPreflight(op, original, facts) != (variant == "valid") {
				t.Fatal("caller hint replaced owner preflight proof")
			}
		})
	}
}
