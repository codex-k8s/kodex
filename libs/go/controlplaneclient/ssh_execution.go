package controlplaneclient

import cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"

// SSHExecutionOwnerOperations registers only the private finite execution ledger; it does not grant generic invocation completion or key projection.
// SSHExecutionOwnerOperations регистрирует лишь private finite execution ledger без generic invocation completion либо key projection.
func SSHExecutionOwnerOperations() map[string]string {
	return map[string]string{
		"platform.runtime.ssh.execution.begin":    cp.ManagedSSHOwnerService_BeginSSHExecution_FullMethodName,
		"platform.runtime.ssh.execution.complete": cp.ManagedSSHOwnerService_CompleteSSHExecution_FullMethodName,
		"platform.runtime.ssh.execution.get":      cp.ManagedSSHOwnerService_GetSSHExecution_FullMethodName,
	}
}
