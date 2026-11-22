package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesGrantCurrent resolves an exact owner-scoped namespace row before child mutation.
// queryKubernetesGrantCurrent разрешает exact owner-scoped namespace row до child mutation.
//
//go:embed sql/kubernetes_grant__current.sql
var queryKubernetesGrantCurrent string

// queryKubernetesGrantCreate inserts one server-assigned scope without retargeting another namespace.
// queryKubernetesGrantCreate создаёт server-assigned scope без retargeting другой namespace.
//
//go:embed sql/kubernetes_grant__create.sql
var queryKubernetesGrantCreate string

// queryKubernetesGrantChange changes only the exact existing ref and immutable namespace digest.
// queryKubernetesGrantChange меняет лишь exact existing ref и immutable namespace digest.
//
//go:embed sql/kubernetes_grant__change.sql
var queryKubernetesGrantChange string

// changeKubernetesGrantTx preserves the caller's Connection OCC and owner/recipient authorization.
// A missing ref may upsert only the same canonical namespace; update/revoke never select an arbitrary
// sibling scope. Existing uncertain work blocks changes of that exact row, not unrelated scopes.
// changeKubernetesGrantTx сохраняет Connection OCC и owner/recipient authorization caller.
// Missing ref допускает upsert лишь той же canonical namespace; update/revoke не выбирает случайный
// sibling scope. Existing uncertain work запрещает изменение exact row, не unrelated scopes.
func changeKubernetesGrantTx(ctx context.Context, tx pgx.Tx, current scope, connectionID string, definition integrationpackage.Package, capability integrationpackage.Capability, payload command.IntegrationGrantInput, targetKind, targetRef string, encodedScope []byte, scopeDigest string) (string, error) {
	if definition.Metadata.Key != integrationpackage.KubernetesDefinitionKey || capability.ResourceScope.Kind != "KUBERNETES_TARGET" ||
		managedkubernetes.ValidateGrantNamespace(capability.Operation, payload.KubernetesNamespace) != nil ||
		payload.KubernetesGrantRef != "" && !managedkubernetes.ValidLocator(payload.KubernetesGrantRef) || !payload.Enabled && payload.KubernetesGrantRef == "" {
		return "", errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_id": connectionID, "capability": payload.CapabilityKey,
		"target_kind": targetKind, "target_ref": targetRef, "resource_scope": encodedScope, "resource_scope_digest": scopeDigest, "grant_ref": payload.KubernetesGrantRef}
	var ref, policy string
	var paths []string
	var active bool
	err := tx.QueryRow(ctx, queryKubernetesGrantCurrent, args).Scan(&ref, &policy, &paths, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		if payload.KubernetesGrantRef != "" || !payload.Enabled {
			return "", errs.ErrNotFound
		}
	} else if err != nil {
		return "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	nextPaths := sortedApprovalScopePaths(payload.ApprovalScopePaths)
	if ref != "" && active && payload.Enabled {
		return "", errs.ErrConflict
	}
	delete(args, "grant_ref")
	args["policy"], args["scope_paths"], args["risk"] = payload.ApprovalPolicy, nextPaths, capability.Risk
	if !payload.Enabled {
		args["policy"], args["scope_paths"] = policy, paths
	}
	args["definition_version"], args["definition_digest"] = definition.Metadata.Version, definition.Digest
	if ref == "" {
		ref, err = newRef("grt")
		if err != nil {
			return "", errs.ErrUnavailable
		}
		args["ref"], args["actor_id"] = ref, current.actorID
		if err := tx.QueryRow(ctx, queryKubernetesGrantCreate, args).Scan(&ref); err != nil {
			return "", mapWriteError(err)
		}
	} else {
		args["ref"], args["enabled"] = ref, payload.Enabled
		if err := tx.QueryRow(ctx, queryKubernetesGrantChange, args).Scan(&ref); err != nil {
			return "", serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	return ref, nil
}
