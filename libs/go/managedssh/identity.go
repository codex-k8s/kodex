package managedssh

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// IdentityPurpose fixes the separate broker-only material lane; neither generic TOKEN descriptors nor Kubernetes identities may be substituted for it.
// IdentityPurpose фиксирует отдельный broker-only material lane; generic TOKEN descriptors либо Kubernetes identities не могут его подменить.
const IdentityPurpose = "SSH_TARGET"

// identityNamespace rejects alternate Kubernetes path spellings before a locator reaches the exact namespace reader.
// identityNamespace отклоняет alternate Kubernetes path spellings до передачи locator exact namespace reader.
var identityNamespace = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// IdentityLocator is a private owner-to-broker material locator, never a gateway or public credential view; every immutable pin is mandatory.
// IdentityLocator является private owner-to-broker material locator, а не gateway либо public credential view; каждый immutable pin обязателен.
type IdentityLocator struct {
	Purpose               string
	IdentityRevisionRef   string
	TargetRef             string
	Revision              int64
	Namespace             string
	SecretName            string
	SecretUID             string
	SecretResourceVersion string
	ContentSHA256         string
	AuthenticationSHA256  string
}

// IdentityProvision is the immutable owner-prepared material tuple; mutable claim fences and lease generations never change this identity or enter Kubernetes metadata.
// IdentityProvision содержит immutable owner-prepared material tuple; mutable claim fences и lease generations не меняют identity и не попадают в Kubernetes metadata.
type IdentityProvision struct {
	Purpose              string
	IdentityRevisionRef  string
	TargetRef            string
	Revision             int64
	Namespace            string
	ContentSHA256        string
	AuthenticationSHA256 string
}

// IdentityKeyMetadata contains only broker-derived private integrity pins for owner intent preparation; it is never a public credential view or a caller declaration of provider permissions.
// IdentityKeyMetadata содержит только broker-derived private integrity pins для подготовки owner intent; это не public credential view либо caller declaration provider permissions.
type IdentityKeyMetadata struct {
	ContentSHA256        string
	AuthenticationSHA256 string
}

// Valid validates an immutable SSH-purpose intent before a create or initial observation; only the setup owner can authorize those distinct operations.
// Valid проверяет immutable SSH-purpose intent до create либо initial observation; только setup owner разрешает эти разные operations.
func (provision IdentityProvision) Valid() bool {
	return provision.Purpose == IdentityPurpose && OwnerReference(provision.IdentityRevisionRef) && OwnerReference(provision.TargetRef) && provision.Revision > 0 &&
		IdentityNamespace(provision.Namespace) && Digest(provision.ContentSHA256) && Digest(provision.AuthenticationSHA256)
}

// Provision recovers only the existing immutable intent from a pinned locator, never a new claim or authority to adopt a replacement object.
// Provision возвращает только существующий immutable intent из pinned locator, а не новый claim либо authority на принятие replacement object.
func (locator IdentityLocator) Provision() IdentityProvision {
	return IdentityProvision{Purpose: locator.Purpose, IdentityRevisionRef: locator.IdentityRevisionRef, TargetRef: locator.TargetRef, Revision: locator.Revision, Namespace: locator.Namespace, ContentSHA256: locator.ContentSHA256, AuthenticationSHA256: locator.AuthenticationSHA256}
}

// Valid requires the fixed purpose and deterministic per-revision object name before a private read, without interpreting a caller-provided locator as authority.
// Valid требует fixed purpose и deterministic per-revision object name до private read, не рассматривая caller-provided locator как authority.
func (locator IdentityLocator) Valid() bool {
	return locator.Provision().Valid() && locator.SecretName == IdentitySecretName(locator.IdentityRevisionRef, locator.Revision) &&
		validIdentityObjectPin(locator.SecretUID) && validIdentityObjectPin(locator.SecretResourceVersion)
}

// IdentitySecretName assigns a bounded material name from the immutable metadata revision; the name alone cannot authorize reading an object or accepting a replacement UID.
// IdentitySecretName назначает bounded material name из immutable metadata revision; само имя не разрешает чтение объекта либо принятие replacement UID.
func IdentitySecretName(revisionRef string, revision int64) string {
	if !OwnerReference(revisionRef) || revision < 1 {
		return ""
	}
	digest := sha256.Sum256([]byte(revisionRef))
	return "sshbi-" + hex.EncodeToString(digest[:16]) + "-r" + strconv.FormatInt(revision, 10)
}

// validIdentityObjectPin keeps Kubernetes UID and resourceVersion opaque and byte-exact; they are never interpreted as UUIDs, numbers, paths or latest-version selectors.
// validIdentityObjectPin сохраняет Kubernetes UID и resourceVersion opaque и byte-exact; они не интерпретируются как UUID, числа, пути либо latest-version selectors.
func validIdentityObjectPin(value string) bool {
	return len(value) > 0 && len(value) <= 256 && utf8.ValidString(value)
}

// IdentityNamespace admits one canonical Kubernetes namespace segment for the private purpose store; syntax alone gives no resource or credential access.
// IdentityNamespace допускает один canonical Kubernetes namespace segment private purpose store; синтаксис не выдаёт resource либо credential access.
func IdentityNamespace(namespace string) bool { return identityNamespace.MatchString(namespace) }

// IdentityPreparation retains owner-assigned material coordinates before the broker parses a private key, without manufacturing an authentication digest or a usable locator.
// IdentityPreparation сохраняет owner-assigned material coordinates до разбора private key broker без изготовления authentication digest либо usable locator.
type IdentityPreparation struct {
	IdentityRevisionRef string
	TargetRef           string
	Revision            int64
	Namespace           string
	ContentSHA256       string
}

// Valid checks only immutable pending coordinates; the owner still decides whether this setup exists and may be claimed by the current principal.
// Valid проверяет только immutable pending coordinates; существование setup и допуск claim текущим principal определяет owner.
func (preparation IdentityPreparation) Valid() bool {
	return OwnerReference(preparation.IdentityRevisionRef) && OwnerReference(preparation.TargetRef) && preparation.Revision > 0 && IdentityNamespace(preparation.Namespace) && Digest(preparation.ContentSHA256)
}

// Provision adds the actual parsed key metadata only when its content agrees with the original intent; the resulting syntax is not storage or binding authority.
// Provision добавляет actual parsed key metadata лишь при совпадении content с original intent; полученная syntax не является storage либо binding authority.
func (preparation IdentityPreparation) Provision(key IdentityKeyMetadata) (IdentityProvision, bool) {
	provision := IdentityProvision{Purpose: IdentityPurpose, IdentityRevisionRef: preparation.IdentityRevisionRef, TargetRef: preparation.TargetRef, Revision: preparation.Revision, Namespace: preparation.Namespace, ContentSHA256: key.ContentSHA256, AuthenticationSHA256: key.AuthenticationSHA256}
	return provision, preparation.ContentSHA256 == key.ContentSHA256 && provision.Valid()
}
