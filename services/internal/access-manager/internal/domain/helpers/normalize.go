// Package helpers contains normalization helpers shared by access use cases.
package helpers

import "strings"

// NormalizeEmail returns the canonical email form used by allowlist and users.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// EmailDomain extracts a normalized domain from an email address.
func EmailDomain(email string) string {
	parts := strings.Split(NormalizeEmail(email), "@")
	if len(parts) != 2 {
		return ""
	}
	return NormalizeDomain(parts[1])
}

// NormalizeDomain returns the canonical domain form used by allowlist matching.
func NormalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

// NormalizeSlug returns the canonical slug form used by scoped natural keys.
func NormalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}
