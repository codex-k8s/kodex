package managedkubernetes

import "github.com/codex-k8s/kodex/libs/go/internalrpcauth"

// MaximumForwardPorts bounds ordered tunnel slots and their two native channels.
// MaximumForwardPorts ограничивает ordered tunnel slots и два native channels каждого.
const MaximumForwardPorts = 16

// ForwardPort preserves an approved mapping; Port is a logical local endpoint, never a listener permission.
// ForwardPort сохраняет approved mapping; Port обозначает logical local endpoint, не право открыть listener.
type ForwardPort struct {
	Port       uint16 `json:"port"`
	TargetPort uint16 `json:"targetPort"`
	Protocol   string `json:"protocol,omitempty"`
	Name       string `json:"name,omitempty"`
}

// PortForwardProfile retains mapping order because frame slots refer to this immutable array.
// PortForwardProfile сохраняет порядок mapping: frame slots ссылаются на этот immutable array.
type PortForwardProfile struct {
	Ports          []ForwardPort `json:"ports"`
	TimeoutSeconds uint32        `json:"timeoutSeconds"`
}

// DecodePortForwardProfile accepts only the pinned Pod TCP profile, without silently sorting or coalescing ports.
// DecodePortForwardProfile принимает лишь pinned Pod TCP profile без скрытой сортировки либо объединения ports.
func DecodePortForwardProfile(input OperationInput) (PortForwardProfile, error) {
	var profile PortForwardProfile
	if input.Group != "" || input.Version != "v1" || input.Resource != "pods" || input.Subresource != "" ||
		internalrpcauth.DecodeStrictJSON(input.Options, &profile) != nil || profile.Validate() != nil {
		return PortForwardProfile{}, ErrInvalidOperationInput
	}
	return profile, nil
}

// Validate rejects ambiguous local mappings and non-TCP protocols; repeated remote ports remain separate slots.
// Validate отклоняет неоднозначные local mappings и non-TCP protocols; одинаковые remote ports сохраняют отдельные slots.
func (profile PortForwardProfile) Validate() error {
	if len(profile.Ports) == 0 || len(profile.Ports) > MaximumForwardPorts || profile.TimeoutSeconds == 0 || profile.TimeoutSeconds > 300 {
		return ErrInvalidOperationInput
	}
	for index, port := range profile.Ports {
		if port.Port == 0 || port.TargetPort == 0 || port.Protocol != "" && port.Protocol != "TCP" || len(port.Name) > 63 {
			return ErrInvalidOperationInput
		}
		for _, previous := range profile.Ports[:index] {
			if previous.Port == port.Port {
				return ErrInvalidOperationInput
			}
		}
	}
	return nil
}

// ValidatePortForwardFrame checks the closed slot-byte envelope and includes its header in the frame budget.
// The empty end marker closes the whole tunnel. It never grants a remote port or a new connection.
// ValidatePortForwardFrame проверяет closed slot-byte envelope с учётом header в frame budget.
// Пустой end marker закрывает весь tunnel. Он не разрешает remote port либо новое connection.
func ValidatePortForwardFrame(payload []byte, endOfInput bool, slots int) error {
	if slots < 1 || slots > MaximumForwardPorts || len(payload) > MaximumFrameBytes {
		return ErrInvalidOperationInput
	}
	if endOfInput && len(payload) == 0 || !endOfInput && len(payload) >= 2 && int(payload[0]) < slots {
		return nil
	}
	return ErrInvalidOperationInput
}
