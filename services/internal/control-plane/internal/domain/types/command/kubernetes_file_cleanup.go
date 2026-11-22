package command

// KubernetesFileCleanupRepair carries locators only; root, project, writer fencing and lifecycle are owner-derived.
// KubernetesFileCleanupRepair содержит лишь locators; root, project, writer fencing и lifecycle определяет owner.
type KubernetesFileCleanupRepair struct {
	InvocationRef, SourceRef string
}
