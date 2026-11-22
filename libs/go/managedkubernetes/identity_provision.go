package managedkubernetes

import "k8s.io/apimachinery/pkg/util/validation"

// IdentityProvision contains owner-resolved pins before a purpose Secret receives a UID/RV.
// It cannot be used as an execution locator; storage may only create its derived immutable name.
// IdentityProvision содержит owner-resolved pins до назначения purpose Secret его UID/RV.
// Это не execution locator; storage создаёт только выведенное неизменяемое имя.
type IdentityProvision struct {
	IdentityRef, TargetRef, Namespace, ContentSHA256 string `json:"-"`
	Revision                                         int64  `json:"-"`
}

// Validate refuses missing private namespace, identity, target, revision and content digest.
// It validates shape only; a fresh one-shot owner admission must precede any storage effect.
// Validate отклоняет неполные приватные namespace, identity, target, revision и content digest.
// Проверяет только форму; storage effect требует свежего одноразового owner admission.
func (provision IdentityProvision) Validate(namespace string) error {
	if provision.Namespace != namespace || len(validation.IsDNS1123Label(namespace)) != 0 ||
		!identityRef.MatchString(provision.IdentityRef) || !identityRef.MatchString(provision.TargetRef) ||
		provision.Revision < 1 || !ValidDigest(provision.ContentSHA256) {
		return ErrIdentityLocator
	}
	return nil
}

// String conceals private materialization coordinates and content hashes from diagnostics.
// String скрывает приватные materialization coordinates и content hashes из диагностики.
func (IdentityProvision) String() string { return identityLocatorRedacted }

// GoString preserves the same private boundary for detailed Go diagnostic formatting.
// GoString сохраняет ту же приватную границу при подробном Go diagnostic formatting.
func (IdentityProvision) GoString() string { return identityLocatorRedacted }
