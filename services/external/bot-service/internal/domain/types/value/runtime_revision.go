package value

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

const RuntimeRevisionSchemaVersion = "mattercodex.runtime-revision/v1"

// RuntimeRevisionInput содержит только безопасные разрешённые входы среды выполнения.
type RuntimeRevisionInput struct {
	RoleID                   int64
	RoleName                 string
	RoleType                 string
	RoleUpdatedAt            time.Time
	Instruction              string
	AdvancedSettings         string
	AccountAlias             string
	AuthorizationRevision    string
	CodexAuthSecretRef       string
	CodexAuthBindingRevision string
	GitHubSecretRef          string
	GitHubBindingRevision    string
	RunnerImage              string
	BotServiceURL            string
	WorkspaceStorage         string
	CPURequest               string
	MemoryRequest            string
	MemoryLimit              string
	DevShmSizeLimit          string
	SandboxMode              string
	ConfigOverlay            string
	Repository               RuntimeRepositoryManifest
	Environment              []RuntimeEnvironmentReference
	KubernetesAccess         string
	ServiceAccountName       string
}

// RuntimeRevisionManifest — каноническое безопасное представление фактической конфигурации pod.
type RuntimeRevisionManifest struct {
	SchemaVersion string                        `json:"schema_version"`
	Role          RuntimeRoleManifest           `json:"role"`
	Account       RuntimeAccountManifest        `json:"account"`
	Runner        RuntimeRunnerManifest         `json:"runner"`
	Resources     RuntimeResourceManifest       `json:"resources"`
	Sandbox       RuntimeSandboxManifest        `json:"sandbox"`
	Repository    RuntimeRepositoryManifest     `json:"repository"`
	Environment   []RuntimeEnvironmentReference `json:"environment"`
	Kubernetes    RuntimeKubernetesManifest     `json:"kubernetes"`
}

// RuntimeRoleManifest фиксирует безопасную ревизию роли и инструкций.
type RuntimeRoleManifest struct {
	ID                     int64  `json:"id"`
	Name                   string `json:"name"`
	Type                   string `json:"type"`
	Revision               string `json:"revision"`
	InstructionSHA256      string `json:"instruction_sha256"`
	AdvancedSettingsSHA256 string `json:"advanced_settings_sha256"`
}

// RuntimeAccountManifest фиксирует alias и ревизию авторизации без credential payload.
type RuntimeAccountManifest struct {
	Alias                    string `json:"alias"`
	AuthorizationRevision    string `json:"authorization_revision"`
	CodexAuthSecretRef       string `json:"codex_auth_secret_ref"`
	CodexAuthBindingRevision string `json:"codex_auth_binding_revision"`
	GitHubSecretRef          string `json:"github_secret_ref,omitempty"`
	GitHubBindingRevision    string `json:"github_binding_revision,omitempty"`
}

// RuntimeRunnerManifest фиксирует фактически выбранный образ и внутреннюю конечную точку.
type RuntimeRunnerManifest struct {
	Image         string `json:"image"`
	BotServiceURL string `json:"bot_service_url"`
}

// RuntimeResourceManifest фиксирует нормализованные фактические requests, limits и размеры томов.
type RuntimeResourceManifest struct {
	WorkspaceStorage string `json:"workspace_storage"`
	CPURequest       string `json:"cpu_request"`
	MemoryRequest    string `json:"memory_request"`
	MemoryLimit      string `json:"memory_limit"`
	DevShmSizeLimit  string `json:"dev_shm_size_limit"`
}

// RuntimeSandboxManifest фиксирует режим и необратимый отпечаток overlay без его содержимого.
type RuntimeSandboxManifest struct {
	Mode                string `json:"mode"`
	ConfigOverlaySHA256 string `json:"config_overlay_sha256"`
}

// RuntimeRepositoryManifest фиксирует безопасную привязку checkout.
type RuntimeRepositoryManifest struct {
	Provider      string `json:"provider,omitempty"`
	Owner         string `json:"owner,omitempty"`
	Name          string `json:"name,omitempty"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// RuntimeEnvironmentReference описывает только имя Kubernetes Secret и ключ, но не значение.
type RuntimeEnvironmentReference struct {
	Name            string `json:"name"`
	SecretName      string `json:"secret_name"`
	SecretKey       string `json:"secret_key"`
	BindingRevision string `json:"binding_revision"`
}

// RuntimeKubernetesManifest фиксирует разрешённый профиль доступа pod.
type RuntimeKubernetesManifest struct {
	Access             string `json:"access"`
	ServiceAccountName string `json:"service_account_name"`
	AutomountToken     bool   `json:"automount_service_account_token"`
}

// RuntimeRevision содержит канонический манифест и его SHA-256.
type RuntimeRevision struct {
	Digest       string
	ManifestJSON string
	Manifest     RuntimeRevisionManifest
}

// BuildRuntimeRevision нормализует вход, сортирует коллекции и вычисляет SHA-256 канонического JSON.
func BuildRuntimeRevision(input RuntimeRevisionInput) (RuntimeRevision, error) {
	if input.RoleID <= 0 || strings.TrimSpace(input.RoleName) == "" {
		return RuntimeRevision{}, fmt.Errorf("runtime role identity is required")
	}
	if strings.TrimSpace(input.AccountAlias) == "" || strings.TrimSpace(input.AuthorizationRevision) == "" {
		return RuntimeRevision{}, fmt.Errorf("runtime account alias and authorization revision are required")
	}
	if strings.TrimSpace(input.CodexAuthSecretRef) == "" || strings.TrimSpace(input.RunnerImage) == "" {
		return RuntimeRevision{}, fmt.Errorf("runtime secret reference and runner image are required")
	}
	if strings.TrimSpace(input.CodexAuthBindingRevision) == "" {
		return RuntimeRevision{}, fmt.Errorf("runtime codex auth binding revision is required")
	}
	resources, err := normalizedRuntimeResources(input)
	if err != nil {
		return RuntimeRevision{}, err
	}

	environment := normalizedRuntimeEnvironment(input.Environment)
	manifest := RuntimeRevisionManifest{
		SchemaVersion: RuntimeRevisionSchemaVersion,
		Role: RuntimeRoleManifest{
			ID:                     input.RoleID,
			Name:                   strings.TrimSpace(input.RoleName),
			Type:                   strings.TrimSpace(input.RoleType),
			Revision:               canonicalRevisionTime(input.RoleUpdatedAt),
			InstructionSHA256:      sha256Text(input.Instruction),
			AdvancedSettingsSHA256: sha256Text(input.AdvancedSettings),
		},
		Account: RuntimeAccountManifest{
			Alias:                    strings.TrimSpace(input.AccountAlias),
			AuthorizationRevision:    strings.TrimSpace(input.AuthorizationRevision),
			CodexAuthSecretRef:       strings.TrimSpace(input.CodexAuthSecretRef),
			CodexAuthBindingRevision: strings.TrimSpace(input.CodexAuthBindingRevision),
			GitHubSecretRef:          strings.TrimSpace(input.GitHubSecretRef),
			GitHubBindingRevision:    strings.TrimSpace(input.GitHubBindingRevision),
		},
		Runner: RuntimeRunnerManifest{
			Image:         strings.TrimSpace(input.RunnerImage),
			BotServiceURL: strings.TrimRight(strings.TrimSpace(input.BotServiceURL), "/"),
		},
		Resources: resources,
		Sandbox: RuntimeSandboxManifest{
			Mode:                strings.TrimSpace(input.SandboxMode),
			ConfigOverlaySHA256: sha256Text(input.ConfigOverlay),
		},
		Repository: RuntimeRepositoryManifest{
			Provider:      strings.TrimSpace(input.Repository.Provider),
			Owner:         strings.TrimSpace(input.Repository.Owner),
			Name:          strings.TrimSpace(input.Repository.Name),
			DefaultBranch: strings.TrimSpace(input.Repository.DefaultBranch),
		},
		Environment: environment,
		Kubernetes: RuntimeKubernetesManifest{
			Access:             strings.TrimSpace(input.KubernetesAccess),
			ServiceAccountName: strings.TrimSpace(input.ServiceAccountName),
			AutomountToken:     true,
		},
	}
	body, err := json.Marshal(manifest)
	if err != nil {
		return RuntimeRevision{}, fmt.Errorf("marshal canonical runtime revision: %w", err)
	}
	digest := sha256.Sum256(body)
	return RuntimeRevision{
		Digest:       hex.EncodeToString(digest[:]),
		ManifestJSON: string(body),
		Manifest:     manifest,
	}, nil
}

func normalizedRuntimeEnvironment(items []RuntimeEnvironmentReference) []RuntimeEnvironmentReference {
	byIdentity := make(map[string]RuntimeEnvironmentReference, len(items))
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.SecretName = strings.TrimSpace(item.SecretName)
		item.SecretKey = strings.TrimSpace(item.SecretKey)
		item.BindingRevision = strings.TrimSpace(item.BindingRevision)
		if item.Name == "" || item.SecretName == "" || item.SecretKey == "" || item.BindingRevision == "" {
			continue
		}
		identity := strings.Join([]string{item.Name, item.SecretName, item.SecretKey}, "\x00")
		byIdentity[identity] = item
	}
	identities := make([]string, 0, len(byIdentity))
	for identity := range byIdentity {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	result := make([]RuntimeEnvironmentReference, 0, len(identities))
	names := make(map[string]struct{}, len(identities))
	for _, identity := range identities {
		item := byIdentity[identity]
		if _, exists := names[item.Name]; exists {
			continue
		}
		names[item.Name] = struct{}{}
		result = append(result, item)
	}
	return result
}

func normalizedRuntimeResources(input RuntimeRevisionInput) (RuntimeResourceManifest, error) {
	values := []struct {
		name  string
		value string
	}{
		{name: "workspace storage", value: input.WorkspaceStorage},
		{name: "cpu request", value: input.CPURequest},
		{name: "memory request", value: input.MemoryRequest},
		{name: "memory limit", value: input.MemoryLimit},
		{name: "dev-shm size limit", value: input.DevShmSizeLimit},
	}
	normalized := make([]string, len(values))
	for index, item := range values {
		quantity, err := resource.ParseQuantity(strings.TrimSpace(item.value))
		if err != nil || quantity.Sign() <= 0 {
			return RuntimeResourceManifest{}, fmt.Errorf("runtime %s is invalid", item.name)
		}
		normalized[index] = quantity.String()
	}
	return RuntimeResourceManifest{
		WorkspaceStorage: normalized[0], CPURequest: normalized[1], MemoryRequest: normalized[2],
		MemoryLimit: normalized[3], DevShmSizeLimit: normalized[4],
	}, nil
}

func canonicalRevisionTime(value time.Time) string {
	if value.IsZero() {
		return "epoch"
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sha256Text(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}
