package entity

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

type RuntimeEnvironmentDraftSpecification struct {
	Name, Description, ImageArtifactRef string
	Values                              []RuntimeEnvironmentValue
	SecretBindings                      []RuntimeSecretBinding
	Tools                               []RuntimeEnvironmentTool
	Policy                              runtimecontract.RuntimeEnvironmentPolicy
}

type RuntimeEnvironmentDraft struct {
	Ref, ProjectRef, EnvironmentRef, State, ValidationDigest, PublishedEnvironmentRef string
	Version, ExpectedEnvironmentVersion                                               int64
	Specification                                                                     RuntimeEnvironmentDraftSpecification
	Diagnostics                                                                       []string
}
