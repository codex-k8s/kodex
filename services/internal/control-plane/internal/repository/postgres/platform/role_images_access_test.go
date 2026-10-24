package platform

import (
	"strings"
	"testing"
)

// TestRoleImageQueriesDoNotContainLegacyMembershipAuthorization inspects
// list/detail/role-resolution/lock SQL for forbidden legacy membership and role fragments; this
// parallel static check does not replace authoritative access-context tests.
// TestRoleImageQueriesDoNotContainLegacyMembershipAuthorization проверяет SQL
// list/detail/role-resolution/lock на запрещённые прежние membership и role fragments;
// параллельная статическая проверка не заменяет тесты авторитетного access context.
func TestRoleImageQueriesDoNotContainLegacyMembershipAuthorization(t *testing.T) {
	t.Parallel()
	queries := map[string]string{
		"list":         queryRoleImagesListRecipes,
		"get":          queryRoleImagesGetRecipe,
		"resolve role": queryRoleImagesResolveProjectRole,
		"lock":         queryRoleImagesLockRecipe,
	}
	for name, query := range queries {
		t.Run(name, func(t *testing.T) {
			lower := strings.ToLower(query)
			for _, forbidden := range []string{"memberships", "manage_agents", "current.role"} {
				if strings.Contains(lower, forbidden) {
					t.Fatalf("role image query contains legacy authorization %q: %s", forbidden, query)
				}
			}
		})
	}
}
