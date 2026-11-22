package kubernetestarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	strictjson "sigs.k8s.io/json"
)

// ValidateQualifiedResourcePolicy combines ordinary object policy with fresh selected-identity
// evidence for operator-pinned CSI storage/custom controllers. The profile is private target
// metadata, never request authority. Missing read permissions close qualification explicitly;
// this method neither borrows operator credentials nor requires namespace GET for ordinary CRUD.
// ValidateQualifiedResourcePolicy объединяет ordinary object policy с fresh selected-identity
// evidence для operator-pinned CSI storage/custom controllers. Profile — private target metadata,
// не request authority. Недостаточные read permissions закрывают qualification; operator
// credentials не заимствуются, namespace GET для ordinary CRUD не требуется.
func (credential Credential) ValidateQualifiedResourcePolicy(ctx context.Context, operation string, address ResourceAddress, registeredKind, schemaDigest, subjectDigest string, object map[string]any, protectedNamespaces []string) error {
	registered, known := integrationpackage.KubernetesOperationByKey(operation)
	if ctx == nil || !known || validateCredentialResourcePolicy(credential.target) != nil {
		return errors.New(resourcePolicyForbidden)
	}
	if registered.Risk == integrationpackage.RiskRead {
		return ValidateResourceObjectPolicy(operation, address, registeredKind, object, protectedNamespaces)
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	podSpec, workload, err := resourcePodSpec(address, registeredKind, object)
	if err != nil {
		return err
	}
	custom := !scheme.Scheme.Recognizes(schema.FromAPIVersionAndKind(resourceGroupVersion(address), registeredKind))
	if custom {
		qualification, err := credential.resourceControllerQualification(bounded, address, registeredKind, schemaDigest, subjectDigest)
		if err != nil {
			return err
		}
		if qualification.Consumer == managedkubernetes.ControllerConsumerPodTemplateV1 {
			if !address.Namespaced || address.AllNamespaces || address.Namespace == "" {
				return errors.New(resourceControllerPolicyRequired)
			}
			podSpec, err = extractQualifiedPodSpec(object, qualification.Extractor, address.Namespace)
			if err != nil {
				return err
			}
			workload = true
		}
	}
	qualified := map[string]bool{}
	if workload {
		qualified, err = credential.qualifyResourceStorage(bounded, address.Namespace, subjectDigest, podSpec)
		if err != nil {
			return err
		}
	}
	var customSpec map[string]any
	if custom && workload {
		customSpec = podSpec
	}
	if bounded.Err() != nil {
		return errors.New(resourcePolicyForbidden)
	}
	return validateResourceObjectPolicy(operation, address, registeredKind, object, protectedNamespaces, qualified, customSpec, custom)
}

// readResourcePolicyObject performs an exact typed GET through selected-identity SSAR and TLS.
// No label-only scan, discovery URL, annotation or alternate credential can supply this evidence.
// readResourcePolicyObject выполняет exact typed GET через selected-identity SSAR/TLS.
// Label-only scan, discovery URL, annotation либо alternate credential не дают это evidence.
func (credential Credential) readResourcePolicyObject(ctx context.Context, subjectDigest string, address ResourceAddress, output any) error {
	requirements, err := OperationAccessRequirements("kubernetes.get", address)
	if err != nil || credential.Authorize(ctx, subjectDigest, requirements) != nil {
		return errors.New(resourcePolicyForbidden)
	}
	client, closeIdle, err := credential.NewClient(30 * time.Second)
	if err != nil {
		return errors.New(resourcePolicyForbidden)
	}
	defer closeIdle()
	request, err := credential.ReadRequest(ctx, address)
	if err != nil {
		return errors.New(resourcePolicyForbidden)
	}
	return decodeResourcePolicyHTTP(client, request, output)
}

// decodeResourcePolicyHTTP shares bounded duplicate-free typed decoding for exact policy evidence.
// decodeResourcePolicyHTTP объединяет bounded duplicate-free typed decode exact policy evidence.
func decodeResourcePolicyHTTP(client *http.Client, request *http.Request, output any) error {
	raw, status, err := readResourceHTTPDocument(client, request, resourceResponseBytes)
	if err != nil || status != http.StatusOK {
		return errors.New(resourcePolicyForbidden)
	}
	defer clear(raw)
	if _, err = decodeResourceJSON(raw, resourceResponseBytes, 64); err != nil {
		return errors.New(resourcePolicyForbidden)
	}
	strictErrors, err := strictjson.UnmarshalStrict(raw, output)
	if err != nil || len(strictErrors) != 0 {
		return errors.New(resourcePolicyForbidden)
	}
	return nil
}

// qualifyResourceStorage resolves only PVC volume sources into exact verified volume modes.
// Inline CSI/ephemeral providers remain explicitly unqualified rather than masquerading as PVCs.
// qualifyResourceStorage разрешает лишь PVC volume sources в exact verified volume modes.
// Inline CSI/ephemeral providers явно unqualified и не выдаются за PVC.
func (credential Credential) qualifyResourceStorage(ctx context.Context, namespace, subject string, podSpec map[string]any) (map[string]bool, error) {
	raw, err := json.Marshal(podSpec)
	if err != nil {
		return nil, errors.New(resourcePolicyForbidden)
	}
	defer clear(raw)
	var spec corev1.PodSpec
	strictErrors, err := strictjson.UnmarshalStrict(raw, &spec)
	if err != nil || len(strictErrors) != 0 {
		return nil, errors.New(resourcePolicyForbidden)
	}
	qualified := map[string]bool{}
	for _, volume := range spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			continue
		}
		var profile *managedkubernetes.StorageQualification
		if credential.target.ResourcePolicy != nil {
			for index := range credential.target.ResourcePolicy.Storage {
				candidate := &credential.target.ResourcePolicy.Storage[index]
				if candidate.PVCNamespace == namespace && candidate.PVCName == volume.PersistentVolumeClaim.ClaimName {
					profile = candidate
				}
			}
		}
		if profile == nil || profile.MountReadOnly != volume.PersistentVolumeClaim.ReadOnly || credential.verifyResourceStorage(ctx, subject, *profile) != nil {
			return nil, errors.New(resourceStoragePolicyRequired)
		}
		qualified[volume.Name] = profile.VolumeMode == string(corev1.PersistentVolumeBlock)
	}
	return qualified, nil
}

// verifyResourceStorage checks the entire fresh PVC/PV/SC/CSIDriver chain against one immutable
// application-data profile. All CSI Secret refs/token requests, mixed sources, hostPath and
// substituted claimRef/source/driver metadata fail even when a caller has ordinary Pod RBAC.
// verifyResourceStorage сверяет всю fresh PVC/PV/SC/CSIDriver chain с одним immutable app-data
// profile. Все CSI Secret refs/token requests, mixed sources, hostPath и подмена claimRef/source/
// driver metadata отклоняются даже при ordinary Pod RBAC caller.
func (credential Credential) verifyResourceStorage(ctx context.Context, subject string, expected managedkubernetes.StorageQualification) error {
	var claim corev1.PersistentVolumeClaim
	var volume corev1.PersistentVolume
	var class storagev1.StorageClass
	var driver storagev1.CSIDriver
	if expected.Validate() != nil || credential.readResourcePolicyObject(ctx, subject, ResourceAddress{Version: "v1", Resource: "persistentvolumeclaims", Namespaced: true, Namespace: expected.PVCNamespace, Name: expected.PVCName}, &claim) != nil ||
		credential.readResourcePolicyObject(ctx, subject, ResourceAddress{Version: "v1", Resource: "persistentvolumes", Name: expected.PVName}, &volume) != nil ||
		credential.readResourcePolicyObject(ctx, subject, ResourceAddress{Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Name: expected.StorageClassName}, &class) != nil ||
		credential.readResourcePolicyObject(ctx, subject, ResourceAddress{Group: "storage.k8s.io", Version: "v1", Resource: "csidrivers", Name: expected.CSIDriver}, &driver) != nil {
		return errors.New(resourceStoragePolicyRequired)
	}
	return validateResourceStorageEvidence(expected, claim, volume, class, driver)
}

// validateResourceStorageEvidence is the pure proof predicate shared by real GETs and fixtures.
// It does not infer a provisioner from annotations or treat a matching profile as live evidence.
// validateResourceStorageEvidence — pure proof predicate для real GETs и fixtures.
// Не выводит provisioner из annotations и не считает matching profile live evidence.
func validateResourceStorageEvidence(expected managedkubernetes.StorageQualification, claim corev1.PersistentVolumeClaim, volume corev1.PersistentVolume, class storagev1.StorageClass, driver storagev1.CSIDriver) error {
	if expected.Validate() != nil || claim.Kind != "PersistentVolumeClaim" || claim.APIVersion != "v1" || claim.Namespace != expected.PVCNamespace || claim.Name != expected.PVCName || string(claim.UID) != expected.PVCUID ||
		claim.DeletionTimestamp != nil || claim.Status.Phase != corev1.ClaimBound || claim.Spec.VolumeName != expected.PVName || claim.Spec.StorageClassName == nil || *claim.Spec.StorageClassName != expected.StorageClassName ||
		volume.Kind != "PersistentVolume" || volume.APIVersion != "v1" || volume.Name != expected.PVName || string(volume.UID) != expected.PVUID || volume.DeletionTimestamp != nil || volume.Status.Phase != corev1.VolumeBound ||
		volume.Spec.StorageClassName != expected.StorageClassName || volume.Spec.ClaimRef == nil || volume.Spec.ClaimRef.Name != expected.PVCName || volume.Spec.ClaimRef.Namespace != expected.PVCNamespace || string(volume.Spec.ClaimRef.UID) != expected.PVCUID ||
		class.Kind != "StorageClass" || class.APIVersion != "storage.k8s.io/v1" || class.Name != expected.StorageClassName || string(class.UID) != expected.StorageClassUID || class.DeletionTimestamp != nil || class.Provisioner != expected.Provisioner ||
		driver.Kind != "CSIDriver" || driver.APIVersion != "storage.k8s.io/v1" || driver.Name != expected.CSIDriver || string(driver.UID) != expected.CSIDriverUID || driver.DeletionTimestamp != nil ||
		len(driver.Spec.TokenRequests) != 0 || driver.Spec.RequiresRepublish != nil && *driver.Spec.RequiresRepublish {
		return errors.New(resourceStoragePolicyRequired)
	}
	claimMode, volumeMode := corev1.PersistentVolumeFilesystem, corev1.PersistentVolumeFilesystem
	if claim.Spec.VolumeMode != nil {
		claimMode = *claim.Spec.VolumeMode
	}
	if volume.Spec.VolumeMode != nil {
		volumeMode = *volume.Spec.VolumeMode
	}
	csi := volume.Spec.CSI
	if string(claimMode) != expected.VolumeMode || string(volumeMode) != expected.VolumeMode || csi == nil || csi.Driver != expected.CSIDriver || csi.VolumeHandle == "" || csi.ReadOnly != expected.MountReadOnly ||
		csi.ControllerPublishSecretRef != nil || csi.NodeStageSecretRef != nil || csi.NodePublishSecretRef != nil || csi.ControllerExpandSecretRef != nil || csi.NodeExpandSecretRef != nil {
		return errors.New(resourceStoragePolicyRequired)
	}
	remainder := volume.Spec.PersistentVolumeSource
	remainder.CSI = nil
	raw, err := json.Marshal(remainder)
	if err != nil || string(raw) != "{}" || resourceCanonicalDigest(volume.Spec.PersistentVolumeSource) != expected.VolumeSourceSHA256 ||
		resourceCanonicalDigest(volume.Spec.MountOptions) != expected.MountOptionsSHA256 || resourceCanonicalDigest(driver.Spec) != expected.CSIDriverSpecSHA256 {
		return errors.New(resourceStoragePolicyRequired)
	}
	return nil
}

// resourceCanonicalDigest hashes the same typed canonical JSON used by operator qualification.
// resourceCanonicalDigest хэширует тот же typed canonical JSON, что operator qualification.
func resourceCanonicalDigest(object any) string {
	raw, err := json.Marshal(object)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// extractQualifiedPodSpec uses only the profile's finite reviewed template locations.
// extractQualifiedPodSpec использует лишь finite reviewed template locations profile.
func extractQualifiedPodSpec(object map[string]any, extractor, namespace string) (map[string]any, error) {
	paths := map[string][]string{"SPEC_TEMPLATE": {"spec", "template", "spec"}, "SPEC_JOB_TEMPLATE": {"spec", "jobTemplate", "spec", "template", "spec"}, "SPEC_POD_TEMPLATE": {"spec", "podTemplate", "spec"}}
	path, known := paths[extractor]
	if !known {
		return nil, errors.New(resourceControllerPolicyRequired)
	}
	current := object
	for index, key := range path {
		if index == len(path)-1 {
			if raw, present := current["metadata"]; present {
				metadata, valid := raw.(map[string]any)
				if !valid {
					return nil, errors.New(resourceControllerPolicyRequired)
				}
				if value, present := metadata["namespace"]; present && value != "" && value != namespace {
					return nil, errors.New(resourceControllerPolicyRequired)
				}
			}
		}
		var ok bool
		current, ok = current[key].(map[string]any)
		if !ok {
			return nil, errors.New(resourceControllerPolicyRequired)
		}
	}
	return current, nil
}
