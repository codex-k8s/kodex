package controlplaneclient

import cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// SSHIdentityHealthOwnerOperations registers only private health admission and completion; setup and business effects retain separate operation profiles.
// SSHIdentityHealthOwnerOperations регистрирует лишь private health admission и completion; setup и business effects сохраняют отдельные operation profiles.
func SSHIdentityHealthOwnerOperations() map[string]string {
	return map[string]string{
		"platform.runtime.ssh.health.begin":    cp.ManagedSSHOwnerService_BeginSSHIdentityHealthProbe_FullMethodName,
		"platform.runtime.ssh.health.complete": cp.ManagedSSHOwnerService_CompleteSSHIdentityHealthProbe_FullMethodName,
	}
}

// SSHIdentityBrokerOperations admits only the closed health, original-claim execution and metadata reconciliation RPCs; generic credential projection is excluded.
// SSHIdentityBrokerOperations допускает лишь closed health, original-claim execution и metadata reconciliation RPC без generic credential projection.
func SSHIdentityBrokerOperations() map[string]string {
	return map[string]string{"ssh.health.probe": "/secretbroker.v1.ManagedSSHService/ProbeSSHIdentity",
		"ssh.execute":   "/secretbroker.v1.ManagedSSHService/ExecuteSSHIdentity",
		"ssh.reconcile": "/secretbroker.v1.ManagedSSHService/ReconcileSSHIdentity"}
}
