// Package value contains governance-manager value objects.
package value

// ExternalRef points to another owner domain without copying its state.
type ExternalRef struct {
	Type string
	Ref  string
}

// EvidenceRef points to bounded evidence without embedding provider payloads, secrets or full logs.
type EvidenceRef struct {
	Kind    string
	Ref     string
	Summary string
	Digest  string
}
