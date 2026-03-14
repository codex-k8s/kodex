package app

import "strings"

const (
	workerModeService              = "service"
	workerModeNamespaceCleanupOnce = "namespace_cleanup_once"
)

func resolveWorkerMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case workerModeNamespaceCleanupOnce:
		return workerModeNamespaceCleanupOnce
	default:
		return workerModeService
	}
}
