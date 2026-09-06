package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/file_binding_target_artifact.sql
var queryFileBindingTargetArtifact string

//go:embed sql/file_binding_target_agents.sql
var queryFileBindingTargetAgents string

const (
	fileTargetAvailable           = "AVAILABLE"
	fileTargetAlreadyBound        = "ALREADY_BOUND"
	fileTargetNotBound            = "NOT_BOUND"
	fileTargetCapabilityRequired  = "AGENT_CAPABILITY_REQUIRED"
	fileTargetArchived            = "AGENT_ARCHIVED"
	fileTargetArtifactUnavailable = "ARTIFACT_UNAVAILABLE"
)

type fileBindingArtifact struct {
	ID, Ref, ProjectID, ProjectRef, Lifecycle, Scan string
	Version                                         int64
}

type fileBindingAgent struct {
	ID, OwnerRef  string
	HasCapability bool
	entity.ArtifactBindingTarget
}

type fileTargetCursor struct{ Scope, Digest, Ref string }

func (repository *Repository) readFileBindingArtifact(ctx context.Context, tx pgx.Tx, current scope, ref string) (fileBindingArtifact, error) {
	item := fileBindingArtifact{Ref: ref}
	if err := repository.requireAccess(ctx, tx, current, "artifact.bind", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: ref}); err != nil {
		return item, err
	}
	var artifact entity.Artifact
	artifact.Ref = ref
	if err := projectArtifactEligibility(ctx, tx, current, &artifact); err != nil {
		return item, err
	}
	err := tx.QueryRow(ctx, queryFileBindingTargetArtifact, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "authority_project_id": current.authorityProjectID, "artifact_ref": ref,
	}).Scan(&item.ID, &item.Version, &item.ProjectID, &item.ProjectRef, &item.Lifecycle, &item.Scan)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, errs.ErrNotFound
	}
	if err != nil {
		return item, errs.ErrUnavailable
	}
	return item, nil
}

func (repository *Repository) readFileBindingAgents(ctx context.Context, tx pgx.Tx, current scope, artifact fileBindingArtifact, agentRef, after, search string) ([]fileBindingAgent, error) {
	rows, err := tx.Query(ctx, queryFileBindingTargetAgents, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "project_id": artifact.ProjectID,
		"artifact_id": artifact.ID, "capability": runtimecontract.ArtifactCapability,
		"agent_ref": agentRef, "after_ref": after, "query": search,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	items := []fileBindingAgent{}
	for rows.Next() {
		var item fileBindingAgent
		if rows.Scan(&item.ID, &item.AgentRef, &item.AgentVersion, &item.Name, &item.State, &item.OwnerRef, &item.HasCapability, &item.Bound) != nil {
			return nil, errs.ErrUnavailable
		}
		if item.AgentVersion < 1 || !contains([]string{"DRAFT", "READY", "RUNNING", "DISABLED", "ARCHIVED"}, item.State) {
			return nil, errs.ErrUnavailable
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return items, nil
}

func (repository *Repository) fileBindingAgentVisible(ctx context.Context, tx pgx.Tx, current scope, artifact fileBindingArtifact, item fileBindingAgent) error {
	// Tombstone разрешается owner-запросом только для существующей связи. Общий
	// каталог Agent не расширяется и продолжает исключать архивные записи.
	target := resolvedAccessTarget{resourceID: item.ID, projectID: artifact.ProjectID, ownerSubjectRef: item.OwnerRef,
		scope: entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "AGENT", ResourceRef: item.AgentRef,
			ProjectRef: artifact.ProjectRef, RelatedResourceRefs: map[string]string{"PROJECT": artifact.ProjectRef}}}
	return repository.requireAccess(ctx, tx, current, "agent.view", target)
}

func projectFileBindingTarget(artifact fileBindingArtifact, item fileBindingAgent) entity.ArtifactBindingTarget {
	result := item.ArtifactBindingTarget
	result.BindReason, result.UnbindReason = fileTargetAvailable, fileTargetAvailable
	switch {
	case artifact.Lifecycle != "ACTIVE" || artifact.Scan != "CLEAN":
		result.BindReason = fileTargetArtifactUnavailable
	case item.State == "ARCHIVED":
		result.BindReason = fileTargetArchived
	case !item.HasCapability:
		result.BindReason = fileTargetCapabilityRequired
	case item.Bound:
		result.BindReason = fileTargetAlreadyBound
	}
	switch {
	case artifact.Lifecycle != "ACTIVE" || artifact.Scan != "CLEAN":
		result.UnbindReason = fileTargetArtifactUnavailable
	case !item.Bound:
		result.UnbindReason = fileTargetNotBound
	}
	result.CanBind, result.CanUnbind = result.BindReason == fileTargetAvailable, result.UnbindReason == fileTargetAvailable
	return result
}

func (repository *Repository) authorizeFileBindingTarget(ctx context.Context, tx pgx.Tx, current scope, payload command.ArtifactBindingInput) error {
	artifact, err := repository.readFileBindingArtifact(ctx, tx, current, payload.ArtifactRef)
	if err != nil {
		return err
	}
	items, err := repository.readFileBindingAgents(ctx, tx, current, artifact, payload.AgentRef, "", "")
	if err != nil {
		return err
	}
	if len(items) != 1 {
		return errs.ErrNotFound
	}
	return repository.fileBindingAgentVisible(ctx, tx, current, artifact, items[0])
}

func (repository *Repository) ListArtifactBindingTargets(ctx context.Context, principal value.Principal, artifactRef string, filter query.Filter) (entity.ArtifactBindingTargets, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := entity.ArtifactBindingTargets{ArtifactRef: artifactRef, Items: []entity.ArtifactBindingTarget{}}
	filter.Query = strings.TrimSpace(filter.Query)
	if !validOverlayHistoryRef(artifactRef) || !utf8.ValidString(filter.Query) || len([]rune(filter.Query)) > 200 || strings.ContainsRune(filter.Query, '\x00') || len(filter.Page.Token) > 2048 {
		return result, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	artifact, err := repository.readFileBindingArtifact(ctx, tx, current, artifactRef)
	if err != nil {
		return result, err
	}
	result.ArtifactVersion, result.ProjectRef = artifact.Version, artifact.ProjectRef
	all := []entity.ArtifactBindingTarget{}
	var after string
	for scanned := 0; ; {
		items, err := repository.readFileBindingAgents(ctx, tx, current, artifact, "", after, filter.Query)
		if err != nil {
			return result, err
		}
		scanned += len(items)
		if scanned > 10000 {
			return result, errs.ErrUnavailable
		}
		for _, item := range items {
			err := repository.fileBindingAgentVisible(ctx, tx, current, artifact, item)
			if errors.Is(err, errs.ErrNotFound) {
				continue
			}
			if err != nil {
				return result, err
			}
			all = append(all, projectFileBindingTarget(artifact, item))
		}
		if len(items) < 100 {
			break
		}
		after = items[len(items)-1].AgentRef
	}
	scopeDigest := fileTargetDigest([]any{current.organizationID, current.actorID, current.authorityProjectID, artifactRef, filter.Query})
	result.Digest = fileTargetDigest([]any{scopeDigest, artifact.Version, artifact.Lifecycle, artifact.Scan, all})
	start := 0
	if filter.Page.Token != "" {
		raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
		var cursor fileTargetCursor
		if err != nil || decodeStrict(raw, &cursor) != nil || cursor.Scope != scopeDigest || cursor.Ref == "" {
			return result, errs.ErrInvalid
		}
		if cursor.Digest != result.Digest {
			return result, errs.ErrVersionMismatch
		}
		found := false
		for index, item := range all {
			if item.AgentRef == cursor.Ref {
				start, found = index+1, true
				break
			}
		}
		if !found {
			return result, errs.ErrVersionMismatch
		}
	}
	end := min(start+int(boundedPage(filter.Page)), len(all))
	result.Items = all[start:end]
	result.Total = int64(len(all))
	if end < len(all) {
		raw, _ := json.Marshal(fileTargetCursor{Scope: scopeDigest, Digest: result.Digest, Ref: all[end-1].AgentRef})
		result.NextPageToken = base64.RawURLEncoding.EncodeToString(raw)
	}
	result.EvaluatedAt = time.Now().UTC()
	if err := tx.Commit(ctx); err != nil {
		return entity.ArtifactBindingTargets{}, errs.ErrUnavailable
	}
	return result, nil
}

func fileTargetDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
