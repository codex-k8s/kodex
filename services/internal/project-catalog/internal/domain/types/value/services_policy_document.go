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
	Services             []ServicesPolicyService              `json:"services,omitempty" yaml:"services,omitempty"`
	DeployableServices   []ServicesPolicyService              `json:"deployableServices,omitempty" yaml:"deployableServices,omitempty"`
	Versions             map[string]ServicesPolicyVersionSpec `json:"versions,omitempty" yaml:"versions,omitempty"`
	Images               map[string]ServicesPolicyImageSpec   `json:"images,omitempty" yaml:"images,omitempty"`
	DeployManifests      []ServicesPolicyDeployManifest       `json:"deployManifests,omitempty" yaml:"deployManifests,omitempty"`
	DocumentationSources []ServicesPolicyDocumentationSource  `json:"documentationSources,omitempty" yaml:"documentationSources,omitempty"`
}

// ServicesPolicyVersionSpec describes one version entry from services.yaml.
type ServicesPolicyVersionSpec struct {
	Value  string   `json:"value,omitempty" yaml:"value,omitempty"`
	BumpOn []string `json:"bumpOn,omitempty" yaml:"bumpOn,omitempty"`
}

// ServicesPolicyImageSpec describes one image build entry from services.yaml.
type ServicesPolicyImageSpec struct {
	ServicesPolicyImageSourceSpec
	ServicesPolicyImageBuildContextSpec
}

// ServicesPolicyImageSourceSpec is the stack image source projection.
type ServicesPolicyImageSourceSpec struct {
	Type             string `json:"type,omitempty" yaml:"type,omitempty"`
	From             string `json:"from,omitempty" yaml:"from,omitempty"`
	Local            string `json:"local,omitempty" yaml:"local,omitempty"`
	Repository       string `json:"repository,omitempty" yaml:"repository,omitempty"`
	TagTemplate      string `json:"tagTemplate,omitempty" yaml:"tagTemplate,omitempty"`
	Dockerfile       string `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Target           string `json:"target,omitempty" yaml:"target,omitempty"`
	Context          string `json:"context,omitempty" yaml:"context,omitempty"`
	ImageEnv         string `json:"imageEnv,omitempty" yaml:"imageEnv,omitempty"`
	MigratesDatabase string `json:"migratesDatabase,omitempty" yaml:"migratesDatabase,omitempty"`
}

// ServicesPolicyImageBuildContextSpec хранит совместимые поля старых checked payload.
// Self-deploy build получает динамические context refs из GetSelfDeployBuildPlan input.
type ServicesPolicyImageBuildContextSpec struct {
	BuildContextRef    string                           `json:"buildContextRef,omitempty" yaml:"buildContextRef,omitempty"`
	BuildContextDigest string                           `json:"buildContextDigest,omitempty" yaml:"buildContextDigest,omitempty"`
	DockerfileDigest   string                           `json:"dockerfileDigest,omitempty" yaml:"dockerfileDigest,omitempty"`
	BuilderImageRef    string                           `json:"builderImageRef,omitempty" yaml:"builderImageRef,omitempty"`
	AllowedSecretRefs  []ServicesPolicyAllowedSecretRef `json:"allowedSecretRefs,omitempty" yaml:"allowedSecretRefs,omitempty"`
	OutputRefs         []ServicesPolicyOutputRef        `json:"outputRefs,omitempty" yaml:"outputRefs,omitempty"`
}

// ServicesPolicyService describes one service entry from normalized services.yaml.
type ServicesPolicyService struct {
	Key                  string                    `json:"key,omitempty" yaml:"key,omitempty"`
	Name                 string                    `json:"name,omitempty" yaml:"name,omitempty"`
	DisplayName          string                    `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Kind                 string                    `json:"kind,omitempty" yaml:"kind,omitempty"`
	Type                 string                    `json:"type,omitempty" yaml:"type,omitempty"`
	RootPath             string                    `json:"rootPath,omitempty" yaml:"rootPath,omitempty"`
	Path                 string                    `json:"path,omitempty" yaml:"path,omitempty"`
	RepositoryID         string                    `json:"repositoryId,omitempty" yaml:"repositoryId,omitempty"`
	DocumentationScopeID string                    `json:"documentationScopeId,omitempty" yaml:"documentationScopeId,omitempty"`
	DependsOn            []string                  `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	DependsOnServiceKeys []string                  `json:"dependsOnServiceKeys,omitempty" yaml:"dependsOnServiceKeys,omitempty"`
	Status               string                    `json:"status,omitempty" yaml:"status,omitempty"`
	Build                *ServicesPolicyBuildSpec  `json:"build,omitempty" yaml:"build,omitempty"`
	Deploy               *ServicesPolicyDeploySpec `json:"deploy,omitempty" yaml:"deploy,omitempty"`
}

// ServicesPolicyBuildSpec описывает проверенный рецепт сборки одного сервиса.
// Динамические build context refs передаются отдельно контуром runtime-manager.
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

// ServicesPolicyDeploySpec описывает проверенный рецепт rollout одного сервиса.
type ServicesPolicyDeploySpec struct {
	ServiceManifest      string                              `json:"serviceManifest,omitempty" yaml:"serviceManifest,omitempty"`
	MigrationsManifest   string                              `json:"migrationsManifest,omitempty" yaml:"migrationsManifest,omitempty"`
	Kustomization        string                              `json:"kustomization,omitempty" yaml:"kustomization,omitempty"`
	ManifestBundleRef    string                              `json:"manifestBundleRef,omitempty" yaml:"manifestBundleRef,omitempty"`
	ManifestBundleDigest string                              `json:"manifestBundleDigest,omitempty" yaml:"manifestBundleDigest,omitempty"`
	TargetNamespace      string                              `json:"targetNamespace,omitempty" yaml:"targetNamespace,omitempty"`
	TargetClusterRef     string                              `json:"targetClusterRef,omitempty" yaml:"targetClusterRef,omitempty"`
	TargetSlotID         string                              `json:"targetSlotId,omitempty" yaml:"targetSlotId,omitempty"`
	RolloutTargets       []ServicesPolicyDeployRolloutTarget `json:"rolloutTargets,omitempty" yaml:"rolloutTargets,omitempty"`
	AllowedSecretRefs    []ServicesPolicyAllowedSecretRef    `json:"allowedSecretRefs,omitempty" yaml:"allowedSecretRefs,omitempty"`
	OutputRefs           []ServicesPolicyOutputRef           `json:"outputRefs,omitempty" yaml:"outputRefs,omitempty"`
}

// ServicesPolicyDeployRolloutTarget describes one bounded Kubernetes rollout target.
type ServicesPolicyDeployRolloutTarget struct {
	Kind      string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Ref       string `json:"ref,omitempty" yaml:"ref,omitempty"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Digest    string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

// ServicesPolicyDeployManifest describes one checked manifest bundle entry.
type ServicesPolicyDeployManifest struct {
	Name       string   `json:"name,omitempty" yaml:"name,omitempty"`
	Path       string   `json:"path,omitempty" yaml:"path,omitempty"`
	RenderMode string   `json:"renderMode,omitempty" yaml:"renderMode,omitempty"`
	Purpose    string   `json:"purpose,omitempty" yaml:"purpose,omitempty"`
	Includes   []string `json:"includes,omitempty" yaml:"includes,omitempty"`
	Requires   []string `json:"requires,omitempty" yaml:"requires,omitempty"`
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
