package entity

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// KubernetesArtifactCaptureOrigin shares the closed safe capture projection with its consumers.
// It contains no source identity, object locator, credential or execution authority.
// KubernetesArtifactCaptureOrigin разделяет closed safe capture projection с consumers.
// Source identity, object locator, credential либо execution authority не содержит.
type KubernetesArtifactCaptureOrigin = managedkubernetes.ArtifactCaptureOrigin
