package integrationpackage

// RequiresConnectionCredential distinguishes a provider credential from the owner-managed mailbox:
// the managed EMAIL_HTTPS route obtains authority from an owner claim, not a connection secret.
// RequiresConnectionCredential отделяет provider credential от managed mailbox.
// У EMAIL_HTTPS полномочия несёт owner claim; SMTP/IMAP/POP3 credentials
// принадлежат email owner.
func (p Package) RequiresConnectionCredential() bool {
	return p.Spec.Credential != nil && !(p.Metadata.Key == "email" && p.Spec.Adapter == "EMAIL_HTTPS" && p.Spec.AdapterOwner == string(OwnerIntegrationGateway) && p.Spec.ExecutionRoute == string(RouteManagedMCP))
}

// ResolveShippedRevision accepts only the current exact shipped version and digest; it does not
// select an older release or treat caller-provided metadata as a source of authority.
// ResolveShippedRevision разрешает только текущую точную поставленную revision.
func ResolveShippedRevision(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Version == version && current.Digest == digest {
		return current, true
	}
	return Package{}, false
}
