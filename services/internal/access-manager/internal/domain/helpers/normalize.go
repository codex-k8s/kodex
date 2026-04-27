package helpers

import "strings"

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func EmailDomain(email string) string {
	parts := strings.Split(NormalizeEmail(email), "@")
	if len(parts) != 2 {
		return ""
	}
	return parts[1]
}

func NormalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}
