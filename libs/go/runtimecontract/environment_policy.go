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
	// RuntimeVolumeEphemeralDisk selects execution-scoped disk-backed emptyDir storage.
	// RuntimeVolumeEphemeralDisk выбирает дисковое emptyDir-хранилище в пределах исполнения.
	RuntimeVolumeEphemeralDisk = "EPHEMERAL_DISK"
	// RuntimeVolumeEphemeralMemory selects execution-scoped memory-backed emptyDir storage.
	// RuntimeVolumeEphemeralMemory выбирает emptyDir-хранилище в памяти в пределах исполнения.
	RuntimeVolumeEphemeralMemory = "EPHEMERAL_MEMORY"

	// RuntimeEgressDNS names the platform-resolved DNS destination, not an arbitrary endpoint.
	// RuntimeEgressDNS обозначает назначаемый платформой DNS, а не произвольный адрес.
	RuntimeEgressDNS = "DNS"
	// RuntimeEgressRuntimeCallback names the controller callback transport required by a turn.
	// RuntimeEgressRuntimeCallback обозначает транспорт обратного вызова контроллера, необходимый ходу.
	RuntimeEgressRuntimeCallback = "RUNTIME_CALLBACK"
	// RuntimeEgressProviderProxy names the managed model-provider transport destination.
	// RuntimeEgressProviderProxy обозначает управляемое назначение транспорта провайдера модели.
	RuntimeEgressProviderProxy = "PROVIDER_PROXY"

	// RuntimeProtocolTCP identifies TCP in the closed service-egress tuple.
	// RuntimeProtocolTCP обозначает TCP в закрытом наборе параметров служебного исходящего трафика.
	RuntimeProtocolTCP = "TCP"
	// RuntimeProtocolUDP identifies UDP, admitted here only for the DNS destination.
	// RuntimeProtocolUDP обозначает UDP, допускаемый здесь только для DNS.
	RuntimeProtocolUDP = "UDP"

	// RuntimeWebAccessNone disables optional public web access while retaining required service links.
	// RuntimeWebAccessNone отключает необязательный публичный веб-доступ, сохраняя служебные связи.
	RuntimeWebAccessNone = "NONE"
	// RuntimeWebAccessAllowlistReadOnly confines web access to named domains and read-only HTTP methods.
	// RuntimeWebAccessAllowlistReadOnly ограничивает веб-доступ указанными доменами и HTTP-методами чтения.
	RuntimeWebAccessAllowlistReadOnly = "ALLOWLIST_READ_ONLY"
	// RuntimeWebAccessAllowlistFull permits the closed write-capable method set only on listed domains.
	// RuntimeWebAccessAllowlistFull допускает закрытый набор методов с записью лишь на перечисленных доменах.
	RuntimeWebAccessAllowlistFull = "ALLOWLIST_FULL"
	// RuntimeWebAccessFullPublic selects managed public-web access without per-domain rules.
	// RuntimeWebAccessFullPublic выбирает управляемый публичный веб-доступ без правил отдельных доменов.
	RuntimeWebAccessFullPublic = "FULL_PUBLIC"
	// RuntimeWebProtocolHTTPS is the sole admitted protocol for web allowlist rules.
	// RuntimeWebProtocolHTTPS — единственный допускаемый протокол доменных веб-правил.
	RuntimeWebProtocolHTTPS = "HTTPS"

	// RuntimeHTTPMethodGet permits retrieval in both read-only and write-capable web modes.
	// RuntimeHTTPMethodGet разрешает получение данных в веб-режимах чтения и записи.
	RuntimeHTTPMethodGet = "GET"
	// RuntimeHTTPMethodHead permits response metadata reads in either web mode.
	// RuntimeHTTPMethodHead разрешает чтение метаданных ответа в обоих веб-режимах.
	RuntimeHTTPMethodHead = "HEAD"
	// RuntimeHTTPMethodOptions permits capability/preflight requests in either web mode.
	// RuntimeHTTPMethodOptions разрешает запросы возможностей и preflight в обоих веб-режимах.
	RuntimeHTTPMethodOptions = "OPTIONS"
	// RuntimeHTTPMethodPost is available only in the write-capable web method set.
	// RuntimeHTTPMethodPost доступен только в наборе веб-методов с записью.
	RuntimeHTTPMethodPost = "POST"
	// RuntimeHTTPMethodPut is available only in the write-capable web method set.
	// RuntimeHTTPMethodPut доступен только в наборе веб-методов с записью.
	RuntimeHTTPMethodPut = "PUT"
	// RuntimeHTTPMethodPatch is available only in the write-capable web method set.
	// RuntimeHTTPMethodPatch доступен только в наборе веб-методов с записью.
	RuntimeHTTPMethodPatch = "PATCH"
	// RuntimeHTTPMethodDelete is available only in the write-capable web method set.
	// RuntimeHTTPMethodDelete доступен только в наборе веб-методов с записью.
	RuntimeHTTPMethodDelete = "DELETE"

	// RuntimeKubernetesAccessNone is the sole supported profile and never issues API rules.
	// RuntimeKubernetesAccessNone — единственный поддерживаемый профиль, не выдающий правил Kubernetes API.
	RuntimeKubernetesAccessNone = "NONE"
	// RuntimeKubernetesNamespace fixes the server-owned namespace for execution identities.
	// RuntimeKubernetesNamespace закрепляет серверное пространство имён идентичностей исполнения.
	RuntimeKubernetesNamespace = "kodex-runtime"
)

// runtimeVolumeNamePattern limits owner volume names to bounded lowercase DNS-label-like tokens.
// runtimeVolumeNamePattern ограничивает имена томов владельца короткими строчными токенами вида DNS-label.
var runtimeVolumeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}[a-z0-9]$|^[a-z]$`)

// reservedRuntimeVolumeNames prevents user volumes from shadowing platform-owned runtime mounts.
// reservedRuntimeVolumeNames запрещает пользовательским томам подменять платформенные mount-имена runtime.
var reservedRuntimeVolumeNames = map[string]struct{}{
	"callback-ca": {}, "callback-client": {}, "kube-api-access": {}, "provider-auth": {},
	"provider-socket": {}, "provider-tmp": {}, "runtime-input": {}, "session": {}, "tmp": {},
}

// RuntimeResourcePolicy uses integer milli-CPU and MiB quantities plus optional narrower workspace
// quotas, keeping the browser contract typed without ambiguous Kubernetes quantity parsing.
// RuntimeResourcePolicy использует целые milli-CPU и MiB и необязательные меньшие квоты workspace,
// сохраняя типизированный browser-контракт без неоднозначного разбора Kubernetes quantities.
type RuntimeResourcePolicy struct {
	WorkspaceLimits            *RuntimeWorkspaceLimits `json:"workspace_limits,omitempty"`
	CPURequestMilli            int64                   `json:"cpu_request_milli"`
	CPULimitMilli              int64                   `json:"cpu_limit_milli"`
	MemoryRequestMiB           int64                   `json:"memory_request_mib"`
	MemoryLimitMiB             int64                   `json:"memory_limit_mib"`
	EphemeralStorageRequestMiB int64                   `json:"ephemeral_storage_request_mib"`
	EphemeralStorageLimitMiB   int64                   `json:"ephemeral_storage_limit_mib"`
}

// RuntimeVolume describes only execution-scoped emptyDir storage; source names, host paths,
// PVCs, Secrets, ConfigMaps, and arbitrary mount locations cannot be selected through it.
// RuntimeVolume описывает лишь emptyDir в пределах исполнения; имена источников, host path,
// PVC, Secret, ConfigMap и произвольные пути монтирования через него не выбираются.
type RuntimeVolume struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	SizeMiB   int64  `json:"size_mib"`
	MountPath string `json:"mount_path"`
}

// RuntimeNetworkEgress records one closed service destination with its exact protocol and port.
// RuntimeNetworkEgress хранит одно закрытое служебное назначение с точными протоколом и портом.
type RuntimeNetworkEgress struct {
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	Port        int32  `json:"port"`
}

// RuntimeWebAccessRule describes public HTTPS access with an explicit HTTP method subset;
// *.example.com matches one subdomain level, while **.example.com includes the apex and deeper levels.
// RuntimeWebAccessRule описывает публичный HTTPS-доступ с явным подмножеством HTTP-методов;
// *.example.com охватывает один уровень, а **.example.com — основной домен и вложенные уровни.
type RuntimeWebAccessRule struct {
	DomainPattern string   `json:"domain_pattern"`
	Protocol      string   `json:"protocol"`
	Port          int32    `json:"port"`
	HTTPMethods   []string `json:"http_methods"`
}

// RuntimeWebAccess selects one closed public-web mode and its mode-compatible domain rules.
// RuntimeWebAccess выбирает один закрытый режим публичного веб-доступа и совместимые доменные правила.
type RuntimeWebAccess struct {
	Mode  string                 `json:"mode"`
	Rules []RuntimeWebAccessRule `json:"rules"`
}

// RuntimeNetworkPolicy combines mandatory deny-by-default service egress with optional managed web access.
// RuntimeNetworkPolicy объединяет обязательный deny-by-default служебный трафик с управляемым веб-доступом.
type RuntimeNetworkPolicy struct {
	DenyByDefault bool                   `json:"deny_by_default"`
	Egress        []RuntimeNetworkEgress `json:"egress"`
	WebAccess     RuntimeWebAccess       `json:"web_access"`
}

// RuntimeKubernetesAccessProfile fixes the absence of Kubernetes permissions;
// the server-assigned ServiceAccount receives neither a token mount nor RBAC rules.
// RuntimeKubernetesAccessProfile закрепляет отсутствие Kubernetes-прав;
// назначенный сервером ServiceAccount не получает смонтированный токен или правила RBAC.
type RuntimeKubernetesAccessProfile struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
}

// RuntimeKubernetesRule represents an effective API rule shape; the supported NONE profile requires no rules.
// RuntimeKubernetesRule представляет форму правила API; поддерживаемый профиль NONE требует пустой набор.
type RuntimeKubernetesRule struct {
	APIGroup      string   `json:"api_group"`
	Resource      string   `json:"resource"`
	Verbs         []string `json:"verbs"`
	ResourceNames []string `json:"resource_names"`
}

// RuntimeKubernetesAccess binds the server-assigned execution identity and empty effective rules to a digest.
// RuntimeKubernetesAccess связывает серверную идентичность исполнения и пустые эффективные правила с дайджестом.
type RuntimeKubernetesAccess struct {
	Profile            RuntimeKubernetesAccessProfile `json:"profile"`
	ServiceAccountName string                         `json:"service_account_name"`
	Rules              []RuntimeKubernetesRule        `json:"rules"`
	Digest             string                         `json:"digest"`
}

// RuntimeEnvironmentPolicy holds normalized resources, volumes, network, and Kubernetes profile with separate digests.
// RuntimeEnvironmentPolicy хранит нормализованные ресурсы, тома, сеть и профиль Kubernetes с отдельными дайджестами.
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

// RuntimeEnvironmentPolicyInput carries owner-selectable resource/web settings and closed destination names,
// leaving service ports, mount roots, and the Kubernetes namespace to the platform.
// RuntimeEnvironmentPolicyInput содержит выбираемые владельцем ресурсы, веб-настройки и закрытые имена назначений,
// оставляя служебные порты, корни томов и пространство имён Kubernetes платформе.
type RuntimeEnvironmentPolicyInput struct {
	Resources           RuntimeResourcePolicy `json:"resources"`
	Volumes             []RuntimeVolume       `json:"volumes"`
	NetworkDestinations []string              `json:"network_destinations"`
	WebAccess           RuntimeWebAccess      `json:"web_access"`
	KubernetesAccess    string                `json:"kubernetes_access"`
}

// DefaultRuntimeEnvironmentPolicy returns the canonical normalized platform defaults with all four digests.
// DefaultRuntimeEnvironmentPolicy возвращает канонические платформенные настройки с четырьмя дайджестами.
func DefaultRuntimeEnvironmentPolicy() RuntimeEnvironmentPolicy {
	normalized, _ := NormalizeRuntimeEnvironmentPolicy(DefaultRuntimeEnvironmentPolicyWithoutDigests())
	return normalized
}

// NormalizeRuntimeEnvironmentPolicy applies the zero-value default, validates platform bounds,
// normalizes ordering and web methods, then computes independent component digests.
// NormalizeRuntimeEnvironmentPolicy применяет default для нулевого значения, проверяет платформенные пределы,
// нормализует порядок и веб-методы и вычисляет отдельные дайджесты компонентов.
func NormalizeRuntimeEnvironmentPolicy(input RuntimeEnvironmentPolicy) (RuntimeEnvironmentPolicy, error) {
	if input.Resources == (RuntimeResourcePolicy{}) && len(input.Volumes) == 0 && len(input.Network.Egress) == 0 && input.KubernetesAccess.Kind == "" {
		input = DefaultRuntimeEnvironmentPolicyWithoutDigests()
	}
	if err := validateRuntimeResources(input.Resources); err != nil {
		return RuntimeEnvironmentPolicy{}, err
	}
	if input.Resources.WorkspaceLimits != nil {
		if err := input.Resources.WorkspaceLimits.Validate(); err != nil {
			return RuntimeEnvironmentPolicy{}, err
		}
		limits := *input.Resources.WorkspaceLimits
		input.Resources.WorkspaceLimits = &limits
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
	if err := validateRuntimeNetwork(network); err != nil {
		return RuntimeEnvironmentPolicy{}, err
	}
	if access.Kind != RuntimeKubernetesAccessNone || access.Namespace != RuntimeKubernetesNamespace {
		return RuntimeEnvironmentPolicy{}, errors.New("runtime Kubernetes access profile is invalid")
	}
	result := RuntimeEnvironmentPolicy{Resources: input.Resources, Volumes: volumes, Network: network, KubernetesAccess: access}
	result.ResourcesDigest = digestRuntimeResources(result.Resources)
	result.VolumesDigest = digestRuntimeVolumes(result.Volumes)
	result.NetworkDigest = digestRuntimeNetwork(result.Network)
	result.RBACDigest = digestRuntimeKubernetesProfile(result.KubernetesAccess)
	return result, nil
}

// DefaultRuntimeEnvironmentPolicyWithoutDigests supplies fresh unnormalized default values for shared construction.
// DefaultRuntimeEnvironmentPolicyWithoutDigests создаёт новые ненормализованные defaults для общего построения.
func DefaultRuntimeEnvironmentPolicyWithoutDigests() RuntimeEnvironmentPolicy {
	return RuntimeEnvironmentPolicy{
		Resources:        RuntimeResourcePolicy{CPURequestMilli: 2000, CPULimitMilli: 2000, MemoryRequestMiB: 4096, MemoryLimitMiB: 4096, EphemeralStorageRequestMiB: 1024, EphemeralStorageLimitMiB: 4096},
		Network:          defaultRuntimeNetworkPolicy(),
		KubernetesAccess: RuntimeKubernetesAccessProfile{Kind: RuntimeKubernetesAccessNone, Namespace: RuntimeKubernetesNamespace},
	}
}

// defaultRuntimeNetworkPolicy creates the exact mandatory service links and disables optional web access.
// defaultRuntimeNetworkPolicy создаёт точные обязательные служебные связи и отключает необязательный веб-доступ.
func defaultRuntimeNetworkPolicy() RuntimeNetworkPolicy {
	return RuntimeNetworkPolicy{DenyByDefault: true, Egress: []RuntimeNetworkEgress{
		{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolTCP, Port: 53},
		{Destination: RuntimeEgressDNS, Protocol: RuntimeProtocolUDP, Port: 53},
		{Destination: RuntimeEgressProviderProxy, Protocol: RuntimeProtocolTCP, Port: 8084},
		{Destination: RuntimeEgressRuntimeCallback, Protocol: RuntimeProtocolTCP, Port: 8444},
	}, WebAccess: RuntimeWebAccess{Mode: RuntimeWebAccessNone, Rules: []RuntimeWebAccessRule{}}}
}

// RuntimeEnvironmentPolicyFromInput requires the exact closed destination set and NONE Kubernetes profile,
// then combines owner settings with platform-owned ports and normalizes the result.
// RuntimeEnvironmentPolicyFromInput требует точный закрытый набор назначений и профиль Kubernetes NONE,
// затем объединяет настройки владельца с платформенными портами и нормализует результат.
func RuntimeEnvironmentPolicyFromInput(input RuntimeEnvironmentPolicyInput) (RuntimeEnvironmentPolicy, error) {
	if input.KubernetesAccess != RuntimeKubernetesAccessNone {
		return RuntimeEnvironmentPolicy{}, errors.New("runtime Kubernetes access is not supported")
	}
	access := RuntimeKubernetesAccessProfile{Kind: input.KubernetesAccess, Namespace: RuntimeKubernetesNamespace}
	required := map[string]struct{}{
		RuntimeEgressDNS: {}, RuntimeEgressProviderProxy: {}, RuntimeEgressRuntimeCallback: {},
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
	network := defaultRuntimeNetworkPolicy()
	network.WebAccess = input.WebAccess
	return NormalizeRuntimeEnvironmentPolicy(RuntimeEnvironmentPolicy{
		Resources: input.Resources, Volumes: input.Volumes,
		Network: network, KubernetesAccess: access,
	})
}

// RuntimeEnvironmentPolicyDigest hashes the four canonical component digests under the policy version tag.
// RuntimeEnvironmentPolicyDigest хеширует четыре канонических дайджеста компонентов с версионным тегом политики.
func RuntimeEnvironmentPolicyDigest(policy RuntimeEnvironmentPolicy) (string, error) {
	normalized, err := NormalizeRuntimeEnvironmentPolicy(policy)
	if err != nil {
		return "", err
	}
	return digestParts("runtime-environment-policy-v1", normalized.ResourcesDigest, normalized.VolumesDigest, normalized.NetworkDigest, normalized.RBACDigest), nil
}

// RuntimeKubernetesAccessForExecution validates server-assigned names and materializes only NONE access with no rules.
// RuntimeKubernetesAccessForExecution проверяет серверные имена и материализует только доступ NONE без правил.
func RuntimeKubernetesAccessForExecution(profile RuntimeKubernetesAccessProfile, serviceAccountName, podName string) (RuntimeKubernetesAccess, error) {
	if profile.Namespace != "kodex-runtime" || !validDNSLabelContract(serviceAccountName) || !validDNSLabelContract(podName) {
		return RuntimeKubernetesAccess{}, errors.New("runtime Kubernetes execution identity is invalid")
	}
	result := RuntimeKubernetesAccess{Profile: profile, ServiceAccountName: serviceAccountName, Rules: []RuntimeKubernetesRule{}}
	switch profile.Kind {
	case RuntimeKubernetesAccessNone:
		result.ServiceAccountName = serviceAccountName
	default:
		return RuntimeKubernetesAccess{}, errors.New("runtime Kubernetes access profile is invalid")
	}
	result.Digest = digestRuntimeKubernetesAccess(result)
	return result, nil
}

// ValidateRuntimeKubernetesAccess reconstructs the supported effective profile and compares rules and digest.
// ValidateRuntimeKubernetesAccess заново строит поддерживаемый профиль и сравнивает правила и дайджест.
func ValidateRuntimeKubernetesAccess(input RuntimeKubernetesAccess) error {
	expected, err := RuntimeKubernetesAccessForExecution(input.Profile, input.ServiceAccountName, runtimeKubernetesRulePodName(input.Rules))
	if err != nil || expected.Digest != input.Digest || !equalRuntimeKubernetesRules(expected.Rules, input.Rules) {
		return errors.New("runtime Kubernetes access is invalid")
	}
	return nil
}

// RuntimeVolumeMountPath derives a volume location under the fixed workspace subtree from a valid name.
// RuntimeVolumeMountPath выводит путь тома внутри фиксированной папки workspace из допустимого имени.
func RuntimeVolumeMountPath(name string) (string, error) {
	if !runtimeVolumeNamePattern.MatchString(name) {
		return "", errors.New("runtime volume name is invalid")
	}
	return "/workspace/.kodex/volumes/" + name, nil
}

// RuntimeTurnPodName derives the stable turn Pod name from the lease identity without granting authority.
// RuntimeTurnPodName выводит постоянное имя Pod хода из идентичности аренды без выдачи полномочий.
func RuntimeTurnPodName(leaseRef string) string {
	return runtimeResourceName("runtime-turn-", leaseRef)
}

// RuntimeServiceAccountName derives the execution ServiceAccount name from the lease identity.
// RuntimeServiceAccountName выводит имя ServiceAccount исполнения из идентичности аренды.
func RuntimeServiceAccountName(leaseRef string) string {
	return runtimeResourceName("runtime-sa-", leaseRef)
}

// RuntimeRoleName derives the lease-scoped Role resource locator, not permission to create rules.
// RuntimeRoleName выводит locator ресурса Role по аренде, а не право создавать правила.
func RuntimeRoleName(leaseRef string) string {
	return runtimeResourceName("runtime-role-", leaseRef)
}

// RuntimeRoleBindingName derives the lease-scoped binding resource locator without assigning permissions.
// RuntimeRoleBindingName выводит locator привязки по аренде без назначения полномочий.
func RuntimeRoleBindingName(leaseRef string) string {
	return runtimeResourceName("runtime-rb-", leaseRef)
}

// RuntimeNetworkPolicyName derives the execution network-policy resource name from its lease identity.
// RuntimeNetworkPolicyName выводит имя сетевой политики исполнения из идентичности аренды.
func RuntimeNetworkPolicyName(leaseRef string) string {
	return runtimeResourceName("runtime-net-", leaseRef)
}

// validateRuntimeResources enforces fixed CPU, memory, and storage admission bounds with limits at least requests.
// validateRuntimeResources проверяет фиксированные пределы CPU, памяти и диска и требует limits не ниже requests.
func validateRuntimeResources(value RuntimeResourcePolicy) error {
	if value.CPURequestMilli < 100 || value.CPURequestMilli > 8000 || value.CPULimitMilli < value.CPURequestMilli || value.CPULimitMilli > 16000 ||
		value.MemoryRequestMiB < 128 || value.MemoryRequestMiB > 32768 || value.MemoryLimitMiB < value.MemoryRequestMiB || value.MemoryLimitMiB > 65536 ||
		value.EphemeralStorageRequestMiB < 256 || value.EphemeralStorageRequestMiB > 20480 ||
		value.EphemeralStorageLimitMiB < value.EphemeralStorageRequestMiB || value.EphemeralStorageLimitMiB > 102400 {
		return errors.New("runtime resource policy is outside platform admission limits")
	}
	return nil
}

// validateRuntimeVolumes checks a list already sorted by normalization for bounded ephemeral volumes,
// adjacent duplicate names, and collisions with reserved platform mounts.
// validateRuntimeVolumes проверяет уже отсортированные нормализацией временные тома, их пределы,
// соседние повторные имена и совпадения с платформенными томами.
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

// validateRuntimeNetwork requires deny-by-default and the four exact service destination/protocol/port tuples
// before validating optional web access; additional or missing links fail closed.
// validateRuntimeNetwork требует deny-by-default и четыре точные служебные тройки назначения, протокола и порта
// до проверки веб-доступа; лишние или отсутствующие связи закрыто отклоняются.
func validateRuntimeNetwork(value RuntimeNetworkPolicy) error {
	if !value.DenyByDefault || len(value.Egress) != 4 {
		return errors.New("runtime network policy must be deny-by-default")
	}
	required := map[string]struct{}{
		RuntimeEgressDNS + "|TCP|53": {}, RuntimeEgressDNS + "|UDP|53": {},
		RuntimeEgressProviderProxy + "|TCP|8084": {}, RuntimeEgressRuntimeCallback + "|TCP|8444": {},
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

// validateRuntimeWebAccess checks mode-compatible rule counts, scoped domains, HTTPS/443, and exact method subsets.
// validateRuntimeWebAccess проверяет число правил режима, доменные шаблоны, HTTPS/443 и точные подмножества методов.
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
		allowed := runtimeWebAccessMethods(value.Mode)
		if len(rule.HTTPMethods) == 0 || len(rule.HTTPMethods) > len(allowed) {
			return errors.New("runtime web access method set is invalid")
		}
		previousMethod := ""
		for _, method := range rule.HTTPMethods {
			if method == previousMethod || !containsString(allowed, method) {
				return errors.New("runtime web access method set is invalid")
			}
			previousMethod = method
		}
		previous = rule.DomainPattern
	}
	return nil
}

// runtimeWebAccessMethods returns a fresh closed method set, narrowing read-only mode to GET/HEAD/OPTIONS.
// runtimeWebAccessMethods возвращает новый закрытый набор методов, сужая режим чтения до GET/HEAD/OPTIONS.
func runtimeWebAccessMethods(mode string) []string {
	if mode == RuntimeWebAccessAllowlistReadOnly {
		return []string{RuntimeHTTPMethodGet, RuntimeHTTPMethodHead, RuntimeHTTPMethodOptions}
	}
	return []string{RuntimeHTTPMethodDelete, RuntimeHTTPMethodGet, RuntimeHTTPMethodHead, RuntimeHTTPMethodOptions, RuntimeHTTPMethodPatch, RuntimeHTTPMethodPost, RuntimeHTTPMethodPut}
}

// validRuntimeDomainPattern accepts canonical multi-label domains with an optional scoped wildcard,
// rejecting IP literals, global wildcards, credentials, ports, and path syntax.
// validRuntimeDomainPattern допускает канонические составные домены с ограниченным wildcard,
// отклоняя IP, общий wildcard, учётные данные, порты и синтаксис путей.
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

// digestRuntimeResources binds integer resource quantities and any explicit workspace quota to their version tag.
// digestRuntimeResources связывает целые ресурсные значения и явные квоты workspace с версионным тегом.
func digestRuntimeResources(value RuntimeResourcePolicy) string {
	parts := []string{"resources-v1", strconv.FormatInt(value.CPURequestMilli, 10), strconv.FormatInt(value.CPULimitMilli, 10),
		strconv.FormatInt(value.MemoryRequestMiB, 10), strconv.FormatInt(value.MemoryLimitMiB, 10),
		strconv.FormatInt(value.EphemeralStorageRequestMiB, 10), strconv.FormatInt(value.EphemeralStorageLimitMiB, 10)}
	if value.WorkspaceLimits != nil {
		parts = append(parts, "workspace-limits-v1", strconv.FormatInt(value.WorkspaceLimits.MaxBytes, 10), strconv.FormatInt(value.WorkspaceLimits.MaxFiles, 10))
	}
	return digestParts(parts...)
}

// digestRuntimeVolumes hashes the ordered volume names, kinds, sizes, and fixed mount locations.
// digestRuntimeVolumes хеширует упорядоченные имена, виды, размеры и фиксированные пути томов.
func digestRuntimeVolumes(values []RuntimeVolume) string {
	parts := []string{"volumes-v1"}
	for _, value := range values {
		parts = append(parts, value.Name, value.Kind, strconv.FormatInt(value.SizeMiB, 10), value.MountPath)
	}
	return digestParts(parts...)
}

// digestRuntimeNetwork pins ordered service egress, public-web mode, domain rules, and HTTP methods.
// digestRuntimeNetwork закрепляет порядок служебных связей, веб-режим, доменные правила и HTTP-методы.
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

// digestRuntimeKubernetesProfile hashes the profile kind and server namespace under a separate domain tag.
// digestRuntimeKubernetesProfile хеширует вид профиля и серверное пространство имён с отдельным тегом.
func digestRuntimeKubernetesProfile(value RuntimeKubernetesAccessProfile) string {
	return digestParts("rbac-profile-v1", value.Kind, value.Namespace)
}

// digestRuntimeKubernetesAccess binds the effective ServiceAccount identity and ordered rule fields.
// digestRuntimeKubernetesAccess связывает эффективную идентичность ServiceAccount и упорядоченные поля правил.
func digestRuntimeKubernetesAccess(value RuntimeKubernetesAccess) string {
	parts := []string{"rbac-effective-v1", value.Profile.Kind, value.Profile.Namespace, value.ServiceAccountName}
	for _, rule := range value.Rules {
		parts = append(parts, rule.APIGroup, rule.Resource)
		parts = append(parts, rule.Verbs...)
		parts = append(parts, rule.ResourceNames...)
	}
	return digestParts(parts...)
}

// digestParts hashes ordered NUL-terminated fields, including the version/domain tag supplied by its caller.
// digestParts хеширует упорядоченные поля с NUL-разделителями, включая переданный вызывающим версионный тег.
func digestParts(parts ...string) string {
	var payload bytes.Buffer
	for _, part := range parts {
		payload.WriteString(part)
		payload.WriteByte(0)
	}
	digest := sha256.Sum256(payload.Bytes())
	return hex.EncodeToString(digest[:])
}

// runtimeKubernetesRulePodName supplies the NONE placeholder or extracts the sole name from the first rule.
// runtimeKubernetesRulePodName возвращает заполнитель NONE либо единственное имя из первого правила.
func runtimeKubernetesRulePodName(rules []RuntimeKubernetesRule) string {
	if len(rules) == 0 {
		return "runtime-no-access"
	}
	if len(rules[0].ResourceNames) != 1 {
		return ""
	}
	return rules[0].ResourceNames[0]
}

// equalRuntimeKubernetesRules compares rule order, API resources, verbs, and exact named-resource lists.
// equalRuntimeKubernetesRules сравнивает порядок правил, API-ресурсы, действия и точные списки имён ресурсов.
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

// validDNSLabelContract accepts a bounded lowercase ASCII DNS label without leading or trailing hyphens.
// validDNSLabelContract допускает ограниченную строчную ASCII DNS-label без дефиса в начале или конце.
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

// runtimeResourceName appends the first eight SHA-256 bytes of an identity to the fixed resource prefix.
// runtimeResourceName добавляет первые восемь байтов SHA-256 идентичности к фиксированному префиксу ресурса.
func runtimeResourceName(prefix, identity string) string {
	digest := sha256.Sum256([]byte(identity))
	return prefix + hex.EncodeToString(digest[:8])
}
