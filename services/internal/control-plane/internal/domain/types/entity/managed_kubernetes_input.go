package entity

import (
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
)

// ManagedKubernetesClaimedInputFrame shares exact private metadata/hash/ownership validation with its gateway.
// ManagedKubernetesClaimedInputFrame разделяет exact private metadata/hash/ownership validation с gateway.
type ManagedKubernetesClaimedInputFrame = managedkubernetes.InputFrameDelivery

// ManagedKubernetesInputFrameAdmission carries payload only for the first durable DELIVER attempt.
// ManagedKubernetesInputFrameAdmission содержит payload лишь при первом durable DELIVER attempt.
type ManagedKubernetesInputFrameAdmission struct {
	Disposition enum.ManagedKubernetesInputFrameDisposition
	Frame       *ManagedKubernetesClaimedInputFrame
}
