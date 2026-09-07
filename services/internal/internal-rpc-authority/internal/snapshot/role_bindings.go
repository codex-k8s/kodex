package snapshot

import "errors"

// Родительская binding нужна issuer только для проверки принятого контекста.
// Право подписи ограничено собственным issuer в доменном Issue/IssueContinuation.
func selectRoleBindings(role Role, workloadID string, bindings []operationBinding) ([]operationBinding, error) {
	if (role != RoleIssuer && role != RoleVerifier) || workloadID == "" {
		return nil, errors.New("authority binding selector identity is invalid")
	}
	byID := make(map[string]operationBinding, len(bindings))
	for _, binding := range bindings {
		if binding.OperationID == "" {
			return nil, errors.New("authority operation id is empty")
		}
		if _, exists := byID[binding.OperationID]; exists {
			return nil, errors.New("duplicate authority operation binding")
		}
		byID[binding.OperationID] = binding
	}
	selected := make(map[string]struct{})
	for _, binding := range bindings {
		if !bindingApplies(role, workloadID, binding) {
			continue
		}
		selected[binding.OperationID] = struct{}{}
		if role != RoleIssuer || binding.Continuation == nil {
			continue
		}
		parent, exists := byID[binding.Continuation.ParentOperationID]
		if !exists || parent.OperationID == binding.OperationID ||
			parent.FullMethod != binding.Continuation.ParentFullMethod ||
			parent.TargetWorkloadID != workloadID || parent.TargetSPIFFEID != binding.CallerSPIFFEID {
			return nil, errors.New("authority continuation parent binding is unavailable or mismatched")
		}
		selected[parent.OperationID] = struct{}{}
	}
	result := make([]operationBinding, 0, len(selected))
	for _, binding := range bindings {
		if _, exists := selected[binding.OperationID]; exists {
			result = append(result, binding)
		}
	}
	return result, nil
}
