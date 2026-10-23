package websockettransport

import (
	"context"
	"errors"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// projectOrganizationRecipes initializes separate organization recipe/page fields, reads the explicit owner endpoint and validates page bounds, organization-only scope and one consistent organization before projection. NotFound/PermissionDenied leave only that organizational catalog empty; other errors propagate. The owner RPC, not this consistency check, establishes caller eligibility.
// projectOrganizationRecipes создаёт отдельные поля организационных рецептов и страницы, читает явный endpoint владельца и перед проекцией проверяет предел, исключительно организационную область и одну согласованную организацию. NotFound/PermissionDenied оставляют пустым только этот каталог; остальные ошибки возвращаются. Допуск вызывающего определяет owner RPC, а не эта проверка согласованности.
func (s *Server) projectOrganizationRecipes(ctx context.Context, catalog map[string]any, localize func(string) string) error {
	catalog["organizationRecipes"], catalog["organizationRecipesPage"] = []any{}, map[string]any{}
	response, err := s.roleImages.ListOrganizationRoleImageRecipes(ctx, &cp.ListOrganizationRoleImageRecipesRequest{Page: platformPage()})
	if err != nil {
		if status.Code(err) == codes.NotFound || status.Code(err) == codes.PermissionDenied {
			return nil
		}
		return err
	}
	if response == nil || len(response.GetRecipes()) > platformSnapshotPageSize {
		return errors.New("organization role image snapshot is invalid")
	}
	organizationRef := ""
	for _, recipe := range response.GetRecipes() {
		if recipe == nil || recipe.GetScopeKind() != cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION || recipe.GetProjectRef() != "" || !safeRef.MatchString(recipe.GetOrganizationRef()) || organizationRef != "" && recipe.GetOrganizationRef() != organizationRef {
			return errors.New("organization role image snapshot scope mismatch")
		}
		organizationRef = recipe.GetOrganizationRef()
	}
	projection, err := projectSnapshotPart(response, localize)
	if err != nil {
		return err
	}
	catalog["organizationRecipes"], catalog["organizationRecipesPage"] = projection["recipes"], projection["page"]
	return nil
}

// projectOrganizationSecrets projects only metadata from the organization-secret owner listing into separate secret/page fields after page, scope and consistent-organization checks. Denied/absent organization data clears only this catalog; dependency failures are not success, and no secret value or reveal operation is requested.
// projectOrganizationSecrets проецирует только metadata списка организационных секретов в отдельные поля секретов и страницы после проверки предела, области и согласованной организации. Запрещённые или отсутствующие данные очищают только этот каталог; отказ зависимости не является успехом, значение секрета и операция раскрытия не запрашиваются.
func (s *Server) projectOrganizationSecrets(ctx context.Context, catalog map[string]any, localize func(string) string) error {
	catalog["organizationSecrets"], catalog["organizationSecretsPage"] = []any{}, map[string]any{}
	response, err := s.query.ListOrganizationRuntimeSecrets(ctx, &cp.ListOrganizationRuntimeSecretsRequest{Page: platformPage()})
	if err != nil {
		if status.Code(err) == codes.NotFound || status.Code(err) == codes.PermissionDenied {
			return nil
		}
		return err
	}
	if response == nil || len(response.GetSecrets()) > platformSnapshotPageSize {
		return errors.New("organization runtime secret snapshot is invalid")
	}
	organizationRef := ""
	for _, secret := range response.GetSecrets() {
		if secret == nil || secret.GetScopeKind() != cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION || secret.GetProjectRef() != "" || !safeRef.MatchString(secret.GetOrganizationRef()) || organizationRef != "" && secret.GetOrganizationRef() != organizationRef {
			return errors.New("organization runtime secret snapshot scope mismatch")
		}
		organizationRef = secret.GetOrganizationRef()
	}
	projection, err := projectSnapshotPart(response, localize)
	if err != nil {
		return err
	}
	catalog["organizationSecrets"], catalog["organizationSecretsPage"] = projection["secrets"], projection["page"]
	return nil
}
