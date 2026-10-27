package platform

import roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"

// roleImageBuildLeaseDigest hashes a value copy of the lease with only Principal.CorrelationRef
// cleared; every other authority, lease, OCC and payload field remains in the commitment and the
// checked principal is unchanged.
// roleImageBuildLeaseDigest хеширует копию lease с очищенным только Principal.CorrelationRef; все
// прочие authority, lease, OCC и payload fields остаются в commitment, проверяемый principal не
// изменяется.
func roleImageBuildLeaseDigest(input roleimagerepo.BuildLeaseInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

// roleImageBuildProgressDigest hashes a value copy of the progress payload with only
// Principal.CorrelationRef cleared; every other authority, lease, OCC and payload field remains in
// the commitment and the checked principal is unchanged.
// roleImageBuildProgressDigest хеширует копию progress payload с очищенным только
// Principal.CorrelationRef; все прочие authority, lease, OCC и payload fields остаются в
// commitment, проверяемый principal не изменяется.
func roleImageBuildProgressDigest(input roleimagerepo.BuildProgressInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

// roleImageBuildCompletionDigest hashes a value copy of the completion payload with only
// Principal.CorrelationRef cleared; every other authority, lease, OCC and payload field remains in
// the commitment and the checked principal is unchanged.
// roleImageBuildCompletionDigest хеширует копию completion payload с очищенным только
// Principal.CorrelationRef; все прочие authority, lease, OCC и payload fields остаются в
// commitment, проверяемый principal не изменяется.
func roleImageBuildCompletionDigest(input roleimagerepo.BuildCompletionInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}

// roleImageBuildFailureDigest hashes a value copy of the failure payload with only
// Principal.CorrelationRef cleared; every other authority, lease, OCC and payload field remains in
// the commitment and the checked principal is unchanged.
// roleImageBuildFailureDigest хеширует копию failure payload с очищенным только
// Principal.CorrelationRef; все прочие authority, lease, OCC и payload fields остаются в
// commitment, проверяемый principal не изменяется.
func roleImageBuildFailureDigest(input roleimagerepo.BuildFailureInput) string {
	input.Principal.CorrelationRef = ""
	return roleImageDigest(input)
}
