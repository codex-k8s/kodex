package generated

type AnonymousSchema_88 struct {
	DefinitionRef        string              `json:"definitionRef" binding:"required"`
	DefinitionVersion    int                 `json:"definitionVersion" binding:"required"`
	Capabilities         []string            `json:"capabilities" binding:"required"`
	CredentialBindingIds []string            `json:"credentialBindingIds" binding:"required"`
	EndpointRef          string              `json:"endpointRef" binding:"required"`
	Ownership            *AnonymousSchema_28 `json:"ownership" binding:"required"`
}
