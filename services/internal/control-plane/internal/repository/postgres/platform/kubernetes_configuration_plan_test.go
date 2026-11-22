package platform

import (
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// TestKubernetesConfigurationPlanCapabilityRefusal prevents SYSTEM/PROJECT scalar hydration
// from reading one arbitrary namespace row while retaining a safe base scope for direct settings.
// TestKubernetesConfigurationPlanCapabilityRefusal запрещает SYSTEM/PROJECT scalar hydration
// читать случайную namespace row, сохраняя safe base scope для direct settings.
func TestKubernetesConfigurationPlanCapabilityRefusal(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal("load actual Kubernetes definition")
	}
	definition := definitions[integrationpackage.KubernetesDefinitionKey]
	for _, key := range []string{"kubernetes.get", "kubernetes.apply"} {
		capability, exists := definition.Capability(key)
		if !exists {
			t.Fatal("missing actual Kubernetes capability")
		}
		_, err := assistantGrantHydrationCapability(definition, command.SystemAssistantIntegrationGrantInput{CapabilityKey: key, Enabled: true, ApprovalPolicy: capability.ApprovalPolicy})
		if !errors.Is(err, errs.ErrKubernetesConfigurationPlanUnsupported) {
			t.Fatal("scalar assistant configuration admitted a Kubernetes grant")
		}
		scope, digest, err := projectAssistantPublicResourceScope(definition, capability, map[string]string{"target_ref": "k3d_target_selected"})
		if err != nil || len(scope) != 1 || scope["target_ref"] != "k3d_target_selected" || len(digest) != 64 {
			t.Fatal("direct manager candidate lost safe target scope", err)
		}
	}
}
