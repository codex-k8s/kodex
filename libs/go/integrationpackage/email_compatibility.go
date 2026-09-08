package integrationpackage

import "encoding/json"

const legacyEmailDigest = "df52f45643b6e4464cf20901b6c069b88dac671303dc31e04f23b3d1ad4006fd"

// RequiresConnectionCredential отделяет provider credential от managed mailbox.
// У EMAIL_HTTPS полномочия несёт owner claim; SMTP/IMAP/POP3 credentials
// принадлежат email owner. Старый descriptor 1.4.0 не является bearer authority.
func (p Package) RequiresConnectionCredential() bool {
	return p.Spec.Credential != nil && !(p.Metadata.Key == "email" && p.Spec.Adapter == "EMAIL_HTTPS" && p.Spec.AdapterOwner == string(OwnerIntegrationGateway) && p.Spec.ExecutionRoute == string(RouteManagedMCP))
}

// ResolveShippedRevision сохраняет exact immutable pins предыдущего email package.
// При изменении других полей digest guard требует отдельного compatibility решения.
func ResolveShippedRevision(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Version == version && current.Digest == digest {
		return current, true
	}
	legacy, ok := legacyManagedMailbox(current)
	return legacy, ok && legacy.Metadata.Version == version && legacy.Digest == digest
}

func legacyManagedMailbox(current Package) (Package, bool) {
	if current.Metadata.Key != "email" || current.Metadata.Version != "1.4.1" || current.Metadata.Origin != Origin || current.Spec.Credential != nil {
		return Package{}, false
	}
	legacy := current
	legacy.Metadata.Version = "1.4.0"
	legacy.Spec.Credential = &Credential{SecretKey: "token", Kind: "TOKEN"}
	raw, err := json.Marshal(legacy)
	if err != nil {
		return Package{}, false
	}
	legacy, err = Parse(raw)
	return legacy, err == nil && legacy.Digest == legacyEmailDigest
}
