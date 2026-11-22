package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/util/validation"
)

// identityLocatorRedacted is the sole diagnostic representation of private storage pins.
// identityLocatorRedacted — единственное diagnostic representation private storage pins.
const identityLocatorRedacted = "[managed Kubernetes private identity locator]"

// ErrIdentityLocator rejects incomplete private material pins without returning them.
// ErrIdentityLocator отклоняет неполные private material pins без их возврата.
var ErrIdentityLocator = errors.New("managed Kubernetes identity locator is invalid")

// identityRef requires the same opaque metadata-reference syntax as the owning domain.
// identityRef требует тот же opaque metadata-reference syntax, что и owning domain.
var identityRef = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// ValidIdentityReference checks the shared opaque identity/target syntax, never its ownership.
// ValidIdentityReference проверяет общий opaque identity/target syntax, не ownership.
func ValidIdentityReference(ref string) bool { return identityRef.MatchString(ref) }

// IdentityLocator identifies a separate broker-only immutable purpose Secret.
// It is never a generic runtime-secret reference or a credential delivered to the gateway.
// Current actor, target, purpose and generation are resolved by the owner before constructing it.
// IdentityLocator указывает отдельный broker-only immutable purpose Secret.
// Это не generic runtime-secret reference либо credential для gateway.
// Owner разрешает current actor, target, purpose и generation до его создания.
type IdentityLocator struct {
	IdentityRef, TargetRef, Namespace, Name string `json:"-"`
	UID, ResourceVersion, ContentSHA256     string `json:"-"`
	Revision                                int64  `json:"-"`
}

// IdentitySecretName derives a deterministic private storage name, never authority.
// Scope and exact UID/RV/content pins remain mandatory even when the name matches.
// IdentitySecretName выводит deterministic private storage name, не authority.
// Scope и exact UID/RV/content pins обязательны даже при совпадении имени.
func IdentitySecretName(ref string, revision int64) (string, error) {
	if !identityRef.MatchString(ref) || revision < 1 {
		return "", ErrIdentityLocator
	}
	digest := sha256.Sum256([]byte(ref))
	return "kbi-" + hex.EncodeToString(digest[:16]) + "-r" + strconv.FormatInt(revision, 10), nil
}

// Validate requires one namespace, real object identity and canonical immutable content pins.
// It does not prove current owner eligibility or turn storage metadata into a credential grant.
// Validate требует один namespace, настоящую object identity и canonical immutable content pins.
// Не доказывает current owner eligibility и не превращает storage metadata в credential grant.
func (locator IdentityLocator) Validate(namespace string) error {
	name, err := IdentitySecretName(locator.IdentityRef, locator.Revision)
	id, idErr := uuid.Parse(locator.UID)
	if err != nil || locator.Name != name || locator.Namespace != namespace || len(validation.IsDNS1123Label(namespace)) != 0 ||
		!identityRef.MatchString(locator.TargetRef) || idErr != nil || id == uuid.Nil || id.String() != locator.UID ||
		!ValidDigest(locator.ContentSHA256) || !ValidLocator(locator.ResourceVersion) || len(locator.ResourceVersion) > 128 {
		return ErrIdentityLocator
	}
	return nil
}

// String hides private storage locators and digests from accidental diagnostic formatting.
// String скрывает private storage locators и digests при случайном диагностическом форматировании.
func (IdentityLocator) String() string {
	return identityLocatorRedacted
}

// GoString applies the same private-metadata boundary to detailed Go formatting.
// GoString сохраняет ту же private-metadata boundary при подробном Go formatting.
func (IdentityLocator) GoString() string {
	return identityLocatorRedacted
}
