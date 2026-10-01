package runtimecontract

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	RuntimeVolumeEphemeralDisk   = "EPHEMERAL_DISK"
	RuntimeVolumeEphemeralMemory = "EPHEMERAL_MEMORY"

	RuntimeEgressDNS             = "DNS"
	RuntimeEgressRuntimeCallback = "RUNTIME_CALLBACK"
	RuntimeEgressProviderProxy   = "PROVIDER_PROXY"
	RuntimeEgressKubernetesAPI   = "KUBERNETES_API"

	RuntimeProtocolTCP = "TCP"
	RuntimeProtocolUDP = "UDP"

	RuntimeWebAccessNone              = "NONE"
	RuntimeWebAccessAllowlistReadOnly = "ALLOWLIST_READ_ONLY"
	RuntimeWebAccessAllowlistFull     = "ALLOWLIST_FULL"
	RuntimeWebAccessFullPublic        = "FULL_PUBLIC"
	RuntimeWebProtocolHTTPS           = "HTTPS"

	RuntimeHTTPMethodGet     = "GET"
	RuntimeHTTPMethodHead    = "HEAD"
	RuntimeHTTPMethodOptions = "OPTIONS"
	RuntimeHTTPMethodPost    = "POST"
	RuntimeHTTPMethodPut     = "PUT"
	RuntimeHTTPMethodPatch   = "PATCH"
	RuntimeHTTPMethodDelete  = "DELETE"

	RuntimeKubernetesAccessNone             = "NONE"
	RuntimeKubernetesAccessReadOwnExecution = "READ_OWN_EXECUTION"
	RuntimeKubernetesNamespace              = "kodex-runtime"
)

var runtimeVolumeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$|^[a-z]$`)

var reservedRuntimeVolumeNames = map[string]struct{}{
	"callback-ca": {}, "callback-client": {}, "kube-api-access": {}, "provider-auth": {},
	"provider-socket": {}, "provider-tmp": {}, "runtime-input": {}, "session": {}, "tmp": {},
}

// RuntimeResourcePolicy использует целые единицы вместо произвольных
// Kubernetes quantities. Это сохраняет browser contract типизированным и
// исключает неоднозначную канонизацию значений.
type RuntimeResourcePolicy struct {
	CPURequestMilli            int64 `json:"cpu_request_milli"`
	CPULimitMilli              int64 `json:"cpu_limit_milli"`
	MemoryRequestMiB           int64 `json:"memory_request_mib"`
	MemoryLimitMiB             int64 `json:"memory_limit_mib"`
	EphemeralStorageRequestMiB int64 `json:"ephemeral_storage_request_mib"`
	EphemeralStorageLimitMiB   int64 `json:"ephemeral_storage_limit_mib"`
}

// RuntimeVolume разрешает только execution-scoped emptyDir. Имя источника,
// host path, PVC, Secret, ConfigMap и произвольный mount path не принимаются.
type RuntimeVolume struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	SizeMiB   int64  `json:"size_mib"`
	MountPath string `json:"mount_path"`
}

type RuntimeNetworkEgress struct {
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	Port        int32  `json:"port"`
}

// RuntimeWebAccessRule задаёт доступ к публичному HTTPS endpoint. Scoped
// wildcard не равен глобальному wildcard: *.example.com разрешает только один
// уровень, **.example.com — apex и любое число поддоменов.
type RuntimeWebAccessRule struct {
	DomainPattern string   `json:"domain_pattern"`
	Protocol      string   `json:"protocol"`
	Port          int32    `json:"port"`
	HTTPMethods   []string `json:"http_methods"`
}

type RuntimeWebAccess struct {
	Mode  string                 `json:"mode"`
	Rules []RuntimeWebAccessRule `json:"rules"`
}

type RuntimeNetworkPolicy struct {
	DenyByDefault bool                   `json:"deny_by_default"`
	Egress        []RuntimeNetworkEgress `json:"egress"`
	WebAccess     RuntimeWebAccess       `json:"web_access"`
}

// RuntimeKubernetesAccessProfile является environment-level выбором. Exact
// resourceNames и ServiceAccount назначаются сервером для каждой execution.
type RuntimeKubernetesAccessProfile struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
}

type RuntimeKubernetesRule struct {
	APIGroup      string   `json:"api_group"`
	Resource      string   `json:"resource"`
	Verbs         []string `json:"verbs"`
	ResourceNames []string `json:"resource_names"`
}

type RuntimeKubernetesAccess struct {
	Profile            RuntimeKubernetesAccessProfile `json:"profile"`
	ServiceAccountName string                         `json:"service_account_name"`
	Rules              []RuntimeKubernetesRule        `json:"rules"`
	Digest             string                         `json:"digest"`
}

type RuntimeEnvironmentPolicy struct {
	Resources        RuntimeResourcePolicy          `json:"resources"`
	Volumes          []RuntimeVolume                `json:"volumes"`
	Network          RuntimeNetworkPolicy           `json:"network"`
	KubernetesAccess RuntimeKubernetesAccessProfile `json:"kubernetes_access"`
	ResourcesDigest  string                         `json:"resources_digest,omitempty"`
	VolumesDigest    string                         `json:"volumes_digest,omitempty"`
	NetworkDigest    string                         `json:"network_digest,omitempty"`
	RBACDigest       string                         `json:"rbac_digest,omitempty"`
}

type RuntimeEnvironmentPolicyInput struct {
	Resources           RuntimeResourcePolicy `json:"resources"`
	Volumes             []RuntimeVolume       `json:"volumes"`
	NetworkDestinations []string              `json:"network_destinations"`
	WebAccess           RuntimeWebAccess      `json:"web_access"`
	KubernetesAccess    string                `json:"kubernetes_access"`
}

func DefaultRuntimeEnvironmentPolicy() RuntimeEnvironmentPolicy {
	policy := RuntimeEnvironmentPolicy{
		Resources: RuntimeResourcePolicy{
			CPURequestMilli: 2000, CPULimitMilli: 2000,
			MemoryRequestMiB: 4096, MemoryLimitMiB: 4096,
			EphemeralStorageRequestMiB: 1024, EphemeralStorageLimitMiB: 4096,
		},
		Network: RuntimeNetworkPolicy{DenyByDefault: true, Egress: []RuntimeNetworkEgress{
			{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolTCP, Port: 53},
			{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolUDP, Port: 53},
			{Destination: RuntimeEgressProviderProxy, Protocol: RuntimeProtocolTCP, Port: 8084},
			{Destination: RuntimeEgressRuntimeCallback, Protocol: RuntimeProtocolTCP, Port: 8444},
		}, WebAccess: RuntimeWebAccess{Mode: RuntimeWebAccessNone, Rules: []RuntimeWebAccessRule{}}},
		KubernetesAccess: RuntimeKubernetesAccessProfile{Kind: RuntimeKubernetesAccessNone, Namespace: RuntimeKubernetesNamespace},
	}
	normalized, _ := NormalizeRuntimeEnvironmentPolicy(policy)
	return normalized
}

func NormalizeRuntimeEnvironmentPolicy(input RuntimeEnvironmentPolicy) (RuntimeEnvironmentPolicy, error) {
	if input.Resources == (RuntimeResourcePolicy{}) && len(input.Volumes) == 0 && len(input.Network.Egress) == 0 && input.KubernetesAccess.Kind == "" {
		input = DefaultRuntimeEnvironmentPolicyWithoutDigests()
	}
	if err := validateRuntimeResources(input.Resources); err != nil {
		return RuntimeEnvironmentPolicy{}, err
	}
	volumes := append([]RuntimeVolume{}, input.Volumes...)
	for index := range volumes {
		mountPath, err := RuntimeVolumeMountPath(volumes[index].Name)
		if err != nil || volumes[index].MountPath != "" && volumes[index].MountPath != mountPath {
			return RuntimeEnvironmentPolicy{}, errors.New("runtime volume mount path is invalid")
		}
		volumes[index].MountPath = mountPath
	}
	sort.Slice(volumes, func(left, right int) bool { return volumes[left].Name < volumes[right].Name })
	if err := validateRuntimeVolumes(volumes); err != nil {
		return RuntimeEnvironmentPolicy{}, err
	}
	network := RuntimeNetworkPolicy{DenyByDefault: input.Network.DenyByDefault, Egress: append([]RuntimeNetworkEgress{}, input.Network.Egress...), WebAccess: RuntimeWebAccess{Mode: input.Network.WebAccess.Mode, Rules: append([]RuntimeWebAccessRule{}, input.Network.WebAccess.Rules...)}}
	if network.WebAccess.Mode == "" {
		network.WebAccess.Mode = RuntimeWebAccessNone
	}
	sort.Slice(network.Egress, func(left, right int) bool {
		if network.Egress[left].Destination != network.Egress[right].Destination {
			return network.Egress[left].Destination < network.Egress[right].Destination
		}
		if network.Egress[left].Protocol != network.Egress[right].Protocol {
			return network.Egress[left].Protocol < network.Egress[right].Protocol
		}
		return network.Egress[left].Port < network.Egress[right].Port
	})
	for index := range network.WebAccess.Rules {
		network.WebAccess.Rules[index].DomainPattern = strings.ToLower(strings.TrimSpace(network.WebAccess.Rules[index].DomainPattern))
		network.WebAccess.Rules[index].Protocol = strings.ToUpper(strings.TrimSpace(network.WebAccess.Rules[index].Protocol))
		for methodIndex := range network.WebAccess.Rules[index].HTTPMethods {
			network.WebAccess.Rules[index].HTTPMethods[methodIndex] = strings.ToUpper(strings.TrimSpace(network.WebAccess.Rules[index].HTTPMethods[methodIndex]))
		}
		sort.Strings(network.WebAccess.Rules[index].HTTPMethods)
	}
	sort.Slice(network.WebAccess.Rules, func(left, right int) bool {
		return network.WebAccess.Rules[left].DomainPattern < network.WebAccess.Rules[right].DomainPattern
	})
	access := input.KubernetesAccess
	if access.Namespace == "" {
		access.Namespace = RuntimeKubernetesNamespace
	}
	if err := validateRuntimeNetwork(network, access); err != nil {
		return RuntimeEnvironmentPolicy{}, err
	}
	if !containsString([]string{RuntimeKubernetesAccessNone, RuntimeKubernetesAccessReadOwnExecution}, access.Kind) || access.Namespace != RuntimeKubernetesNamespace {
		return RuntimeEnvironmentPolicy{}, errors.New("runtime Kubernetes access profile is invalid")
	}
	result := RuntimeEnvironmentPolicy{Resources: input.Resources, Volumes: volumes, Network: network, KubernetesAccess: access}
	result.ResourcesDigest = digestRuntimeResources(result.Resources)
	result.VolumesDigest = digestRuntimeVolumes(result.Volumes)
	result.NetworkDigest = digestRuntimeNetwork(result.Network)
	result.RBACDigest = digestRuntimeKubernetesProfile(result.KubernetesAccess)
	return result, nil
}

func DefaultRuntimeEnvironmentPolicyWithoutDigests() RuntimeEnvironmentPolicy {
	return RuntimeEnvironmentPolicy{
		Resources: RuntimeResourcePolicy{CPURequestMilli: 2000, CPULimitMilli: 2000, MemoryRequestMiB: 4096, MemoryLimitMiB: 4096, EphemeralStorageRequestMiB: 1024, EphemeralStorageLimitMiB: 4096},
		Network: RuntimeNetworkPolicy{DenyByDefault: true, Egress: []RuntimeNetworkEgress{
			{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolTCP, Port: 53},
			{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolUDP, Port: 53},
			{Destination: RuntimeEgressProviderProxy, Protocol: RuntimeProtocolTCP, Port: 8084},
			{Destination: RuntimeEgressRuntimeCallback, Protocol: RuntimeProtocolTCP, Port: 8444},
		}, WebAccess: RuntimeWebAccess{Mode: RuntimeWebAccessNone, Rules: []RuntimeWebAccessRule{}}},
		KubernetesAccess: RuntimeKubernetesAccessProfile{Kind: RuntimeKubernetesAccessNone, Namespace: RuntimeKubernetesNamespace},
	}
}

func RuntimeEnvironmentPolicyFromInput(input RuntimeEnvironmentPolicyInput) (RuntimeEnvironmentPolicy, error) {
	access := RuntimeKubernetesAccessProfile{Kind: input.KubernetesAccess, Namespace: RuntimeKubernetesNamespace}
	required := map[string]struct{}{
		RuntimeEgressDNS: {}, RuntimeEgressProviderProxy: {}, RuntimeEgressRuntimeCallback: {},
	}
	if access.Kind == RuntimeKubernetesAccessReadOwnExecution {
		required[RuntimeEgressKubernetesAPI] = struct{}{}
	}
	if len(input.NetworkDestinations) != len(required) {
		return RuntimeEnvironmentPolicy{}, errors.New("runtime network destination set is invalid")
	}
	for _, destination := range input.NetworkDestinations {
		if _, ok := required[destination]; !ok {
			return RuntimeEnvironmentPolicy{}, errors.New("runtime network destination is invalid")
		}
		delete(required, destination)
	}
	if len(required) != 0 {
		return RuntimeEnvironmentPolicy{}, errors.New("runtime network destination set is incomplete")
	}
	egress := []RuntimeNetworkEgress{
		{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolTCP, Port: 53},
		{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolUDP, Port: 53},
		{Destination: RuntimeEgressProviderProxy, Protocol: RuntimeProtocolTCP, Port: 8084},
		{Destination: RuntimeEgressRuntimeCallback, Protocol: RuntimeProtocolTCP, Port: 8444},
	}
	if access.Kind == RuntimeKubernetesAccessReadOwnExecution {
		egress = append(egress, RuntimeNetworkEgress{Destination: RuntimeEgressKubernetesAPI, Protocol: RuntimeProtocolTCP, Port: 443})
	}
	return NormalizeRuntimeEnvironmentPolicy(RuntimeEnvironmentPolicy{
		Resources: input.Resources, Volumes: input.Volumes,
		Network: RuntimeNetworkPolicy{DenyByDefault: true, Egress: egress, WebAccess: input.WebAccess}, KubernetesAccess: access,
	})
}

func RuntimeEnvironmentPolicyDigest(policy RuntimeEnvironmentPolicy) (string, error) {
	normalized, err := NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		return "", err
	}
	return digestParts("runtime-environment-policy-v1", normalized.ResourcesDigest, normalized.VolumesDigest, normalized.NetworkDigest, normalized.RBACDigest), nil
}

func RuntimeKubernetesAccessForExecution(profile RuntimeKubernetesAccessProfile, serviceAccountName, podName string) (RuntimeKubernetesAccess, error) {
	if profile.Namespace != "kodex-runtime" || !validDNSLabelContract(serviceAccountName) || !validDNSLabelContract(podName) {
		return RuntimeKubernetesAccess{}, errors.New("runtime Kubernetes execution identity is invalid")
	}
	result := RuntimeKubernetesAccess{Profile: profile, ServiceAccountName: serviceAccountName, Rules: []RuntimeKubernetesRule{}}
	switch profile.Kind {
	case RuntimeKubernetesAccessNone:
		result.ServiceAccountName = serviceAccountName
	case RuntimeKubernetesAccessReadOwnExecution:
		result.Rules = []RuntimeKubernetesRule{
			{APIGroup: "", Resource: "pods", Verbs: []string{"get"}, ResourceNames: []string{podName}},
			{APIGroup: "", Resource: "pods/log", Verbs: []string{"get"}, ResourceNames: []string{podName}},
		}
	default:
		return RuntimeKubernetesAccess{}, errors.New("runtime Kubernetes access profile is invalid")
	}
	result.Digest = digestRuntimeKubernetesAccess(result)
	return result, nil
}

func ValidateRuntimeKubernetesAccess(input RuntimeKubernetesAccess) error {
	expected, err := RuntimeKubernetesAccessForExecution(input.Profile, input.ServiceAccountName, runtimeKubernetesRulePodName(input.Rules))
	if err != nil || expected.Digest != input.Digest || !equalRuntimeKubernetesRules(expected.Rules, input.Rules) {
		return errors.New("runtime Kubernetes access is invalid")
	}
	return nil
}

func RuntimeVolumeMountPath(name string) (string, error) {
	if !runtimeVolumeNamePattern.MatchString(name) {
		return "", errors.New("runtime volume name is invalid")
	}
	return "/workspace/.kodex/volumes/" + name, nil
}

func RuntimeTurnPodName(leaseRef string) string {
	return runtimeResourceName("runtime-turn-", leaseRef)
}

func RuntimeServiceAccountName(leaseRef string) string {
	return runtimeResourceName("runtime-sa-", leaseRef)
}

func RuntimeRoleName(leaseRef string) string {
	return runtimeResourceName("runtime-role-", leaseRef)
}

func RuntimeRoleBindingName(leaseRef string) string {
	return runtimeResourceName("runtime-rb-", leaseRef)
}

func RuntimeNetworkPolicyName(leaseRef string) string {
	return runtimeResourceName("runtime-net-", leaseRef)
}

func validateRuntimeResources(value RuntimeResourcePolicy) error {
	if value.CPURequestMilli < 100 || value.CPURequestMilli > 8000 || value.CPULimitMilli < value.CPURequestMilli || value.CPULimitMilli > 16000 ||
		value.MemoryRequestMiB < 128 || value.MemoryRequestMiB > 32768 || value.MemoryLimitMiB < value.MemoryRequestMiB || value.MemoryLimitMiB > 65536 ||
		value.EphemeralStorageRequestMiB < 256 || value.EphemeralStorageRequestMiB > 20480 ||
		value.EphemeralStorageLimitMiB < value.EphemeralStorageRequestMiB || value.EphemeralStorageLimitMiB > 102400 {
		return errors.New("runtime resource policy is outside platform admission limits")
	}
	return nil
}

func validateRuntimeVolumes(values []RuntimeVolume) error {
	if len(values) > 16 {
		return errors.New("runtime volume limit exceeded")
	}
	for index, value := range values {
		_, reserved := reservedRuntimeVolumeNames[value.Name]
		if !runtimeVolumeNamePattern.MatchString(value.Name) || reserved || !containsString([]string{RuntimeVolumeEphemeralDisk, RuntimeVolumeEphemeralMemory}, value.Kind) ||
			value.SizeMiB < 16 || value.SizeMiB > 10240 || index > 0 && values[index-1].Name == value.Name {
			return errors.New("runtime volume is invalid")
		}
	}
	return nil
}

func validateRuntimeNetwork(value RuntimeNetworkPolicy, access RuntimeKubernetesAccessProfile) error {
	if !value.DenyByDefault || len(value.Egress) < 4 || len(value.Egress) > 5 {
		return errors.New("runtime network policy must be deny-by-default")
	}
	required := map[string]struct{}{
		RuntimeEgressDNS + "|TCP|53": {}, RuntimeEgressDNS + "|UDP|53": {},
		RuntimeEgressProviderProxy + "|TCP|8084": {}, RuntimeEgressRuntimeCallback + "|TCP|8444": {},
	}
	if access.Kind == RuntimeKubernetesAccessReadOwnExecution {
		required[RuntimeEgressKubernetesAPI+"|TCP|443"] = struct{}{}
	}
	if len(value.Egress) != len(required) {
		return errors.New("runtime network policy destination set is invalid")
	}
	for _, item := range value.Egress {
		key := item.Destination + "|" + item.Protocol + "|" + strconv.FormatInt(int64(item.Port), 10)
		if _, ok := required[key]; !ok {
			return errors.New("runtime network policy contains wildcard or unsupported destination")
		}
		delete(required, key)
	}
	if len(required) != 0 {
		return errors.New("runtime network policy is incomplete")
	}
	return validateRuntimeWebAccess(value.WebAccess)
}

func validateRuntimeWebAccess(value RuntimeWebAccess) error {
	if !containsString([]string{RuntimeWebAccessNone, RuntimeWebAccessAllowlistReadOnly, RuntimeWebAccessAllowlistFull, RuntimeWebAccessFullPublic}, value.Mode) {
		return errors.New("runtime web access mode is invalid")
	}
	if value.Mode == RuntimeWebAccessNone || value.Mode == RuntimeWebAccessFullPublic {
		if len(value.Rules) != 0 {
			return errors.New("runtime web access rules are invalid for selected mode")
		}
		return nil
	}
	if len(value.Rules) == 0 || len(value.Rules) > 64 {
		return errors.New("runtime web access allowlist is invalid")
	}
	previous := ""
	for _, rule := range value.Rules {
		if !validRuntimeDomainPattern(rule.DomainPattern) || rule.Protocol != RuntimeWebProtocolHTTPS || rule.Port != 443 || rule.DomainPattern == previous {
			return errors.New("runtime web access rule is invalid")
		}
		expected := runtimeWebAccessMethods(value.Mode)
		if len(rule.HTTPMethods) != len(expected) {
			return errors.New("runtime web access method set is invalid")
		}
		for index, method := range rule.HTTPMethods {
			if method != expected[index] {
				return errors.New("runtime web access method set is invalid")
			}
		}
		previous = rule.DomainPattern
	}
	return nil
}

func runtimeWebAccessMethods(mode string) []string {
	if mode == RuntimeWebAccessAllowlistReadOnly {
		return []string{RuntimeHTTPMethodGet, RuntimeHTTPMethodHead, RuntimeHTTPMethodOptions}
	}
	return []string{RuntimeHTTPMethodDelete, RuntimeHTTPMethodGet, RuntimeHTTPMethodHead, RuntimeHTTPMethodOptions, RuntimeHTTPMethodPatch, RuntimeHTTPMethodPost, RuntimeHTTPMethodPut}
}

func validRuntimeDomainPattern(value string) bool {
	if value == "" || value == "*" || len(value) > 253 || strings.ContainsAny(value, "/:@ \\") {
		return false
	}
	host := value
	if strings.HasPrefix(host, "**.") {
		host = strings.TrimPrefix(host, "**.")
	} else if strings.HasPrefix(host, "*.") {
		host = strings.TrimPrefix(host, "*.")
	}
	if strings.Contains(host, "*") || net.ParseIP(host) != nil || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return false
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validDNSLabelContract(label) {
			return false
		}
	}
	return true
}

func digestRuntimeResources(value RuntimeResourcePolicy) string {
	return digestParts("resources-v1", strconv.FormatInt(value.CPURequestMilli, 10), strconv.FormatInt(value.CPULimitMilli, 10),
		strconv.FormatInt(value.MemoryRequestMiB, 10), strconv.FormatInt(value.MemoryLimitMiB, 10),
		strconv.FormatInt(value.EphemeralStorageRequestMiB, 10), strconv.FormatInt(value.EphemeralStorageLimitMiB, 10))
}

func digestRuntimeVolumes(values []RuntimeVolume) string {
	parts := []string{"volumes-v1"}
	for _, value := range values {
		parts = append(parts, value.Name, value.Kind, strconv.FormatInt(value.SizeMiB, 10), value.MountPath)
	}
	return digestParts(parts...)
}

func digestRuntimeNetwork(value RuntimeNetworkPolicy) string {
	parts := []string{"network-v2", strconv.FormatBool(value.DenyByDefault)}
	for _, item := range value.Egress {
		parts = append(parts, item.Destination, item.Protocol, strconv.FormatInt(int64(item.Port), 10))
	}
	parts = append(parts, value.WebAccess.Mode)
	for _, rule := range value.WebAccess.Rules {
		parts = append(parts, rule.DomainPattern, rule.Protocol, strconv.FormatInt(int64(rule.Port), 10))
		parts = append(parts, rule.HTTPMethods...)
	}
	return digestParts(parts...)
}

func digestRuntimeKubernetesProfile(value RuntimeKubernetesAccessProfile) string {
	return digestParts("rbac-profile-v1", value.Kind, value.Namespace)
}

func digestRuntimeKubernetesAccess(value RuntimeKubernetesAccess) string {
	parts := []string{"rbac-effective-v1", value.Profile.Kind, value.Profile.Namespace, value.ServiceAccountName}
	for _, rule := range value.Rules {
		parts = append(parts, rule.APIGroup, rule.Resource)
		parts = append(parts, rule.Verbs...)
		parts = append(parts, rule.ResourceNames...)
	}
	return digestParts(parts...)
}

func digestParts(parts ...string) string {
	var payload bytes.Buffer
	for _, part := range parts {
		payload.WriteString(part)
		payload.WriteByte(0)
	}
	digest := sha256.Sum256(payload.Bytes())
	return hex.EncodeToString(digest[:])
}

func runtimeKubernetesRulePodName(rules []RuntimeKubernetesRule) string {
	if len(rules) == 0 {
		return "runtime-no-access"
	}
	if len(rules[0].ResourceNames) != 1 {
		return ""
	}
	return rules[0].ResourceNames[0]
}

func equalRuntimeKubernetesRules(left, right []RuntimeKubernetesRule) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].APIGroup != right[index].APIGroup || left[index].Resource != right[index].Resource ||
			strings.Join(left[index].Verbs, "\x00") != strings.Join(right[index].Verbs, "\x00") ||
			strings.Join(left[index].ResourceNames, "\x00") != strings.Join(right[index].ResourceNames, "\x00") {
			return false
		}
	}
	return true
}

func validDNSLabelContract(value string) bool {
	if value == "" || len(value) > 63 || strings.HasPrefix(value, "-") || strings.HasSuffix(value, "-") {
		return false
	}
	for _, character := range value {
		if character < 'a' || character > 'z' {
			if character < '0' || character > '9' {
				if character != '-' {
					return false
				}
			}
		}
	}
	return true
}

func runtimeResourceName(prefix, identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return prefix + hex.EncodeToString(digest[:8])
}
