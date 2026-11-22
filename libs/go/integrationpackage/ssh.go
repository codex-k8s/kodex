package integrationpackage

import (
	"errors"
	"reflect"

	"github.com/codex-k8s/kodex/libs/go/managedssh"
)

const (
	// AdapterSSHTarget routes the separate broker-owned SSH purpose; it never upgrades SSH_COMPOSE or a generic TOKEN connection.
	// AdapterSSHTarget направляет отдельный broker-owned SSH purpose, не повышая SSH_COMPOSE либо generic TOKEN connection.
	AdapterSSHTarget AdapterKey = "SSH_TARGET"
	// SSHManagedDefinitionKey selects only operator-qualified managed recipes with a separate EACH-only execute definition.
	// SSHManagedDefinitionKey выбирает лишь operator-qualified managed recipes; EACH-only execute имеет отдельное definition.
	SSHManagedDefinitionKey = "ssh-managed"
	// SSHExecuteDefinitionKey selects arbitrary execution whose risk is always SENSITIVE and whose gate is always EACH.
	// SSHExecuteDefinitionKey выбирает arbitrary execution с неизменными SENSITIVE risk и EACH gate.
	SSHExecuteDefinitionKey = "ssh-execute"
	// CredentialResourcePermissionSSHScopedV1 requires current target, identity and recipe checks at every operation; health never proves reusable host-wide READ or WRITE permission.
	// CredentialResourcePermissionSSHScopedV1 требует current target, identity и recipe checks при каждой operation; health не доказывает reusable host-wide READ либо WRITE permission.
	CredentialResourcePermissionSSHScopedV1 = "SSH_SCOPED_RESOURCE_V1"
)

// errSSHDefinition rejects malformed package/configuration boundaries without exposing any target or credential metadata.
// errSSHDefinition отклоняет malformed package/configuration boundary без target либо credential metadata.
var errSSHDefinition = errors.New("SSH target definition is invalid")

// sshBrokerDestination pins only the existing protected internal RPC, never a caller-selected host, port, SSH URL or proxy destination.
// sshBrokerDestination закрепляет лишь existing protected internal RPC, не caller-selected host, port, SSH URL либо proxy destination.
func sshBrokerDestination() NetworkDestination {
	return NetworkDestination{Key: "ssh_broker", Source: "STATIC", Hostname: "secret-broker.kodex-system.svc", Port: 8443, TLS: "REQUIRED"}
}

// sshBrokerNetworkDestination recognizes an exact fixed package/adapter/transport tuple; it cannot exempt another adapter or arbitrary internal hostname from external TLS policy.
// sshBrokerNetworkDestination распознаёт exact fixed package/adapter/transport tuple, не освобождая другой adapter либо arbitrary internal hostname от external TLS policy.
func sshBrokerNetworkDestination(definition Package, destination NetworkDestination) bool {
	return definition.Spec.Adapter == string(AdapterSSHTarget) && (definition.Metadata.Key == SSHManagedDefinitionKey || definition.Metadata.Key == SSHExecuteDefinitionKey) && destination == sshBrokerDestination()
}

// sshTypedOperation recognizes only the closed managed recipes and the separately named execute/identity paths; a shared prefix cannot authorize an arbitrary command.
// sshTypedOperation распознаёт лишь closed managed recipes и отдельные execute/identity paths; общий prefix не разрешает arbitrary command.
func sshTypedOperation(operation string) bool {
	_, err := managedssh.ParseOperation(operation)
	return err == nil || operation == "ssh.identity.inspect" || operation == "ssh.execute"
}

// validateSSHConfiguration accepts selectors only; the owner resolves the current target and an explicitly selected active SERVICE for shared custody.
// validateSSHConfiguration принимает лишь selectors; owner разрешает current target и explicitly selected active SERVICE для shared custody.
func validateSSHConfiguration(configuration map[string]string) error {
	if len(configuration) < 1 || len(configuration) > 2 || !managedssh.OwnerReference(configuration["target_ref"]) {
		return errSSHDefinition
	}
	for key, value := range configuration {
		if key != "target_ref" && (key != "service_subject_ref" || !managedssh.OwnerReference(value)) {
			return errSSHDefinition
		}
	}
	return nil
}

// validateSSHDefinition fixes broker custody, exact configuration and policy floors; execution still requires the exact owner, purpose consumer and installed schema contract.
// validateSSHDefinition закрепляет broker custody, exact configuration и policy floors; execution требует exact owner, purpose consumer и installed schema contract.
func validateSSHDefinition(definition Package) error {
	if definition.Spec.Adapter != string(AdapterSSHTarget) || (definition.Metadata.Key != SSHManagedDefinitionKey && definition.Metadata.Key != SSHExecuteDefinitionKey) ||
		!validConnectionCredential(definition.Spec.Adapter, definition.Spec.Credential) || len(definition.Spec.ConfigurationFields) != 2 || !reflect.DeepEqual(definition.Spec.NetworkDestinations, []NetworkDestination{sshBrokerDestination()}) {
		return errSSHDefinition
	}
	target, subject := definition.Spec.ConfigurationFields[0], definition.Spec.ConfigurationFields[1]
	if target.Key != "target_ref" || target.Type != "STRING" || target.Format != "IDENTIFIER" || !target.Required || target.MaximumLength != 96 || target.AllowEmpty || len(target.AllowedValues) != 0 ||
		subject.Key != "service_subject_ref" || subject.Type != "STRING" || subject.Format != "IDENTIFIER" || subject.Required || subject.MaximumLength != 96 || subject.AllowEmpty || len(subject.AllowedValues) != 0 {
		return errSSHDefinition
	}
	health := "ssh.target.inspect"
	expected := 14
	if definition.Metadata.Key == SSHExecuteDefinitionKey {
		health = "ssh.identity.inspect"
		expected = 2
	}
	if definition.Spec.HealthCheck.Operation != health || definition.Spec.HealthCheck.MaxAttempts != 1 || definition.Metadata.Origin == Origin && len(definition.Spec.Capabilities) != expected {
		return errSSHDefinition
	}
	for _, capability := range definition.Spec.Capabilities {
		if capability.Key != capability.Operation || !sshTypedOperation(capability.Operation) || capability.OpenAPI != nil || capability.ResourceScope.Kind != "SSH_TARGET" || !reflect.DeepEqual(capability.ResourceScope.ConnectionFields, []string{"target_ref"}) || capability.Execution.MaxAttempts != 1 || capability.Execution.LeaseProfile != "" || capability.Execution.TimeoutSeconds > 20 {
			return errSSHDefinition
		}
		risk, approval, idempotency, known := sshOperationRequirements(definition.Metadata.Key, capability.Operation)
		if !known {
			return errSSHDefinition
		}
		if capability.Operation == "ssh.execute" && !validSSHExecuteFields(capability.InputFields) {
			return errSSHDefinition
		}
		if capability.Risk != risk || capability.ApprovalPolicy != approval || capability.Execution.Idempotency != idempotency {
			return errSSHDefinition
		}
	}
	return nil
}

// sshOperationRequirements derives policy floors from the single managed recipe registry and the separate EACH-only execute path.
// It does not classify arbitrary shell text or issue execution authority.
// sshOperationRequirements выводит policy floors из единого managed recipe registry и отдельного EACH-only execute path.
// Не классифицирует arbitrary shell text и не выдаёт execution authority.
func sshOperationRequirements(definitionKey, operation string) (risk, approval, idempotency string, known bool) {
	if definitionKey == SSHExecuteDefinitionKey {
		switch operation {
		case "ssh.identity.inspect":
			return "READ", "NONE", "READ_ONLY", true
		case "ssh.execute":
			return "SENSITIVE", "HUMAN_EACH_EFFECT", "ONE_ATTEMPT", true
		default:
			return "", "", "", false
		}
	}
	if definitionKey != SSHManagedDefinitionKey {
		return "", "", "", false
	}
	managed, err := managedssh.ParseOperation(operation)
	if err != nil {
		return "", "", "", false
	}
	effect, err := managed.Effect()
	if err != nil {
		return "", "", "", false
	}
	switch effect {
	case managedssh.ReadOnly:
		return "READ", "NONE", "READ_ONLY", true
	case managedssh.ScopedChange:
		return "WRITE", "HUMAN_SCOPED", "EFFECT_KEY", true
	case managedssh.EachChange:
		if managed == managedssh.FileDelete {
			return "DESTRUCTIVE", "HUMAN_EACH_EFFECT", "EFFECT_KEY", true
		}
		return "SENSITIVE", "HUMAN_EACH_EFFECT", "EFFECT_KEY", true
	default:
		return "", "", "", false
	}
}

// sshPermissionContract makes identity health a prerequisite for fresh per-operation checks, never a proof of arbitrary host access.
// sshPermissionContract делает identity health prerequisite свежих per-operation checks, не доказательством arbitrary host access.
func sshPermissionContract(definitionKey, capabilityKey string) (CredentialResourcePermissionContract, bool) {
	risk, approval, _, known := sshOperationRequirements(definitionKey, capabilityKey)
	if !known {
		return CredentialResourcePermissionContract{}, false
	}
	health := "ssh.target.inspect"
	if definitionKey == SSHExecuteDefinitionKey {
		health = "ssh.identity.inspect"
	}
	return CredentialResourcePermissionContract{Profile: CredentialResourcePermissionSSHScopedV1, Operation: capabilityKey, ResourceKind: "SSH_TARGET", Risk: risk, ApprovalPolicy: approval, ReadCapabilities: []string{health}}, true
}

// sshCredentialResourceProfile recognizes only an actual capability of the exact validated SSH package; it supplies no reusable resource permission.
// sshCredentialResourceProfile распознаёт лишь actual capability exact validated SSH package без reusable resource permission.
func sshCredentialResourceProfile(definition Package, capability Capability) string {
	contract, known := sshPermissionContract(definition.Metadata.Key, capability.Key)
	declared, exists := definition.Capability(capability.Key)
	if !known || !exists || !reflect.DeepEqual(declared, capability) || validateSSHDefinition(definition) != nil || capability.ResourceScope.Kind != contract.ResourceKind {
		return ""
	}
	return contract.Profile
}

// validSSHExecuteFields keeps the public package's command/stdin/output bounds equal to the actual typed transport; it cannot advertise an unsupported size or silently coerce another scalar type.
// validSSHExecuteFields сохраняет public command/stdin/output bounds равными actual typed transport, не объявляя unsupported size и не преобразуя другой scalar type.
func validSSHExecuteFields(fields []Field) bool {
	if len(fields) != 3 {
		return false
	}
	command, stdin, output := fields[0], fields[1], fields[2]
	return command.Key == "command" && command.Type == "STRING" && command.Format == "PLAIN" && command.Required && command.MaximumLength == managedssh.MaximumCommand && !command.AllowEmpty && len(command.AllowedValues) == 0 &&
		stdin.Key == "stdin_base64" && stdin.Type == "STRING" && stdin.Format == "PLAIN" && !stdin.Required && stdin.MaximumLength == 21848 && stdin.AllowEmpty && len(stdin.AllowedValues) == 0 &&
		output.Key == "maximum_output" && output.Type == "INTEGER" && output.Required && output.Minimum == 1 && output.Maximum == managedssh.MaximumReply
}
