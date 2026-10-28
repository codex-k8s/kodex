package workload

import (
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InitializationObservation binds closed init metadata to one current owned Pod UID.
// The UID is observational provenance, not historical Pod authority or a business receipt.
// InitializationObservation связывает закрытые init metadata с текущим owned Pod UID.
// UID является observation provenance, не historical authority либо business receipt.
type InitializationObservation struct {
	PodUID          string
	Stage, Reason   string
	RecordValidated bool
}

// observeInitializationFailure accepts only current nonzero init termination under exact metadata.
// Missing or malformed records stay UNKNOWN; caller never receives raw termination bytes.
// observeInitializationFailure принимает current nonzero init termination при exact metadata.
// Отсутствующий или повреждённый record остаётся UNKNOWN; raw bytes caller не получает.
func (manager *Manager) observeInitializationFailure(input runtimecontract.RunnerInput, pod *corev1.Pod) (*InitializationObservation, bool) {
	expected := metav1.ObjectMeta{Name: runtimecontract.RuntimeTurnPodName(input.LeaseRef), Namespace: manager.config.RuntimeNamespace,
		Labels:      map[string]string{managedLabel: "true", modeLabel: "turn", "app.kubernetes.io/name": "agent-runner", "app.kubernetes.io/component": "role-runtime", "kodex.dev/environment": manager.config.Environment, executionHashLabel: shortHash(input.LeaseRef)},
		Annotations: runtimeProjectionAnnotations(input, runtimecontract.RuntimeTurnPodName(input.LeaseRef))}
	expected.Annotations[leaseAnnotation] = input.LeaseRef
	if manager.config.RPCProfile == runtimecontract.CallbackProfileTrustedCluster {
		expected.Labels["kodex.dev/security-profile"] = runtimecontract.CallbackProfileTrustedCluster
	}
	if pod == nil || !validInitializationPodUID(string(pod.UID)) || pod.Name != expected.Name || pod.Namespace != expected.Namespace || !managedMetadataMatches(pod.ObjectMeta, expected) || pod.DeletionTimestamp != nil {
		return nil, false
	}
	validContainer := false
	containerCount := 0
	for _, container := range pod.Spec.InitContainers {
		if container.Name == "workspace-init" {
			containerCount++
			validContainer = (container.TerminationMessagePolicy == "" || container.TerminationMessagePolicy == corev1.TerminationMessageReadFile) &&
				(container.TerminationMessagePath == "" || container.TerminationMessagePath == "/dev/termination-log") &&
				len(container.Args) == 1 && container.Args[0] == "runtime-init-workspace" && container.Image == input.ImageReference
		}
	}
	if !validContainer || containerCount != 1 {
		return nil, false
	}
	statusCount := 0
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == "workspace-init" {
			statusCount++
		}
	}
	if statusCount != 1 {
		return nil, false
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name != "workspace-init" || status.State.Terminated == nil || status.State.Terminated.ExitCode == 0 {
			continue
		}
		observation := &InitializationObservation{PodUID: string(pod.UID), Stage: "UNKNOWN", Reason: "UNKNOWN"}
		raw := []byte(status.State.Terminated.Message)
		if record, err := runtimecontract.DecodeInitializationFailure(raw); err == nil && record.Matches(input) {
			observation.Stage, observation.Reason, observation.RecordValidated = record.Stage, record.Reason, true
		}
		return observation, true
	}
	return nil, false
}

// validInitializationPodUID rejects empty or unbounded/control-bearing API identity metadata.
// validInitializationPodUID отклоняет пустую, неограниченную или control-bearing API identity.
func validInitializationPodUID(uid string) bool {
	if len(uid) != 36 || uid == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for index, r := range uid {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// InitializationObservationKey fences terminal grace by exact UID and closed immutable record.
// InitializationObservationKey закрепляет terminal grace за UID и closed immutable record.
func InitializationObservationKey(value *InitializationObservation) string {
	if value == nil {
		return ""
	}
	if !value.Valid() {
		return "INVALID"
	}
	return value.PodUID + "/" + value.Stage + "/" + value.Reason + "/" + strconv.FormatBool(value.RecordValidated)
}

// Valid accepts one safe current UID and either a closed verified pair or explicit unknown.
// Valid принимает safe current UID и closed verified pair либо явную неизвестность.
func (value InitializationObservation) Valid() bool {
	return validInitializationPodUID(value.PodUID) &&
		runtimecontract.ValidInitializationFailureSummary("i18n:RUNTIME_INIT_"+value.Stage+"_"+value.Reason) &&
		(value.RecordValidated || value.Stage == "UNKNOWN" && value.Reason == "UNKNOWN")
}
