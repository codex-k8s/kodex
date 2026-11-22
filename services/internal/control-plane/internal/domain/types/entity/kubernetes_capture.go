package entity

import "github.com/codex-k8s/kodex/libs/go/managedkubernetes"

// ManagedKubernetesCapturedArtifact identifies one immutable channel revision without object locators.
// ManagedKubernetesCapturedArtifact определяет одну immutable channel revision без object locators.
type ManagedKubernetesCapturedArtifact = managedkubernetes.CapturedArtifactReceipt

// ManagedKubernetesCaptureReceipt returns task metadata and immutable adopted channel receipts.
// COMPLETE/PARTIAL does not assert native stop, join or successful invocation outcome.
// ManagedKubernetesCaptureReceipt возвращает task metadata и immutable adopted channel receipts.
// COMPLETE/PARTIAL не утверждает native stop, join либо successful invocation outcome.
type ManagedKubernetesCaptureReceipt = managedkubernetes.CaptureTaskReceipt
