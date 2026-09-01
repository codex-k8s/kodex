package platform

import (
	"strings"
	"testing"
)

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
