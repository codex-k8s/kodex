package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	broker "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestImmediateSecretScopeClosedBeforeStorageAndAfterCompletion(t *testing.T) {
	for _, scope := range []cp.RuntimeResourceScopeKind{cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT, cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION} {
		t.Run(scope.String(), func(t *testing.T) {
			for _, kind := range []cp.RuntimeSecretOperationKind{cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE, cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL, cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVOKE} {
				t.Run(kind.String(), func(t *testing.T) {
					newOperation := func() *cp.ConsumeRuntimeSecretOperationResponse {
						var op *cp.ConsumeRuntimeSecretOperationResponse
						if kind == cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE {
							op = mutationOperation(kind, "secop_scoped", 3, []byte("scoped-value"))
						} else {
							op = readOperation(kind, "secop_scoped", 3)
						}
						op.ScopeKind = scope
						if scope == cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION {
							op.ProjectRef = ""
						}
						return op
					}
					invoke := func(server *Server) error {
						switch kind {
						case cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_CREATE:
							_, err := server.CreateSecret(t.Context(), &broker.CreateSecretRequest{OperationGrant: "grant", Value: []byte("scoped-value")})
							return err
						case cp.RuntimeSecretOperationKind_RUNTIME_SECRET_OPERATION_KIND_REVEAL:
							response, err := server.RevealSecret(t.Context(), &broker.RevealSecretRequest{OperationGrant: "grant"})
							if err != nil && len(response.GetValue()) != 0 {
								t.Fatal("failed scope boundary returned plaintext")
							}
							return err
						default:
							_, err := server.RevokeSecret(t.Context(), &broker.RevokeSecretRequest{OperationGrant: "grant"})
							return err
						}
					}
					for name, mutate := range map[string]func(*cp.ConsumeRuntimeSecretOperationResponse){
						"unknown scope": func(op *cp.ConsumeRuntimeSecretOperationResponse) { op.ScopeKind = cp.RuntimeResourceScopeKind(99) },
						"empty scope": func(op *cp.ConsumeRuntimeSecretOperationResponse) {
							op.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_UNSPECIFIED
						},
						"empty organization": func(op *cp.ConsumeRuntimeSecretOperationResponse) { op.OrganizationRef = "" },
						"invalid project tuple": func(op *cp.ConsumeRuntimeSecretOperationResponse) {
							if op.ProjectRef == "" {
								op.ProjectRef = "prj_foreign"
							} else {
								op.ProjectRef = ""
							}
						},
					} {
						t.Run(name, func(t *testing.T) {
							op := newOperation()
							mutate(op)
							owner := successfulOwner("grant", op)
							storage := &fakeStore{}
							server := &Server{owner: owner, store: storage, namespace: runtimeNamespace, maximumSize: 1024}
							if status.Code(invoke(server)) != codes.FailedPrecondition || owner.completionCalls != 0 || len(storage.createdValue) != 0 || storage.deleteCalls != 0 {
								t.Fatal("invalid owner scope reached storage completion")
							}
						})
					}
					for name, mutate := range map[string]func(*cp.RuntimeSecret){
						"foreign organization": func(s *cp.RuntimeSecret) { s.OrganizationRef = "org_foreign" },
						"cross scope": func(s *cp.RuntimeSecret) {
							if scope == cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT {
								s.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_ORGANIZATION
								s.ProjectRef = ""
							} else {
								s.ScopeKind = cp.RuntimeResourceScopeKind_RUNTIME_RESOURCE_SCOPE_KIND_PROJECT
								s.ProjectRef = "prj_foreign"
							}
						},
						"foreign resource": func(s *cp.RuntimeSecret) { s.Ref = "sec_foreign" },
					} {
						t.Run(name, func(t *testing.T) {
							op := newOperation()
							owner := successfulOwner("grant", op)
							mutate(owner.completion)
							storage := &fakeStore{materialized: materialization(op), revealedValue: []byte("stored-value")}
							server := &Server{owner: owner, store: storage, namespace: runtimeNamespace, maximumSize: 1024}
							if status.Code(invoke(server)) != codes.FailedPrecondition || owner.completionCalls != 1 || storage.deleteCalls != 0 {
								t.Fatal("foreign completion returned result or deleted scoped effects")
							}
						})
					}
					op := newOperation()
					owner := successfulOwner("grant", op)
					storage := &fakeStore{materialized: materialization(op), revealedValue: []byte("stored-value")}
					if err := invoke(&Server{owner: owner, store: storage, namespace: runtimeNamespace, maximumSize: 1024}); err != nil {
						t.Fatalf("canonical scoped operation rejected: %v", err)
					}
				})
			}
		})
	}
}
