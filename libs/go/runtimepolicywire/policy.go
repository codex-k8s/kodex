// Package runtimepolicywire maps closed control-plane policy enums to canonical runtime names without resolving authority or validating complete policy objects.
// Package runtimepolicywire преобразует закрытые enum политик control-plane в канонические имена runtime без разрешения полномочий или проверки полных объектов политики.
package runtimepolicywire

import (
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// VolumeKindFromProto maps disk and memory ephemeral-volume enums to their canonical names;
// every other numeric value stays empty for rejection by the caller's policy validator.
// It performs no defaulting, allocation, I/O or workload configuration.
// VolumeKindFromProto преобразует enum временных дискового и оперативного томов в канонические
// имена; остальные числовые значения остаются пустыми для отказа валидатором вызывающего.
// Функция не выбирает значение по умолчанию, не выделяет ресурсы и не настраивает workload.
func VolumeKindFromProto(value controlplanev1.RuntimeVolumeKind) string {
	switch value {
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_DISK:
		return runtimecontract.RuntimeVolumeEphemeralDisk
	case controlplanev1.RuntimeVolumeKind_RUNTIME_VOLUME_KIND_EPHEMERAL_MEMORY:
		return runtimecontract.RuntimeVolumeEphemeralMemory
	default:
		return ""
	}
}

// NetworkDestinationFromProto recognizes only DNS, runtime callback and provider proxy enums.
// Unknown values become empty rather than arbitrary hostnames; destination eligibility, ports,
// protocols and complete policy validation remain with the caller.
// NetworkDestinationFromProto распознаёт только enum DNS, runtime callback и provider proxy.
// Неизвестные значения становятся пустыми, а не произвольными hostnames; допустимость
// назначения, порты, протоколы и проверка полной политики остаются у вызывающего.
func NetworkDestinationFromProto(value controlplanev1.RuntimeNetworkDestination) string {
	switch value {
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_DNS:
		return runtimecontract.RuntimeEgressDNS
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_RUNTIME_CALLBACK:
		return runtimecontract.RuntimeEgressRuntimeCallback
	case controlplanev1.RuntimeNetworkDestination_RUNTIME_NETWORK_DESTINATION_PROVIDER_PROXY:
		return runtimecontract.RuntimeEgressProviderProxy
	default:
		return ""
	}
}

// KubernetesAccessKindFromProto recognizes only the NONE profile and leaves unknown values
// empty for caller rejection. It neither grants access nor selects a service account, namespace
// or rule set; effective-access and digest validation remain separate caller obligations.
// KubernetesAccessKindFromProto распознаёт только профиль NONE, оставляя неизвестные значения
// пустыми для отказа вызывающим. Функция не выдаёт доступ и не выбирает service account,
// namespace или правила; проверка фактического доступа и digest остаётся обязанностью caller.
func KubernetesAccessKindFromProto(value controlplanev1.RuntimeKubernetesAccessKind) string {
	switch value {
	case controlplanev1.RuntimeKubernetesAccessKind_RUNTIME_KUBERNETES_ACCESS_KIND_NONE:
		return runtimecontract.RuntimeKubernetesAccessNone
	default:
		return ""
	}
}
