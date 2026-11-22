package kubernetestarget

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	strictjson "sigs.k8s.io/json"
)

// resourcePolicyForbidden is intentionally independent of object names, bodies and provider errors.
// resourcePolicyForbidden намеренно не содержит object names, bodies либо provider errors.
const resourcePolicyForbidden = "managed Kubernetes resource policy denied"

// resourceControllerPolicyRequired distinguishes an unqualified controller from a safe built-in.
// resourceControllerPolicyRequired отделяет unqualified controller от safe built-in.
const resourceControllerPolicyRequired = "managed Kubernetes controller policy is required"

// resourceStoragePolicyRequired keeps missing qualified storage distinct from a credential deny.
// resourceStoragePolicyRequired отделяет missing qualified storage от credential deny.
const resourceStoragePolicyRequired = "managed Kubernetes target storage policy is required"

// ResourcePolicyReason maps only owned closed policy errors to safe user-visible reason codes.
// Provider text is never reflected. Missing operator qualification remains visibly incomplete.
// ResourcePolicyReason отображает лишь owned closed policy errors в safe user-visible codes.
// Provider text не отражается. Missing operator qualification остаётся явно incomplete.
func ResourcePolicyReason(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case resourceStoragePolicyRequired:
		return managedkubernetes.ReasonTargetStoragePolicyRequired
	case resourceControllerPolicyRequired:
		return managedkubernetes.ReasonControllerPolicyRequired
	case resourceControllerRuntimeUnavailable:
		return managedkubernetes.ReasonControllerRuntimeUnavailable
	default:
		return managedkubernetes.ReasonProtectedResource
	}
}

// ValidateResourceObjectPolicy checks protected namespaces and registered built-in workload
// escalation boundaries on a strictly decoded, schema-validated current/reconstructed object.
// It issues no owner permit. Non-READ effects must validate both current and proposed objects;
// scale must also validate its exact UID/RV parent, not only the Scale acknowledgement.
// Custom controller semantics are not inferred from a JSON field name or a matching kind alone.
// ValidateResourceObjectPolicy проверяет protected namespaces и escalation boundaries
// зарегистрированных built-in workloads по строго разобранному, schema-validated current/
// reconstructed object. Owner permit не выдаётся. Non-READ effects проверяют current и proposed
// objects; scale дополнительно проверяет exact UID/RV parent, не только Scale acknowledgement.
// Семантика custom controller не выводится из JSON field name либо совпавшего kind.
func ValidateResourceObjectPolicy(operation string, address ResourceAddress, registeredKind string, object map[string]any, protectedNamespaces []string) error {
	return validateResourceObjectPolicy(operation, address, registeredKind, object, protectedNamespaces, nil, nil, false)
}

// validateResourceObjectPolicy accepts only broker-verified storage evidence and a fixed custom
// controller extractor. Those private parameters never originate in OperationInput or metadata.
// validateResourceObjectPolicy принимает лишь broker-verified storage evidence и fixed custom
// controller extractor. Эти private parameters никогда не приходят из OperationInput/metadata.
func validateResourceObjectPolicy(operation string, address ResourceAddress, registeredKind string, object map[string]any, protectedNamespaces []string, volumes map[string]bool, customSpec map[string]any, qualifiedController bool) error {
	registered, ok := integrationpackage.KubernetesOperationByKey(operation)
	if !ok || address.Validate() != nil || registeredKind == "" || object == nil ||
		object["kind"] != registeredKind || resourceNamespaceProtected(address, protectedNamespaces) || resourceReadReferencesProtectedNamespace(object, protectedNamespaces) {
		return errors.New(resourcePolicyForbidden)
	}
	version := address.Version
	if address.Group != "" {
		version = address.Group + "/" + version
	}
	metadata, ok := object["metadata"].(map[string]any)
	if !ok || object["apiVersion"] != version ||
		(address.Name != "" && metadata["name"] != address.Name) ||
		(address.Namespaced && metadata["namespace"] != address.Namespace) ||
		(!address.Namespaced && metadata["namespace"] != nil && metadata["namespace"] != "") {
		return errors.New(resourcePolicyForbidden)
	}
	if registered.Risk == integrationpackage.RiskRead {
		return nil
	}
	if address.Group == "apps" && registeredKind == "StatefulSet" {
		stateful, _ := object["spec"].(map[string]any)
		if templates, present := stateful["volumeClaimTemplates"]; present && templates != nil {
			values, ok := templates.([]any)
			if !ok || len(values) != 0 {
				return errors.New(resourceStoragePolicyRequired)
			}
		}
	}
	if !scheme.Scheme.Recognizes(schema.FromAPIVersionAndKind(version, registeredKind)) && !qualifiedController {
		return errors.New(resourceControllerPolicyRequired)
	}
	if address.Group == "" && address.Resource == "secrets" && object["type"] == string(corev1.SecretTypeServiceAccountToken) {
		return errors.New(resourcePolicyForbidden)
	}
	spec, workload, err := resourcePodSpec(address, registeredKind, object)
	if err != nil {
		return err
	}
	if customSpec != nil {
		spec, workload = customSpec, true
	}
	if workload {
		if err := validateResourcePodSpecWithStorage(spec, volumes); err != nil {
			return err
		}
	}
	return nil
}

// resourceNamespaceProtected uses only the caller's owner-resolved protected-name inventory.
// All-namespace collections must filter each item before projection, never treat an empty
// namespace on the collection as permission to return private items.
// resourceNamespaceProtected использует только owner-resolved inventory protected names.
// All-namespace collection фильтрует каждый item до projection; пустой namespace collection
// не разрешает выдачу private items.
func resourceNamespaceProtected(address ResourceAddress, protectedNamespaces []string) bool {
	return slices.Contains(protectedNamespaces, address.Namespace) ||
		(address.Group == "" && address.Resource == "namespaces" && slices.Contains(protectedNamespaces, address.Name))
}

// resourcePodSpec resolves only Kubernetes' registered built-in Pod/workload template paths.
// Missing or malformed paths fail closed; arbitrary recursive JSON searching is not a policy.
// resourcePodSpec разрешает только registered built-in Pod/workload template paths Kubernetes.
// Missing либо malformed paths закрыто отклоняются; arbitrary recursive JSON search не является policy.
func resourcePodSpec(address ResourceAddress, kind string, object map[string]any) (map[string]any, bool, error) {
	path := resourcePodSpecPath(address, kind)
	if path == nil {
		return nil, false, nil
	}
	current := object
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, true, errors.New(resourcePolicyForbidden)
		}
		current = next
	}
	return current, true, nil
}

// resourcePodSpecPath shares the closed built-in workload paths between policy validation and command patch construction.
// resourcePodSpecPath задаёт единый закрытый набор путей встроенных workload для проверки политики и построения patch команды.
func resourcePodSpecPath(address ResourceAddress, kind string) []string {
	switch {
	case address.Group == "" && address.Resource == "pods" && kind == "Pod":
		return []string{"spec"}
	case address.Group == "" && address.Resource == "podtemplates" && kind == "PodTemplate":
		return []string{"template", "spec"}
	case address.Group == "" && address.Resource == "replicationcontrollers" && kind == "ReplicationController":
		return []string{"spec", "template", "spec"}
	case address.Group == "apps" && ((address.Resource == "deployments" && kind == "Deployment") ||
		(address.Resource == "daemonsets" && kind == "DaemonSet") ||
		(address.Resource == "replicasets" && kind == "ReplicaSet") ||
		(address.Resource == "statefulsets" && kind == "StatefulSet")):
		return []string{"spec", "template", "spec"}
	case address.Group == "batch" && address.Resource == "jobs" && kind == "Job":
		return []string{"spec", "template", "spec"}
	case address.Group == "batch" && address.Resource == "cronjobs" && kind == "CronJob":
		return []string{"spec", "jobTemplate", "spec", "template", "spec"}
	default:
		return nil
	}
}

// validateResourcePodSpec rejects credential-bearing or host-escalating built-in Pod specs.
// Automount must be explicitly false; neither EACH nor a successful Kubernetes SSAR overrides
// the deny. ConfigMap/downward-API/emptyDir volumes are allowed; unknown backing storage is not
// silently classified as safe. The exact registered API schema remains an independent check.
// validateResourcePodSpec отклоняет credential-bearing либо host-escalating built-in Pod specs.
// Automount явно false; ни EACH, ни успешный Kubernetes SSAR не отменяют deny.
// ConfigMap/downward-API/emptyDir volumes допустимы; unknown backing storage не считается
// безопасным молча. Exact registered API schema остаётся самостоятельной проверкой.
func validateResourcePodSpec(object map[string]any) error {
	return validateResourcePodSpecWithStorage(object, nil)
}

// validateResourcePodSpecWithStorage permits PVCs only by verified volume name and mode.
// validateResourcePodSpecWithStorage допускает PVC только по verified volume name/mode.
func validateResourcePodSpecWithStorage(object map[string]any, qualified map[string]bool) error {
	raw, err := json.Marshal(object)
	if err != nil {
		return errors.New(resourcePolicyForbidden)
	}
	defer clear(raw)
	var spec corev1.PodSpec
	strictErrors, err := strictjson.UnmarshalStrict(raw, &spec)
	if err != nil || len(strictErrors) != 0 || spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken ||
		spec.HostNetwork || spec.HostPID || spec.HostIPC || spec.ShareProcessNamespace != nil && *spec.ShareProcessNamespace ||
		len(spec.ImagePullSecrets) != 0 || len(spec.EphemeralContainers) != 0 || len(spec.Containers) == 0 ||
		(spec.ServiceAccountName != "" && spec.ServiceAccountName != "default") ||
		(spec.DeprecatedServiceAccount != "" && spec.DeprecatedServiceAccount != "default") {
		return errors.New(resourcePolicyForbidden)
	}
	if security := spec.SecurityContext; security != nil &&
		(security.SELinuxOptions != nil || len(security.Sysctls) != 0 ||
			security.WindowsOptions != nil && security.WindowsOptions.HostProcess != nil && *security.WindowsOptions.HostProcess ||
			security.SeccompProfile != nil && security.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined ||
			security.AppArmorProfile != nil && security.AppArmorProfile.Type == corev1.AppArmorProfileTypeUnconfined) {
		return errors.New(resourcePolicyForbidden)
	}
	for _, container := range append(slices.Clone(spec.Containers), spec.InitContainers...) {
		devices := container.VolumeDevices
		container.VolumeDevices = nil
		if validateResourceContainer(container) != nil {
			return errors.New(resourcePolicyForbidden)
		}
		for _, device := range devices {
			block, exists := qualified[device.Name]
			if !exists || !block {
				return errors.New(resourceStoragePolicyRequired)
			}
		}
		for _, mount := range container.VolumeMounts {
			if block, exists := qualified[mount.Name]; exists && block {
				return errors.New(resourcePolicyForbidden)
			}
		}
	}
	for _, volume := range spec.Volumes {
		if _, allowed := qualified[volume.Name]; allowed && volume.PersistentVolumeClaim != nil {
			remainder := volume.VolumeSource
			remainder.PersistentVolumeClaim = nil
			raw, err := json.Marshal(remainder)
			if err != nil || string(raw) != "{}" {
				return errors.New(resourcePolicyForbidden)
			}
			continue
		}
		if err := validateResourceVolume(volume.VolumeSource); err != nil {
			return err
		}
	}
	return nil
}

// validateResourceContainer checks all init/ordinary containers without accepting Secret env
// aliases, device mounts or privilege settings. Default runtime capabilities are not a grant
// to add arbitrary capabilities; only NET_BIND_SERVICE is an explicit safe addition here.
// validateResourceContainer проверяет все init/ordinary containers без Secret env aliases,
// device mounts либо privilege settings. Default runtime capabilities не разрешают
// произвольные added capabilities; здесь явно допустим только NET_BIND_SERVICE.
func validateResourceContainer(container corev1.Container) error {
	if len(container.VolumeDevices) != 0 {
		return errors.New(resourcePolicyForbidden)
	}
	for _, port := range container.Ports {
		if port.HostPort != 0 || port.HostIP != "" {
			return errors.New(resourcePolicyForbidden)
		}
	}
	for _, mount := range container.VolumeMounts {
		if mount.MountPropagation != nil && *mount.MountPropagation != corev1.MountPropagationNone {
			return errors.New(resourcePolicyForbidden)
		}
	}
	for _, source := range container.EnvFrom {
		if source.SecretRef != nil {
			return errors.New(resourcePolicyForbidden)
		}
	}
	for _, variable := range container.Env {
		if variable.ValueFrom != nil && variable.ValueFrom.SecretKeyRef != nil {
			return errors.New(resourcePolicyForbidden)
		}
	}
	security := container.SecurityContext
	if security == nil {
		return nil
	}
	if security.Privileged != nil && *security.Privileged || security.AllowPrivilegeEscalation != nil && *security.AllowPrivilegeEscalation ||
		security.ProcMount != nil && *security.ProcMount == corev1.UnmaskedProcMount || security.SELinuxOptions != nil ||
		security.WindowsOptions != nil && security.WindowsOptions.HostProcess != nil && *security.WindowsOptions.HostProcess ||
		security.SeccompProfile != nil && security.SeccompProfile.Type == corev1.SeccompProfileTypeUnconfined ||
		security.AppArmorProfile != nil && security.AppArmorProfile.Type == corev1.AppArmorProfileTypeUnconfined {
		return errors.New(resourcePolicyForbidden)
	}
	if security.Capabilities != nil {
		for _, capability := range security.Capabilities.Add {
			if capability != "NET_BIND_SERVICE" {
				return errors.New(resourcePolicyForbidden)
			}
		}
	}
	return nil
}

// validateResourceVolume permits only fully recognized noncredential sources. Marshaling the
// remainder after clearing permitted fields catches mixed or newly introduced volume kinds,
// rather than allowing a Secret/CSI/PVC source beside an otherwise harmless emptyDir field.
// validateResourceVolume допускает только полностью распознанные noncredential sources.
// Marshaling remainder после очистки allowed fields выявляет mixed/new volume kinds, не позволяя
// Secret/CSI/PVC source рядом с внешне безопасным emptyDir field.
func validateResourceVolume(source corev1.VolumeSource) error {
	if source.PersistentVolumeClaim != nil || source.Ephemeral != nil || source.CSI != nil && source.CSI.NodePublishSecretRef == nil {
		return errors.New(resourceStoragePolicyRequired)
	}
	allowed := 0
	for _, present := range []bool{source.EmptyDir != nil, source.ConfigMap != nil, source.DownwardAPI != nil, source.Projected != nil} {
		if present {
			allowed++
		}
	}
	if allowed != 1 {
		return errors.New(resourcePolicyForbidden)
	}
	if source.Projected != nil {
		if len(source.Projected.Sources) == 0 {
			return errors.New(resourcePolicyForbidden)
		}
		for _, projection := range source.Projected.Sources {
			if (projection.ConfigMap == nil) == (projection.DownwardAPI == nil) {
				return errors.New(resourcePolicyForbidden)
			}
			projection.ConfigMap, projection.DownwardAPI = nil, nil
			raw, err := json.Marshal(projection)
			if err != nil || string(raw) != "{}" {
				return errors.New(resourcePolicyForbidden)
			}
		}
	}
	source.EmptyDir, source.ConfigMap, source.DownwardAPI, source.Projected = nil, nil, nil, nil
	raw, err := json.Marshal(source)
	if err != nil || string(raw) != "{}" {
		return errors.New(resourcePolicyForbidden)
	}
	return nil
}
