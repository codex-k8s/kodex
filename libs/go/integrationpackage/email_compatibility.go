package integrationpackage

// RequiresConnectionCredential distinguishes a provider credential from the owner-managed mailbox:
// the managed EMAIL_HTTPS route obtains authority from an owner claim, not a connection secret.
// RequiresConnectionCredential отделяет provider credential от managed mailbox.
// У EMAIL_HTTPS полномочия несёт owner claim; SMTP/IMAP/POP3 credentials
// принадлежат email owner.
func (p Package) RequiresConnectionCredential() bool {
	return p.Spec.Credential != nil && !(p.Metadata.Key == "email" && p.Spec.Adapter == "EMAIL_HTTPS" && p.Spec.AdapterOwner == string(OwnerIntegrationGateway) && p.Spec.ExecutionRoute == string(RouteManagedMCP))
}

// ResolveShippedRevision accepts current exact pins or explicitly frozen GitHub/Jira releases,
// preserving historical policy rather than inheriting the current capability or input authority.
// ResolveShippedRevision допускает текущие exact pins либо закреплённые выпуски GitHub/Jira,
// сохраняя историческую политику без наследования новых capabilities или полномочий ввода.
func ResolveShippedRevision(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Version == version && current.Digest == digest {
		return current, true
	}
	return resolveHistoricalPolicyRevision(current, version, digest)
}
