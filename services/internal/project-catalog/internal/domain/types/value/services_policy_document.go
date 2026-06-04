package value

// ServicesPolicyDocument is the normalized JSON representation of services.yaml.
type ServicesPolicyDocument struct {
	APIVersion           string                              `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind                 string                              `json:"kind,omitempty" yaml:"kind,omitempty"`
	Metadata             ServicesPolicyMetadata              `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec                 ServicesPolicySpec                  `json:"spec,omitempty" yaml:"spec,omitempty"`
	Services             []ServicesPolicyService             `json:"services,omitempty" yaml:"services,omitempty"`
	DocumentationSources []ServicesPolicyDocumentationSource `json:"documentationSources,omitempty" yaml:"documentationSources,omitempty"`
}

// ServicesPolicyMetadata stores safe document metadata.
type ServicesPolicyMetadata struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
}

// ServicesPolicySpec stores the policy body used by project-catalog.
type ServicesPolicySpec struct {
	Services             []ServicesPolicyService             `json:"services,omitempty" yaml:"services,omitempty"`
	DeployableServices   []ServicesPolicyService             `json:"deployableServices,omitempty" yaml:"deployableServices,omitempty"`
	DocumentationSources []ServicesPolicyDocumentationSource `json:"documentationSources,omitempty" yaml:"documentationSources,omitempty"`
}

// ServicesPolicyService describes one service entry from normalized services.yaml.
type ServicesPolicyService struct {
	Key                  string                   `json:"key,omitempty" yaml:"key,omitempty"`
	Name                 string                   `json:"name,omitempty" yaml:"name,omitempty"`
	DisplayName          string                   `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Kind                 string                   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Type                 string                   `json:"type,omitempty" yaml:"type,omitempty"`
	RootPath             string                   `json:"rootPath,omitempty" yaml:"rootPath,omitempty"`
	Path                 string                   `json:"path,omitempty" yaml:"path,omitempty"`
	RepositoryID         string                   `json:"repositoryId,omitempty" yaml:"repositoryId,omitempty"`
	DocumentationScopeID string                   `json:"documentationScopeId,omitempty" yaml:"documentationScopeId,omitempty"`
	DependsOn            []string                 `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	DependsOnServiceKeys []string                 `json:"dependsOnServiceKeys,omitempty" yaml:"dependsOnServiceKeys,omitempty"`
	Status               string                   `json:"status,omitempty" yaml:"status,omitempty"`
	Build                *ServicesPolicyBuildSpec `json:"build,omitempty" yaml:"build,omitempty"`
}

// ServicesPolicyBuildSpec описывает проверенный build-вход одного сервиса.
type ServicesPolicyBuildSpec struct {
	ImageRef           string                           `json:"imageRef,omitempty" yaml:"imageRef,omitempty"`
	ImageTag           string                           `json:"imageTag,omitempty" yaml:"imageTag,omitempty"`
	ImageDigest        string                           `json:"imageDigest,omitempty" yaml:"imageDigest,omitempty"`
	BuildContextRef    string                           `json:"buildContextRef,omitempty" yaml:"buildContextRef,omitempty"`
	BuildContextDigest string                           `json:"buildContextDigest,omitempty" yaml:"buildContextDigest,omitempty"`
	DockerfileRef      string                           `json:"dockerfileRef,omitempty" yaml:"dockerfileRef,omitempty"`
	DockerfileDigest   string                           `json:"dockerfileDigest,omitempty" yaml:"dockerfileDigest,omitempty"`
	DockerfileTarget   string                           `json:"dockerfileTarget,omitempty" yaml:"dockerfileTarget,omitempty"`
	BuilderImageRef    string                           `json:"builderImageRef,omitempty" yaml:"builderImageRef,omitempty"`
	AllowedSecretRefs  []ServicesPolicyAllowedSecretRef `json:"allowedSecretRefs,omitempty" yaml:"allowedSecretRefs,omitempty"`
	OutputRefs         []ServicesPolicyOutputRef        `json:"outputRefs,omitempty" yaml:"outputRefs,omitempty"`
}

// ServicesPolicyAllowedSecretRef несёт только ссылку на секрет и ограниченное назначение.
type ServicesPolicyAllowedSecretRef struct {
	SecretRef string `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
	Purpose   string `json:"purpose,omitempty" yaml:"purpose,omitempty"`
}

// ServicesPolicyOutputRef несёт ограниченную ссылку на build-результат.
type ServicesPolicyOutputRef struct {
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Ref  string `json:"ref,omitempty" yaml:"ref,omitempty"`
}

// ServicesPolicyDocumentationSource describes one documentation source from normalized services.yaml.
type ServicesPolicyDocumentationSource struct {
	Key          string `json:"key,omitempty" yaml:"key,omitempty"`
	RepositoryID string `json:"repositoryId,omitempty" yaml:"repositoryId,omitempty"`
	ScopeType    string `json:"scopeType,omitempty" yaml:"scopeType,omitempty"`
	ScopeID      string `json:"scopeId,omitempty" yaml:"scopeId,omitempty"`
	LocalPath    string `json:"localPath,omitempty" yaml:"localPath,omitempty"`
	Path         string `json:"path,omitempty" yaml:"path,omitempty"`
	AccessMode   string `json:"accessMode,omitempty" yaml:"accessMode,omitempty"`
	Status       string `json:"status,omitempty" yaml:"status,omitempty"`
}
