package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"regexp"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/mailpolicy"
	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// sshComposeCommitPattern permits exact immutable Git objects, never refs or abbreviated SHAs.
// sshComposeCommitPattern допускает exact immutable Git objects, не refs и сокращённые SHA.
var sshComposeCommitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

// resolveSSHComposeSnapshot derives target, secret, source and predecessor pins inside the owner transaction.
// resolveSSHComposeSnapshot выводит target, secret, source и predecessor pins в owner transaction.
func (repository *Repository) resolveSSHComposeSnapshot(ctx context.Context, tx pgx.Tx, current scope, projectID, nodeID, connectionID, operation string, input map[string]any, origin *sshcompose.Snapshot) (sshcompose.Snapshot, error) {
	var snapshot sshcompose.Snapshot
	var rawConfiguration []byte
	var projectRef string
	if projectID == "" {
		return snapshot, errs.ErrForbidden
	}
	pins := pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "node_id": nodeID, "connection_id": connectionID,
		"runtime_revision_id": "", "model_lease_id": "", "model_generation": int64(0)}
	if origin != nil {
		pins["runtime_revision_id"], pins["model_lease_id"], pins["model_generation"] = origin.OriginRuntimeRevisionID, origin.OriginLeaseID, origin.OriginGeneration
		if origin.OriginRuntimeRevisionID == "" || origin.OriginLeaseID == "" || origin.OriginGeneration < 1 {
			return snapshot, errs.ErrForbidden
		}
	}
	err := tx.QueryRow(ctx, querySSHComposeContext, pins).Scan(&rawConfiguration, &projectRef, &snapshot.RootRunRef, &snapshot.SessionRef,
		&snapshot.TurnRef, &snapshot.Attempt, &snapshot.OriginRuntimeRevisionID, &snapshot.OriginLeaseID, &snapshot.OriginGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, errs.ErrForbidden
	}
	if err != nil {
		return snapshot, serializableTransactionError(err, errs.ErrUnavailable)
	}
	var configuration map[string]string
	if json.Unmarshal(rawConfiguration, &configuration) != nil {
		return snapshot, errs.ErrUnavailable
	}
	target, err := sshcompose.TargetFromConfiguration(configuration)
	if err != nil || target.ProjectRef != projectRef || mailpolicy.ValidateAddresses([]netip.Addr{netip.MustParseAddr(target.Address)}) != nil {
		return snapshot, errs.ErrForbidden
	}
	snapshot.Version, snapshot.Target = sshcompose.ProtocolVersion, target
	snapshot.SSHSecret, err = resolveSSHComposeSecret(ctx, tx, current.organizationID, projectID, configuration["ssh_secret_ref"])
	if err != nil {
		return snapshot, err
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: snapshot.SSHSecret.Ref}); err != nil {
		return snapshot, err
	}
	switch operation {
	case "ssh.compose.inspect":
		mergeRef, _ := input["merge_invocation_ref"].(string)
		if mergeRef != "" {
			snapshot.MergeInvocationRef = mergeRef
			snapshot.Source, snapshot.MergedSHA, err = repository.resolveSSHComposeSource(ctx, tx, current.organizationID, projectID, nodeID, snapshot.OriginRuntimeRevisionID, target, mergeRef)
		}
	case "ssh.compose.deploy", "ssh.compose.status":
		key, predecessorOperation := "inspect_invocation_ref", "ssh.compose.inspect"
		if operation == "ssh.compose.status" {
			key, predecessorOperation = "deployment_invocation_ref", "ssh.compose.deploy"
		}
		ref, _ := input[key].(string)
		var raw, summary []byte
		var digest, predecessorRef, predecessorInput, predecessorEffect string
		if err := tx.QueryRow(ctx, querySSHComposePredecessor, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "project_id": projectID, "connection_id": connectionID,
			"predecessor_ref": ref, "operation": predecessorOperation, "node_id": nodeID,
		}).Scan(&raw, &summary, &digest, &predecessorRef, &predecessorInput, &predecessorEffect); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return snapshot, errs.ErrForbidden
			}
			return snapshot, serializableTransactionError(err, errs.ErrUnavailable)
		}
		var predecessor sshcompose.Snapshot
		if sshcompose.Decode(raw, &predecessor, 32768) != nil || predecessor.Target != target || predecessor.Source == nil {
			return snapshot, errs.ErrForbidden
		}
		snapshot.MergeInvocationRef = predecessor.MergeInvocationRef
		snapshot.Source, snapshot.MergedSHA, err = repository.resolveSSHComposeSource(ctx, tx, current.organizationID, projectID, nodeID, snapshot.OriginRuntimeRevisionID, target, predecessor.MergeInvocationRef)
		if err != nil {
			return snapshot, err
		}
		if snapshot.MergedSHA != predecessor.MergedSHA || !reflect.DeepEqual(snapshot.Source, predecessor.Source) {
			return snapshot, errs.ErrForbidden
		}
		if operation == "ssh.compose.deploy" {
			var result struct {
				Ready        bool   `json:"ready"`
				TargetRef    string `json:"target_ref"`
				MergedSHA    string `json:"merged_sha"`
				TreeSHA      string `json:"tree_sha"`
				BundleSHA256 string `json:"bundle_sha256"`
			}
			if sshcompose.Digest(summary) != digest || sshcompose.Decode(summary, &result, 16384) != nil || !result.Ready ||
				result.TargetRef != target.Ref || result.MergedSHA != snapshot.MergedSHA || !sshComposeCommitPattern.MatchString(result.TreeSHA) || !validRuntimeSecretSHA256(result.BundleSHA256) {
				return snapshot, errs.ErrForbidden
			}
			snapshot.InspectInvocationRef, snapshot.TreeSHA, snapshot.BundleSHA256 = predecessorRef, result.TreeSHA, result.BundleSHA256
		} else {
			snapshot.InspectInvocationRef, snapshot.TreeSHA, snapshot.BundleSHA256 = predecessor.InspectInvocationRef, predecessor.TreeSHA, predecessor.BundleSHA256
			snapshot.DeploymentRef, snapshot.DeploymentInputDigest, snapshot.DeploymentEffectKey = predecessorRef, predecessorInput, predecessorEffect
		}
	default:
		return snapshot, errs.ErrForbidden
	}
	return snapshot, err
}

// validateSSHComposeConfiguration binds public metadata to an authorized active project and secret before persistence.
// validateSSHComposeConfiguration связывает public metadata с разрешёнными active project и secret до сохранения.
func (repository *Repository) validateSSHComposeConfiguration(ctx context.Context, tx pgx.Tx, current scope, connectionRef string, configuration map[string]string) error {
	target, err := sshcompose.TargetFromConfiguration(configuration)
	if err != nil || mailpolicy.ValidateAddresses([]netip.Addr{netip.MustParseAddr(target.Address)}) != nil {
		return errs.ErrInvalid
	}
	project, err := repository.resolveAccessTarget(ctx, tx, current.organizationID, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: target.ProjectRef, ProjectRef: target.ProjectRef})
	if err != nil {
		return err
	}
	if err := repository.requireAccess(ctx, tx, current, "project.manage", project); err != nil {
		return err
	}
	connectionCommand := command.Command{Kind: command.CreateConnection, Payload: command.ConnectionInput{Ref: connectionRef}}
	if connectionRef != "" {
		connectionCommand.Kind = command.UpdateConnection
	}
	permission, accessTarget, err := repository.commandAccessTarget(ctx, tx, current, connectionCommand)
	if err != nil {
		return err
	}
	if permission == "" {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, permission, accessTarget); err != nil {
		return err
	}
	var lockedRef, purpose string
	if err := tx.QueryRow(ctx, querySSHComposeSecretLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": project.projectID, "secret_ref": configuration["ssh_secret_ref"]}).Scan(&lockedRef, &purpose); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrForbidden
		}
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := repository.requireAccess(ctx, tx, current, "secret.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: lockedRef}); err != nil {
		return err
	}
	var secretID string
	if err := tx.QueryRow(ctx, querySSHComposeSecretReserve, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": project.projectID, "secret_ref": lockedRef}).Scan(&secretID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if _, err := tx.Exec(ctx, querySSHComposeSecretRedact, pgx.StrictNamedArgs{"secret_id": secretID}); err != nil {
		return errs.ErrUnavailable
	}
	secret, err := resolveSSHComposeSecret(ctx, tx, current.organizationID, project.projectID, configuration["ssh_secret_ref"])
	if err != nil {
		return err
	}
	return repository.requireAccess(ctx, tx, current, "secret.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: secret.Ref})
}

// resolveSSHComposeSecret pins the current active project revision without loading plaintext.
// resolveSSHComposeSecret закрепляет текущую active project revision без чтения plaintext.
func resolveSSHComposeSecret(ctx context.Context, tx pgx.Tx, organizationID, projectID, ref string) (sshcompose.SecretBinding, error) {
	var binding sshcompose.SecretBinding
	var revision int64
	if err := tx.QueryRow(ctx, querySSHComposeSecretPin, pgx.StrictNamedArgs{"organization_id": organizationID, "project_id": projectID, "secret_ref": ref}).Scan(&revision); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return binding, errs.ErrForbidden
		}
		return binding, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if revision < 1 {
		return binding, errs.ErrUnavailable
	}
	err := tx.QueryRow(ctx, queryRuntimeSecretResolveBinding, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "scope_kind": "PROJECT", "secret_ref": ref, "revision": revision,
		"purpose": "SSH_COMPOSE",
	}).Scan(&binding.Ref, &binding.Namespace, &binding.Revision, &binding.Name, &binding.Key, &binding.UID, &binding.ResourceVersion, &binding.ContentSHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return binding, errs.ErrForbidden
	}
	if err != nil {
		return binding, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return binding, nil
}

// resolveSSHComposeSource checks committed merge evidence and all three current source read grants.
// resolveSSHComposeSource проверяет committed merge evidence и три current source read grants.
func (repository *Repository) resolveSSHComposeSource(ctx context.Context, tx pgx.Tx, organizationID, projectID, nodeID, revisionID string, target sshcompose.Target, mergeRef string) (*sshcompose.SourceBinding, string, error) {
	parts := strings.Split(target.Repository, "/")
	var scopeJSON, summary []byte
	var digest string
	var source sshcompose.SourceBinding
	err := tx.QueryRow(ctx, querySSHComposeMerge, pgx.StrictNamedArgs{
		"organization_id": organizationID, "project_id": projectID, "node_id": nodeID, "merge_ref": mergeRef, "owner": parts[0], "repository": parts[1], "target_ref": target.Ref,
	}).Scan(&scopeJSON, &summary, &digest, &source.ConnectionRef, &source.DefinitionVersion, &source.DefinitionDigest,
		&source.CredentialRef, &source.CredentialRevision, &source.SecretRef, &source.SecretUID, &source.SecretResourceVersion, &source.CredentialSHA256)
	var result struct {
		Merged bool   `json:"merged"`
		SHA    string `json:"sha"`
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", errs.ErrForbidden
	}
	if err != nil {
		return nil, "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	if sshcompose.Digest(summary) != digest || sshcompose.Decode(summary, &result, 16384) != nil || !result.Merged || !sshComposeCommitPattern.MatchString(result.SHA) {
		return nil, "", errs.ErrForbidden
	}
	rows, err := tx.Query(ctx, querySSHComposeSourceGrants, pgx.StrictNamedArgs{
		"organization_id": organizationID, "node_id": nodeID, "runtime_revision_id": revisionID, "connection_ref": source.ConnectionRef, "owner": parts[0], "repository": parts[1],
	})
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var grant sshcompose.GrantBinding
		if rows.Scan(&grant.Ref, &grant.Version, &grant.Key) != nil {
			return nil, "", errs.ErrUnavailable
		}
		source.Grants = append(source.Grants, grant)
	}
	if err := rows.Err(); err != nil {
		return nil, "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	if len(source.Grants) != 3 {
		return nil, "", errs.ErrForbidden
	}
	return &source, result.SHA, nil
}
