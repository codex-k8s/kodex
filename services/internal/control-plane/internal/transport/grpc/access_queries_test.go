package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestAccessQueriesAuthenticateBeforeValidationAndService checks all nine access read adapters
// reject missing verified context before touching the nil receiver or validating request data.
// Nil requests also ensure authentication keeps priority over timestamp and decision-shape
// checks. This local guard test does not establish successful membership or live authorization.
// TestAccessQueriesAuthenticateBeforeValidationAndService проверяет, что все девять адаптеров
// чтения доступа отклоняют отсутствие проверенного context до обращения к nil receiver либо
// проверки запроса. Nil-запросы также закрепляют приоритет аутентификации перед проверками
// timestamp и формы решений. Этот локальный тест не доказывает членство или живую авторизацию.
func TestAccessQueriesAuthenticateBeforeValidationAndService(t *testing.T) {
	t.Parallel()
	var server *Server
	for name, call := range map[string]func(context.Context) (bool, error){
		"permission registry": func(ctx context.Context) (bool, error) {
			response, err := server.ListPermissionRegistry(ctx, nil)
			return response != nil, err
		},
		"subjects": func(ctx context.Context) (bool, error) {
			response, err := server.ListAccessSubjects(ctx, nil)
			return response != nil, err
		},
		"OIDC groups": func(ctx context.Context) (bool, error) {
			response, err := server.ListOIDCGroups(ctx, nil)
			return response != nil, err
		},
		"roles": func(ctx context.Context) (bool, error) {
			response, err := server.ListAccessRoles(ctx, nil)
			return response != nil, err
		},
		"role versions": func(ctx context.Context) (bool, error) {
			response, err := server.ListAccessRoleVersions(ctx, nil)
			return response != nil, err
		},
		"bindings": func(ctx context.Context) (bool, error) {
			response, err := server.ListAccessBindings(ctx, nil)
			return response != nil, err
		},
		"effective access": func(ctx context.Context) (bool, error) {
			response, err := server.QueryEffectiveAccess(ctx, nil)
			return response != nil, err
		},
		"explanation": func(ctx context.Context) (bool, error) {
			response, err := server.ExplainAccess(ctx, nil)
			return response != nil, err
		},
		"simulation": func(ctx context.Context) (bool, error) {
			response, err := server.SimulateAccess(ctx, nil)
			return response != nil, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			nonempty, err := call(t.Context())
			if nonempty || status.Code(err) != codes.Unauthenticated {
				t.Fatal("access read validated input or reached service before authentication")
			}
		})
	}
}
