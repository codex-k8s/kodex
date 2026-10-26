package sshcompose

// SecretBinding pins one project-scoped platform secret revision without its value.
// SecretBinding закрепляет project-scoped platform secret revision без значения.
type SecretBinding struct {
	Ref             string `json:"ref"`
	Revision        int64  `json:"revision"`
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Key             string `json:"key"`
	UID             string `json:"uid"`
	ResourceVersion string `json:"resourceVersion"`
	ContentSHA256   string `json:"contentSha256"`
}

// GrantBinding identifies one server-selected source grant and its immutable version.
// GrantBinding обозначает один выбранный сервером source grant и immutable version.
type GrantBinding struct {
	Ref     string `json:"ref"`
	Version int64  `json:"version"`
	Key     string `json:"key"`
}

// SourceBinding pins the protected GitHub connection and read grants used for one release.
// SourceBinding закрепляет защищённую GitHub connection и read grants одного release.
type SourceBinding struct {
	ConnectionRef         string         `json:"connectionRef"`
	DefinitionVersion     string         `json:"definitionVersion"`
	DefinitionDigest      string         `json:"definitionDigest"`
	CredentialRef         string         `json:"credentialRef"`
	CredentialRevision    int64          `json:"credentialRevision"`
	SecretRef             string         `json:"secretRef"`
	SecretUID             string         `json:"secretUid"`
	SecretResourceVersion string         `json:"secretResourceVersion"`
	CredentialSHA256      string         `json:"credentialSha256"`
	Grants                []GrantBinding `json:"grants"`
}

// Snapshot is owner-assigned immutable worker input; it is never a model credential projection.
// Snapshot — назначенный owner immutable input worker, не credential projection модели.
type Snapshot struct {
	Version                 int            `json:"version"`
	OriginRuntimeRevisionID string         `json:"originRuntimeRevisionId,omitempty"`
	OriginLeaseID           string         `json:"originLeaseId,omitempty"`
	OriginGeneration        int64          `json:"originGeneration,omitempty"`
	Target                  Target         `json:"target"`
	SSHSecret               SecretBinding  `json:"sshSecret"`
	Source                  *SourceBinding `json:"source,omitempty"`
	MergeInvocationRef      string         `json:"mergeInvocationRef,omitempty"`
	InspectInvocationRef    string         `json:"inspectInvocationRef,omitempty"`
	DeploymentRef           string         `json:"deploymentRef,omitempty"`
	DeploymentInputDigest   string         `json:"deploymentInputDigest,omitempty"`
	DeploymentEffectKey     string         `json:"deploymentEffectKey,omitempty"`
	MergedSHA               string         `json:"mergedSha,omitempty"`
	TreeSHA                 string         `json:"treeSha,omitempty"`
	BundleSHA256            string         `json:"bundleSha256,omitempty"`
	RootRunRef              string         `json:"rootRunRef"`
	SessionRef              string         `json:"sessionRef"`
	TurnRef                 string         `json:"turnRef"`
	Attempt                 int            `json:"attempt"`
}
