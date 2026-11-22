package app

import (
	"context"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
)

// sshFactoryBroker forbids accidental RPCs during construction by leaving the generated transport unconfigured; it supplies neither owner claims nor host authority.
// sshFactoryBroker запрещает случайные RPC при construction отсутствующим generated transport; owner claims и host authority не выдаются.
type sshFactoryBroker struct{ sb.ManagedSSHServiceClient }

// TestSSHConsumerFactoryKeepsHealthAndCapacitySeparate proves zero-capacity serving has no activity slots, while enabled source availability comes from the actual registry and never a test readiness override.
// TestSSHConsumerFactoryKeepsHealthAndCapacitySeparate доказывает отсутствие activity slots при zero capacity; enabled source availability берётся из actual registry без test readiness override.
func TestSSHConsumerFactoryKeepsHealthAndCapacitySeparate(t *testing.T) {
	adapter, err := integration.New(integration.Config{CredentialDirectory: t.TempDir(), Timeout: time.Second, ProxyURL: "http://egress-gateway.kodex-system.svc.cluster.local:8080", OpenAPIProxyURL: "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083", SyntheticBaseURL: "http://integration-synthetic.kodex-system.svc.cluster.local:8080"})
	if err != nil {
		t.Fatal(err)
	}
	broker := &sshFactoryBroker{}
	for _, capacity := range []uint32{0, 2} {
		consumer, err := newSSHConfiguredConsumer(t.Context(), Config{InstanceID: "ssh-factory-source", SSHInvocationCapacity: capacity}, adapter, broker)
		registered, _ := integrationpackage.Adapter(string(integrationpackage.AdapterSSHTarget))
		if capacity != 0 && registered.Readiness != integrationpackage.ReadinessReady {
			if err == nil || consumer != nil {
				t.Fatal("incomplete source profile acquired invocation capacity")
			}
			continue
		}
		if err != nil || consumer == nil {
			t.Fatal("supported health/source profile was unavailable", err)
		}
		var slots []brokerIdentityClaimSlot
		for count := uint32(0); count < capacity; count++ {
			slot, ok := consumer.ReserveClaim()
			if !ok || slot == nil {
				t.Fatal("factory lost a real configured slot")
			}
			slots = append(slots, slot)
		}
		if slot, ok := consumer.ReserveClaim(); ok || slot != nil {
			t.Fatal("health-only or full consumer exceeded actual capacity")
		}
		for _, slot := range slots {
			slot.ReleaseUnused()
		}
		if consumer.Close() != nil || consumer.Wait(context.Background()) != nil || !consumer.Joined() {
			t.Fatal("unused constructor activity did not positively join")
		}
	}
}

// TestSSHBrokerProfileKeepsDisabledExecutionClosed compares actual generated method names and proves the factory adds no generic credential or KUBE stream authority.
// TestSSHBrokerProfileKeepsDisabledExecutionClosed сравнивает actual generated method names и доказывает отсутствие generic credential либо KUBE stream authority в factory.
func TestSSHBrokerProfileKeepsDisabledExecutionClosed(t *testing.T) {
	for _, capacity := range []uint32{0, 1} {
		operations := integrationBrokerOperations(capacity)
		if operations["ssh.health.probe"] != sb.ManagedSSHService_ProbeSSHIdentity_FullMethodName || operations["kubernetes.execute"] != sb.ManagedKubernetesService_ExecuteManagedKubernetesOperation_FullMethodName {
			t.Fatal("purpose health registry lost the real generated method")
		}
		for operation, method := range map[string]string{"ssh.execute": sb.ManagedSSHService_ExecuteSSHIdentity_FullMethodName, "ssh.reconcile": sb.ManagedSSHService_ReconcileSSHIdentity_FullMethodName} {
			value, found := operations[operation]
			if found != (capacity != 0) || found && value != method {
				t.Fatal("disabled lane gained execution or a substituted method")
			}
		}
		for _, operation := range []string{"secret.get", "kubernetes.stream.exchange", "kubernetes.cancel"} {
			if _, found := operations[operation]; found {
				t.Fatal("SSH factory broadened another purpose")
			}
		}
	}
}

// TestSSHInvocationCapacityConfiguration rejects invalid process values before Dial while retaining explicit zero by default; it grants no claim authority.
// TestSSHInvocationCapacityConfiguration отвергает неверные process values до Dial и сохраняет explicit zero по умолчанию; claim authority не выдаётся.
func TestSSHInvocationCapacityConfiguration(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	for _, capacity := range []string{"0", "4", "5", "-1", "not-a-number"} {
		t.Setenv("INTEGRATION_GATEWAY_SSH_INVOCATION_CAPACITY", capacity)
		config, err := loadConfig()
		valid := capacity == "0" || capacity == "4"
		if (err == nil) != valid || valid && config.SSHInvocationCapacity > 4 {
			t.Fatal("SSH capacity crossed the process bound")
		}
	}
}
