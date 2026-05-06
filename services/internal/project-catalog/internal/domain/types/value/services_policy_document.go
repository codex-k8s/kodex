package value

// ServicesPolicyDocument is the normalized JSON representation of services.yaml.
type ServicesPolicyDocument struct {
	APIVersion string                  `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
	Kind       string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Metadata   ServicesPolicyMetadata  `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Spec       ServicesPolicySpec      `json:"spec,omitempty" yaml:"spec,omitempty"`
	Services   []ServicesPolicyService `json:"services,omitempty" yaml:"services,omitempty"`
}

// ServicesPolicyMetadata stores safe document metadata.
type ServicesPolicyMetadata struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
}

// ServicesPolicySpec stores the policy body used by project-catalog.
type ServicesPolicySpec struct {
	Services           []ServicesPolicyService `json:"services,omitempty" yaml:"services,omitempty"`
	DeployableServices []ServicesPolicyService `json:"deployableServices,omitempty" yaml:"deployableServices,omitempty"`
}

// ServicesPolicyService describes one service entry from normalized services.yaml.
type ServicesPolicyService struct {
	Key                  string   `json:"key,omitempty" yaml:"key,omitempty"`
	Name                 string   `json:"name,omitempty" yaml:"name,omitempty"`
	DisplayName          string   `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Kind                 string   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Type                 string   `json:"type,omitempty" yaml:"type,omitempty"`
	RootPath             string   `json:"rootPath,omitempty" yaml:"rootPath,omitempty"`
	Path                 string   `json:"path,omitempty" yaml:"path,omitempty"`
	RepositoryID         string   `json:"repositoryId,omitempty" yaml:"repositoryId,omitempty"`
	DocumentationScopeID string   `json:"documentationScopeId,omitempty" yaml:"documentationScopeId,omitempty"`
	DependsOn            []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	DependsOnServiceKeys []string `json:"dependsOnServiceKeys,omitempty" yaml:"dependsOnServiceKeys,omitempty"`
	Status               string   `json:"status,omitempty" yaml:"status,omitempty"`
}
