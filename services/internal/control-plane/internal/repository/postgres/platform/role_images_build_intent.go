package platform

import roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"

// CorrelationRef назначается заново проверяющей transport boundary каждого RPC.
// Он не меняет semantic intent; остальные authority, lease и OCC поля сохраняются.
// Копия для digest никогда не подменяет Principal при проверке полномочий.
func roleImageBuildLeaseDigest(input roleimagerepo.BuildLeaseInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

func roleImageBuildProgressDigest(input roleimagerepo.BuildProgressInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

func roleImageBuildCompletionDigest(input roleimagerepo.BuildCompletionInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

func roleImageBuildFailureDigest(input roleimagerepo.BuildFailureInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}
