package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidBinding rejects malformed owner locators without disclosing a fence or foreign ref.
// ErrInvalidBinding отклоняет неверные owner locators без раскрытия fence либо foreign ref.
var ErrInvalidBinding = errors.New("invalid managed Kubernetes owner binding")

// Binding identifies one owner-issued invocation or health-test claim and its immutable input.
// It never carries actor/tenant/target authority. All consumers must still check trusted transport
// and re-resolve the complete current owner graph before returning metadata or external credit.
// Binding задаёт один owner-issued invocation либо health-test claim и immutable input.
// Не переносит actor/tenant/target authority. Каждый consumer проверяет trusted transport
// и полный current owner graph до metadata либо external credit.
type Binding struct {
	WorkloadInstance, InvocationRef, ConnectionTestRef, LeaseRef string
	Fence                                                        string `json:"-"`
	InputDigest                                                  string
	Generation                                                   int64
}

// Validate accepts exactly one bounded claim locator with a canonical input digest.
// It does not normalize refs, fences or instance names and never treats their shape as authority.
// Validate принимает ровно один bounded claim locator с canonical input digest.
// Не нормализует refs, fences либо instance names и не считает их форму authority.
func (binding Binding) Validate() error {
	if (binding.InvocationRef == "") == (binding.ConnectionTestRef == "") || binding.Generation < 1 ||
		!ValidDigest(binding.InputDigest) || !ValidLocator(binding.WorkloadInstance) ||
		!ValidLocator(binding.LeaseRef) || !ValidLocator(binding.Fence) ||
		(binding.InvocationRef != "" && !ValidLocator(binding.InvocationRef)) ||
		(binding.ConnectionTestRef != "" && !ValidLocator(binding.ConnectionTestRef)) {
		return ErrInvalidBinding
	}
	return nil
}

// ValidDigest requires canonical lowercase SHA256 without coercion or whitespace stripping.
// ValidDigest требует canonical lowercase SHA256 без coercion либо удаления whitespace.
func ValidDigest(digest string) bool {
	decoded, err := hex.DecodeString(digest)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(digest) == digest
}

// ValidLocator permits bounded opaque owner-issued refs while rejecting controls and whitespace.
// The database remains responsible for exact ownership, incarnation, fence and lifecycle matching.
// ValidLocator допускает bounded opaque owner-issued refs без controls и whitespace.
// Database проверяет exact ownership, incarnation, fence и lifecycle matching.
func ValidLocator(ref string) bool {
	if ref == "" || len(ref) > 256 || !utf8.ValidString(ref) {
		return false
	}
	for _, character := range ref {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// String hides the fence even when an enclosing domain value is formatted accidentally.
// String скрывает fence даже при случайном форматировании содержащего domain value.
func (Binding) String() string { return "[managed Kubernetes owner binding]" }

// GoString prevents detailed Go formatting from bypassing the ordinary fence redaction.
// GoString не позволяет detailed Go formatting обойти обычное скрытие fence.
func (Binding) GoString() string { return "[managed Kubernetes owner binding]" }
