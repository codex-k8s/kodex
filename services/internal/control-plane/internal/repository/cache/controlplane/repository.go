// Package controlplane реализует versioned Redis read-through поверх PostgreSQL.
package controlplane

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"time"

	sharedcache "github.com/codex-k8s/matter-codex/libs/go/cache"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
)

const maximumCacheValueBytes = 128 << 10

// Repository кэширует только resource snapshots с PostgreSQL-owned epoch.
type Repository struct {
	source domainrepo.Repository
	engine *sharedcache.Engine[entity.Resource]
}

var _ domainrepo.Repository = (*Repository)(nil)

// New создаёт decorator; cache failure всегда отступает к PostgreSQL.
func New(
	source domainrepo.Repository,
	store sharedcache.Store,
	timeout, ttl time.Duration,
) (*Repository, error) {
	if source == nil {
		return nil, errors.New("control-plane cache source is required")
	}
	engine, err := sharedcache.New[entity.Resource](
		store,
		resourceCodec{},
		timeout,
		ttl,
	)
	if err != nil {
		return nil, err
	}
	return &Repository{source: source, engine: engine}, nil
}

func (repository *Repository) Transact(
	ctx context.Context,
	organizationID, projectID string,
	callback func(domainrepo.Transaction) error,
) error {
	return repository.source.Transact(ctx, organizationID, projectID, callback)
}

func (repository *Repository) Get(
	ctx context.Context,
	organizationID, projectID, resourceID string,
) (entity.Resource, error) {
	epoch, err := repository.source.CacheEpoch(ctx, organizationID, projectID)
	if err != nil {
		return entity.Resource{}, err
	}
	key := fmt.Sprintf(
		"control-plane:v1:resource:%s:%s:%d:%s",
		organizationID,
		scopeKey(projectID),
		epoch,
		resourceID,
	)
	return repository.engine.Load(ctx, key, func(ctx context.Context) (entity.Resource, error) {
		return repository.source.Get(ctx, organizationID, projectID, resourceID)
	})
}

func (repository *Repository) List(
	ctx context.Context,
	filter query.ResourceFilter,
) ([]entity.Resource, error) {
	return repository.source.List(ctx, filter)
}

func (repository *Repository) ListEligibleProjects(
	ctx context.Context,
	organizationID, actorID, afterID string,
	limit int,
) ([]entity.Resource, error) {
	return repository.source.ListEligibleProjects(
		ctx,
		organizationID,
		actorID,
		afterID,
		limit,
	)
}

func (repository *Repository) CacheEpoch(
	ctx context.Context,
	organizationID, projectID string,
) (uint64, error) {
	return repository.source.CacheEpoch(ctx, organizationID, projectID)
}

func (repository *Repository) Check(ctx context.Context) error {
	return repository.source.Check(ctx)
}

func (repository *Repository) Close() {
	repository.source.Close()
}

type resourceCodec struct{}

func (resourceCodec) Marshal(resource entity.Resource) ([]byte, error) {
	if err := resource.Validate(); err != nil {
		return nil, errors.New("cache resource is invalid")
	}
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(&resource); err != nil ||
		buffer.Len() > maximumCacheValueBytes {
		return nil, errors.New("encode cached resource")
	}
	return buffer.Bytes(), nil
}

func (resourceCodec) Unmarshal(raw []byte) (entity.Resource, error) {
	if len(raw) == 0 || len(raw) > maximumCacheValueBytes {
		return entity.Resource{}, errors.New("cached resource size is invalid")
	}
	decoder := gob.NewDecoder(bytes.NewReader(raw))
	var resource entity.Resource
	if err := decoder.Decode(&resource); err != nil ||
		resource.Validate() != nil {
		return entity.Resource{}, errors.New("decode cached resource")
	}
	return resource, nil
}

func scopeKey(projectID string) string {
	if projectID == "" {
		return "tenant"
	}
	return projectID
}

func init() {
	gob.Register(entity.ProjectSpec{})
	gob.Register(entity.TeamSpec{})
	gob.Register(entity.ChatSpec{})
	gob.Register(entity.RoleSpec{})
	gob.Register(entity.PromptProfileSpec{})
	gob.Register(entity.CredentialBindingSpec{})
	gob.Register(entity.RepositoryWorkspaceSpec{})
	gob.Register(entity.IntegrationSpec{})
	gob.Register(entity.RuntimeRevisionSpec{})
	gob.Register(entity.SessionSpec{})
	gob.Register(entity.TurnSpec{})
	gob.Register(entity.ProcessRunSpec{})
	gob.Register(entity.ScheduleSpec{})
	gob.Register(entity.OwnerGateSpec{})
	gob.Register(entity.MemoryRecordSpec{})
	gob.Register(entity.WorkClaimSpec{})
	gob.Register(entity.ArtifactSpec{})
}
