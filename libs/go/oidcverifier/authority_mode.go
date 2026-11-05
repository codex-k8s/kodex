package oidcverifier

// AuthorityMode selects an explicit application-authority contract independently of the identity provider's installation topology.
// Empty configuration retains legacy claims; unknown modes never silently downgrade to it.
// AuthorityMode явно выбирает контракт application authority независимо от способа установки identity provider.
// Пустая configuration сохраняет legacy claims; неизвестный mode не понижается до него молча.
type AuthorityMode string

const (
	// ClaimsAuthorityMode requires the existing signed organization/revision and explicit owner claims.
	// ClaimsAuthorityMode требует прежние подписанные organization/revision и explicit owner claims.
	ClaimsAuthorityMode AuthorityMode = "claims"
	// PlatformManagedAuthorityMode obtains application authority only from current platform-owned identity and session state.
	// PlatformManagedAuthorityMode получает application authority только из текущего platform-owned identity и session state.
	PlatformManagedAuthorityMode AuthorityMode = "platform-managed"
)

// ParseAuthorityMode accepts only the closed supported modes, retaining the established claims default for empty inputs.
// ParseAuthorityMode принимает лишь закрытый набор supported modes, сохраняя прежний claims default при пустом входе.
func ParseAuthorityMode(raw string) (AuthorityMode, bool) {
	if raw == "" || raw == string(ClaimsAuthorityMode) {
		return ClaimsAuthorityMode, true
	}
	if raw == string(PlatformManagedAuthorityMode) {
		return PlatformManagedAuthorityMode, true
	}
	return "", false
}
