// Package controlplane реализует версионированное сквозное чтение Redis поверх
// PostgreSQL.
package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	sharedcache "github.com/codex-k8s/matter-codex/libs/go/cache"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
)

const maximumCacheValueBytes = 128 << 10

type cacheEnvelope struct {
	Version          uint32          `json:"version"`
	OrganizationID   string          `json:"organizationId"`
	ProjectID        string          `json:"projectId"`
	Kind             enum.Kind       `json:"kind"`
	ResourceID       string          `json:"resourceId"`
	ResourceVersion  uint64          `json:"resourceVersion"`
	CacheEpoch       uint64          `json:"cacheEpoch"`
	KeyDigestSHA256  string          `json:"keyDigestSha256"`
	ProjectionSHA256 string          `json:"projectionSha256"`
	Resource         json.RawMessage `json:"resource"`
}

// Repository кэширует только снимки ресурсов с эпохой, принадлежащей PostgreSQL.
type Repository struct {
	source domainrepo.Repository
	engine *sharedcache.Engine[cacheEnvelope]
}

var _ domainrepo.Repository = (*Repository)(nil)

// New создаёт декоратор; при сбое кэша чтение всегда возвращается к PostgreSQL.
func New(
	source domainrepo.Repository,
	store sharedcache.Store,
	timeout, ttl time.Duration,
) (*Repository, error) {
	if source == nil {
		return nil, errors.New("control-plane cache source is required")
	}
	engine, err := sharedcache.New[cacheEnvelope](
		store,
		envelopeCodec{},
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
	scope domainrepo.Scope,
	callback func(domainrepo.Transaction) error,
) error {
	return repository.source.Transact(ctx, scope, callback)
}

func (repository *Repository) Get(
	ctx context.Context,
	organizationID, projectID, resourceID string,
	expectedKind enum.Kind,
) (entity.Resource, error) {
	// RoleImageRecipe содержит multiline installation block и bounded secret
	// references. Этот owner state никогда не материализуется в Redis.
	if expectedKind == enum.KindRoleImageRecipe {
		return repository.source.Get(ctx, organizationID, projectID, resourceID, expectedKind)
	}
	epoch, err := repository.source.CacheEpoch(ctx, organizationID, projectID)
	if err != nil {
		return entity.Resource{}, err
	}
	keyDigest := digestCacheKey(
		organizationID,
		projectID,
		expectedKind,
		resourceID,
		epoch,
	)
	key := "control-plane:v2:resource:" + keyDigest
	envelope, err := repository.engine.GetOrSet(
		ctx,
		key,
		resourceEnvelopeSource{
			repository:     repository.source,
			organizationID: organizationID,
			projectID:      projectID,
			resourceID:     resourceID,
			expectedKind:   expectedKind,
			epoch:          epoch,
			keyDigest:      keyDigest,
		},
	)
	if err != nil {
		return entity.Resource{}, err
	}
	resource, valid := validateEnvelope(
		envelope,
		organizationID,
		projectID,
		expectedKind,
		resourceID,
		epoch,
		keyDigest,
	)
	if valid {
		return resource, nil
	}
	_ = repository.engine.Invalidate(ctx, key)
	resource, err = repository.source.Get(
		ctx,
		organizationID,
		projectID,
		resourceID,
		expectedKind,
	)
	if err != nil {
		return entity.Resource{}, err
	}
	repaired, err := makeEnvelope(resource, epoch, keyDigest)
	if err == nil {
		_ = repository.engine.Store(ctx, key, repaired)
	}
	return resource, nil
}

// GetIncludingDeleted обходит projection cache: terminal tombstone нужен
// только специализированному idempotency readback lifecycle-команды.
func (repository *Repository) GetIncludingDeleted(
	ctx context.Context,
	organizationID, projectID, resourceID string,
	expectedKind enum.Kind,
) (entity.Resource, error) {
	return repository.source.GetIncludingDeleted(ctx, organizationID, projectID, resourceID, expectedKind)
}

type resourceEnvelopeSource struct {
	repository     domainrepo.Repository
	organizationID string
	projectID      string
	resourceID     string
	expectedKind   enum.Kind
	epoch          uint64
	keyDigest      string
}

func (source resourceEnvelopeSource) Get(ctx context.Context) (cacheEnvelope, error) {
	resource, err := source.repository.Get(
		ctx,
		source.organizationID,
		source.projectID,
		source.resourceID,
		source.expectedKind,
	)
	if err != nil {
		return cacheEnvelope{}, err
	}
	return makeEnvelope(resource, source.epoch, source.keyDigest)
}

func (repository *Repository) List(
	ctx context.Context,
	filter query.ResourceFilter,
) ([]entity.Resource, error) {
	return repository.source.List(ctx, filter)
}

func (repository *Repository) Search(
	ctx context.Context,
	filter query.ResourceSearch,
) ([]entity.Resource, error) {
	return repository.source.Search(ctx, filter)
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

func (repository *Repository) ListAudit(
	ctx context.Context,
	filter query.AuditFilter,
) ([]domainrepo.Audit, error) {
	return repository.source.ListAudit(ctx, filter)
}

func (repository *Repository) ListTombstones(
	ctx context.Context,
	filter query.TombstoneFilter,
) ([]domainrepo.Tombstone, error) {
	return repository.source.ListTombstones(ctx, filter)
}

func (repository *Repository) ListRuntimeIncidents(
	ctx context.Context,
	filter query.RuntimeIncidentFilter,
) ([]domainrepo.RuntimeIncident, error) {
	return repository.source.ListRuntimeIncidents(ctx, filter)
}

func (repository *Repository) ListScheduleOccurrences(
	ctx context.Context,
	filter query.ScheduleOccurrenceFilter,
) ([]domainrepo.ScheduleOccurrence, error) {
	return repository.source.ListScheduleOccurrences(ctx, filter)
}

func (repository *Repository) Diagnostics(
	ctx context.Context,
	scope domainrepo.Scope,
) (domainrepo.Diagnostics, error) {
	return repository.source.Diagnostics(ctx, scope)
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

type envelopeCodec struct{}

func (envelopeCodec) Marshal(envelope cacheEnvelope) ([]byte, error) {
	if _, valid := validateEnvelope(
		envelope,
		envelope.OrganizationID,
		envelope.ProjectID,
		envelope.Kind,
		envelope.ResourceID,
		envelope.CacheEpoch,
		envelope.KeyDigestSHA256,
	); !valid {
		return nil, errors.New("cache envelope is invalid")
	}
	raw, err := json.Marshal(envelope)
	if err != nil || len(raw) > maximumCacheValueBytes {
		return nil, errors.New("encode cache envelope")
	}
	return raw, nil
}

func (envelopeCodec) Unmarshal(raw []byte) (cacheEnvelope, error) {
	if len(raw) == 0 || len(raw) > maximumCacheValueBytes {
		return cacheEnvelope{}, errors.New("cached resource size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope cacheEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return cacheEnvelope{}, errors.New("decode cache envelope")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cacheEnvelope{}, errors.New("cache envelope has trailing data")
	}
	return envelope, nil
}

func makeEnvelope(
	resource entity.Resource,
	epoch uint64,
	keyDigest string,
) (cacheEnvelope, error) {
	projection, err := entity.MarshalSnapshot(resource)
	if err != nil {
		return cacheEnvelope{}, err
	}
	projectionDigest, err := entity.ProjectionSHA256(resource)
	if err != nil {
		return cacheEnvelope{}, err
	}
	return cacheEnvelope{
		Version:          1,
		OrganizationID:   resource.OrganizationID,
		ProjectID:        resource.ProjectID,
		Kind:             resource.Kind,
		ResourceID:       resource.ID,
		ResourceVersion:  resource.Version,
		CacheEpoch:       epoch,
		KeyDigestSHA256:  keyDigest,
		ProjectionSHA256: projectionDigest,
		Resource:         projection,
	}, nil
}

func validateEnvelope(
	envelope cacheEnvelope,
	organizationID, projectID string,
	kind enum.Kind,
	resourceID string,
	epoch uint64,
	keyDigest string,
) (entity.Resource, bool) {
	if envelope.Version != 1 ||
		envelope.OrganizationID != organizationID ||
		envelope.ProjectID != projectID ||
		envelope.Kind != kind ||
		envelope.ResourceID != resourceID ||
		envelope.CacheEpoch != epoch ||
		envelope.KeyDigestSHA256 != keyDigest ||
		keyDigest != digestCacheKey(
			organizationID,
			projectID,
			kind,
			resourceID,
			epoch,
		) {
		return entity.Resource{}, false
	}
	resource, err := entity.UnmarshalSnapshot(envelope.Resource)
	if err != nil ||
		resource.OrganizationID != organizationID ||
		resource.ProjectID != projectID ||
		resource.Kind != kind ||
		resource.ID != resourceID ||
		resource.Version != envelope.ResourceVersion {
		return entity.Resource{}, false
	}
	digest, err := entity.ProjectionSHA256(resource)
	if err != nil || digest != envelope.ProjectionSHA256 {
		return entity.Resource{}, false
	}
	return resource, true
}

func digestCacheKey(
	organizationID, projectID string,
	kind enum.Kind,
	resourceID string,
	epoch uint64,
) string {
	canonical := organizationID + "\x00" +
		projectID + "\x00" +
		string(kind) + "\x00" +
		resourceID + "\x00" +
		strconv.FormatUint(epoch, 10)
	digest := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(digest[:])
}
