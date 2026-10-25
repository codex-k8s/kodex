package integrationpackage

// RequiresConnectionCredential distinguishes a provider credential from the owner-managed mailbox:
// the managed EMAIL_HTTPS route obtains authority from an owner claim, not a connection secret.
// RequiresConnectionCredential отделяет provider credential от managed mailbox.
// У EMAIL_HTTPS полномочия несёт owner claim; SMTP/IMAP/POP3 credentials
// принадлежат email owner.
func (p Package) RequiresConnectionCredential() bool {
	return p.Spec.Credential != nil && !(p.Metadata.Key == "email" && p.Spec.Adapter == "EMAIL_HTTPS" && p.Spec.AdapterOwner == string(OwnerIntegrationGateway) && p.Spec.ExecutionRoute == string(RouteManagedMCP))
}

// ResolveShippedRevision accepts current exact pins or the one explicit GitHub 6.0 rolling-read compatibility snapshot. It never substitutes a requested digest, gives an old connection the new bundle capability, or enables arbitrary historical/email revisions.
// ResolveShippedRevision допускает current exact pins либо один явный GitHub6.0 compatibility snapshot для rolling-read. Запрошенный digest не заменяется, старая connection не получает bundle capability, произвольные historical/email revisions не включаются.
func ResolveShippedRevision(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Version == version && current.Digest == digest {
		return current, true
	}
	return resolveGitHubSixCompatibility(current, version, digest)
}
