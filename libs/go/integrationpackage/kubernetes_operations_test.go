package integrationpackage

import "testing"

// TestKubernetesOperationsKeepRiskIndependentOfInput verifies the non-negotiable command gates,
// including mutations hidden behind familiar kubectl families and separately gated streams.
// TestKubernetesOperationsKeepRiskIndependentOfInput проверяет обязательные command gates,
// включая mutations в знакомых kubectl families и отдельно подтверждаемые streams.
func TestKubernetesOperationsKeepRiskIndependentOfInput(t *testing.T) {
	for _, test := range []struct {
		key       string
		risk      Risk
		approval  ApprovalPolicy
		execution KubernetesExecution
	}{
		{"kubernetes.get", RiskRead, ApprovalNone, KubernetesResource},
		{"kubernetes.list.all-namespaces", RiskRead, ApprovalNone, KubernetesResource},
		{"kubernetes.watch.all-namespaces", RiskRead, ApprovalNone, KubernetesStream},
		{"kubernetes.logs", RiskRead, ApprovalNone, KubernetesStream},
		{"kubernetes.rollout.history", RiskRead, ApprovalNone, KubernetesPlan},
		{"kubernetes.rollout.undo", RiskWrite, ApprovalHumanEachEffect, KubernetesPlan},
		{"kubernetes.apply", RiskWrite, ApprovalHumanEachEffect, KubernetesResource},
		{"kubernetes.delete", RiskDestructive, ApprovalHumanEachEffect, KubernetesResource},
		{"kubernetes.drain", RiskDestructive, ApprovalHumanEachEffect, KubernetesPlan},
		{"kubernetes.auth.reconcile", RiskWrite, ApprovalHumanEachEffect, KubernetesPlan},
		{"kubernetes.certificate.approve", RiskWrite, ApprovalHumanEachEffect, KubernetesPlan},
		{"kubernetes.exec", RiskSensitive, ApprovalHumanEachEffect, KubernetesStream},
		{"kubernetes.cp.download", RiskSensitive, ApprovalHumanEachEffect, KubernetesStream},
		{"kubernetes.port-forward", RiskSensitive, ApprovalHumanEachEffect, KubernetesStream},
		{"kubernetes.config.metadata", RiskRead, ApprovalNone, KubernetesOffline},
	} {
		operation, known := KubernetesOperationByKey(test.key)
		if !known || operation.Risk != test.risk || operation.Approval != test.approval || operation.Execution != test.execution {
			t.Fatalf("command policy changed: %s", test.key)
		}
	}
	for _, key := range []string{"kubectl", "kubernetes.proxy", "kubernetes.raw", "kubernetes.config.view", "kubernetes.create.token", "kubernetes.plugin", "kubernetes.get --raw=/api"} {
		if _, known := KubernetesOperationByKey(key); known {
			t.Fatalf("unsafe client feature became an executable command: %s", key)
		}
	}
	previous := ""
	for _, operation := range KubernetesOperations() {
		if operation.Key <= previous || operation.Family == "" ||
			(operation.Execution == KubernetesResource) != (operation.Verb != "") {
			t.Fatal("command discovery is ambiguous or differs from executable classification")
		}
		if operation.Risk == RiskRead {
			if operation.Approval != ApprovalNone || operation.Idempotency != IdempotencyReadOnly {
				t.Fatal("read classification inherited a mutation policy")
			}
		} else if operation.Approval == ApprovalNone || operation.Idempotency != IdempotencyOneAttempt {
			t.Fatal("external mutation became ungated or automatically replayable")
		}
		previous = operation.Key
	}
}
