package workload

import (
	"errors"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	corev1 "k8s.io/api/core/v1"
)

// errWorkspacePreparedPVC refuses missing or changed physical custody without creating a replacement.
// errWorkspacePreparedPVC отклоняет missing либо changed physical custody без создания replacement.
var errWorkspacePreparedPVC = errors.New("runtime prepared workspace volume is unavailable")

// preparedWorkspacePVCMatches verifies the owner-pinned incarnation and qualified geometry after
// a read-only API lookup. Names, RWO and annotations alone never authorize adoption or repair.
// preparedWorkspacePVCMatches проверяет owner-pinned incarnation и qualified geometry после
// read-only API lookup. Name, RWO и annotations не разрешают adoption либо repair.
func preparedWorkspacePVCMatches(input runtimecontract.RunnerInput, actual, expected *corev1.PersistentVolumeClaim, storageClass string) bool {
	binding := input.WorkspaceStorage
	if binding == nil || binding.VolumeReady == nil || binding.ValidateForInput(input) != nil ||
		actual == nil || expected == nil || storageClass != "local-path" || actual.ResourceVersion == "" ||
		actual.Name != expected.Name || actual.Namespace != expected.Namespace || len(actual.OwnerReferences) != 0 ||
		string(actual.UID) != binding.VolumeReady.Preparation.PVCUID ||
		actual.Status.Phase != corev1.ClaimBound || actual.Spec.VolumeName == "" ||
		actual.Spec.VolumeMode == nil || *actual.Spec.VolumeMode != corev1.PersistentVolumeFilesystem ||
		actual.Spec.Resources.Requests.Storage().Value() != runtimecontract.WorkspaceVolumeRequestedBytes ||
		actual.Annotations["workspace.kodex.dev/workspace-ref"] != binding.WorkspaceRef ||
		actual.Annotations["workspace.kodex.dev/volume-generation"] != strconv.FormatInt(binding.VolumeReady.Preparation.VolumeGeneration, 10) {
		return false
	}
	return sessionPVCMatches(actual, expected, storageClass)
}
