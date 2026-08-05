// Package value содержит общие для runtime-controller доменные value objects.
package value

import "strings"

const forbiddenRepositoryCharacters = "@?# \\\r\n\t"

// ValidImageRepository проверяет repository без tag/digest/query/fragment.
func ValidImageRepository(repository string) bool {
	return repository != "" && strings.Contains(repository, "/") &&
		!strings.HasSuffix(repository, "/") &&
		!strings.ContainsAny(repository, forbiddenRepositoryCharacters) &&
		!strings.Contains(repository, "://")
}
