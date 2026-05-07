package catalog

import (
	"encoding/base64"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

const (
	defaultPageSize = int32(100)
	maxPageSize     = int32(500)
)

type pageQueryArgs struct {
	pgx.NamedArgs
	PageSize int32
	Offset   int64
}

func packageSourceArgs(source entity.PackageSource) pgx.NamedArgs {
	return withVersionedBaseArgs(source.VersionedBase, pgx.NamedArgs{
		"organization_id":      postgreslib.NullableUUID(source.OrganizationID),
		"slug":                 source.Slug,
		"display_name":         source.DisplayName,
		"source_kind":          string(source.Kind),
		"repository_ref":       source.RepositoryRef,
		"catalog_endpoint_ref": source.CatalogEndpointRef,
		"status":               string(source.Status),
		"last_sync_at":         postgreslib.NullableTime(source.LastSyncAt),
		"last_error":           source.LastError,
	})
}

func packageArgs(entry entity.PackageEntry) pgx.NamedArgs {
	return withVersionedBaseArgs(entry.VersionedBase, pgx.NamedArgs{
		"source_id":         postgreslib.NullableUUID(entry.SourceID),
		"slug":              entry.Slug,
		"package_kind":      string(entry.Kind),
		"publisher_ref":     entry.PublisherRef,
		"display_name":      localizedTextPayload(entry.DisplayName),
		"description":       localizedTextPayload(entry.Description),
		"icon_object_uri":   entry.IconObjectURI,
		"commercial_status": string(entry.CommercialStatus),
		"trust_status":      string(entry.TrustStatus),
		"status":            string(entry.Status),
	})
}

func packageVersionArgs(version entity.PackageVersion) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  version.ID,
		"package_id":          version.PackageID,
		"version_label":       version.VersionLabel,
		"source_ref_kind":     string(version.SourceRef.Kind),
		"source_ref":          version.SourceRef.Ref,
		"source_commit_sha":   version.SourceRef.CommitSHA,
		"manifest_digest":     version.ManifestDigest,
		"verification_status": string(version.VerificationStatus),
		"release_status":      string(version.ReleaseStatus),
		"revision":            version.Revision,
		"published_at":        postgreslib.NullableTime(version.PublishedAt),
		"created_at":          version.CreatedAt,
		"updated_at":          version.UpdatedAt,
	}
}

func packageVersionVerificationArgs(version entity.PackageVersion, previousRevision int64) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                  version.ID,
		"package_id":          version.PackageID,
		"verification_status": string(version.VerificationStatus),
		"release_status":      string(version.ReleaseStatus),
		"revision":            version.Revision,
		"updated_at":          version.UpdatedAt,
		"previous_revision":   previousRevision,
	}
}

func manifestSnapshotArgs(snapshot entity.PackageManifestSnapshot) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                 snapshot.ID,
		"package_version_id": snapshot.PackageVersionID,
		"schema_version":     snapshot.SchemaVersion,
		"payload":            objectPayload(snapshot.Payload),
		"validation_status":  string(snapshot.ValidationStatus),
		"validation_errors":  arrayPayload(snapshot.ValidationErrors),
		"created_at":         snapshot.CreatedAt,
	}
}

func pricingMetadataArgs(metadata entity.PackagePricingMetadata) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":            metadata.ID,
		"package_id":    metadata.PackageID,
		"pricing_kind":  string(metadata.Kind),
		"currency":      metadata.Currency,
		"price_payload": objectPayload(metadata.PricePayload),
		"version":       metadata.Version,
		"updated_at":    metadata.UpdatedAt,
	}
}

func pricingMetadataUpdateArgs(metadata entity.PackagePricingMetadata, previousVersion int64) pgx.NamedArgs {
	args := pricingMetadataArgs(metadata)
	args["previous_version"] = previousVersion
	return args
}

func packageInstallationArgs(installation entity.PackageInstallation) pgx.NamedArgs {
	return withVersionedBaseArgs(installation.VersionedBase, pgx.NamedArgs{
		"package_id":                 installation.PackageID,
		"package_version_id":         installation.PackageVersionID,
		"scope_type":                 string(installation.Scope.Type),
		"scope_ref":                  installation.Scope.Ref,
		"installation_status":        string(installation.InstallationStatus),
		"desired_state":              string(installation.DesiredState),
		"runtime_requirement_digest": installation.RuntimeRequirementDigest,
		"secret_binding_status":      string(installation.SecretBindingStatus),
		"last_health_status":         string(installation.LastHealthStatus),
	})
}

func packageInstallationUpdateArgs(installation entity.PackageInstallation, previousVersion int64) pgx.NamedArgs {
	args := packageInstallationArgs(installation)
	args["previous_version"] = previousVersion
	return args
}

func packageSecretSchemaArgs(schema entity.PackageSecretSchema) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                 schema.ID,
		"package_version_id": schema.PackageVersionID,
		"schema_digest":      schema.SchemaDigest,
		"fields":             secretFieldsPayload(schema.Fields),
		"created_at":         schema.CreatedAt,
	}
}

func packageVerificationArgs(verification entity.PackageVerification) pgx.NamedArgs {
	return pgx.NamedArgs{
		"id":                    verification.ID,
		"package_version_id":    verification.PackageVersionID,
		"verification_status":   string(verification.VerificationStatus),
		"verified_by_actor_ref": verification.VerifiedByActorRef,
		"verification_notes":    verification.VerificationNotes,
		"created_at":            verification.CreatedAt,
	}
}

func commandResultArgs(result entity.CommandResult) pgx.NamedArgs {
	return pgx.NamedArgs{
		"key":             result.Key,
		"command_id":      postgreslib.NullableUUID(result.CommandID),
		"idempotency_key": result.IdempotencyKey,
		"operation":       result.Operation,
		"aggregate_type":  string(result.AggregateType),
		"aggregate_id":    result.AggregateID,
		"result_payload":  objectPayload(result.ResultPayload),
		"created_at":      result.CreatedAt,
	}
}

func commandIdentityArgs(identity query.CommandIdentity) pgx.NamedArgs {
	return pgx.NamedArgs{
		"command_id":      postgreslib.NullableUUID(identity.CommandID),
		"idempotency_key": identity.IdempotencyKey,
		"operation":       identity.Operation,
	}
}

func outboxEventArgs(event entity.OutboxEvent) pgx.NamedArgs {
	args := pgx.NamedArgs{"id": event.ID, "event_type": event.EventType}
	args["schema_version"] = event.SchemaVersion
	args["aggregate_type"] = event.AggregateType
	args["aggregate_id"] = event.AggregateID
	args["payload"] = objectPayload(event.Payload)
	args["occurred_at"] = event.OccurredAt
	args["published_at"] = postgreslib.NullableTime(event.PublishedAt)
	return args
}

func packageSourceFilterArgs(filter query.PackageSourceFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"organization_id": postgreslib.NullableUUID(filter.OrganizationID),
		"source_kind":     optionalString(filter.Kind),
		"status":          optionalString(filter.Status),
	})
}

func packageFilterArgs(filter query.PackageFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"source_id":         postgreslib.NullableUUID(filter.SourceID),
		"package_kind":      optionalString(filter.Kind),
		"status":            optionalString(filter.Status),
		"commercial_status": optionalString(filter.CommercialStatus),
		"trust_status":      optionalString(filter.TrustStatus),
		"query":             filter.Query,
	})
}

func packageVersionFilterArgs(filter query.PackageVersionFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"package_id":          filter.PackageID,
		"verification_status": optionalString(filter.VerificationStatus),
		"release_status":      optionalString(filter.ReleaseStatus),
	})
}

func packageInstallationFilterArgs(filter query.PackageInstallationFilter) pageQueryArgs {
	var scopeType any
	var scopeRef any
	if filter.Scope != nil {
		scopeType = string(filter.Scope.Type)
		scopeRef = filter.Scope.Ref
	}
	return withPage(filter.Page, pgx.NamedArgs{
		"scope_type":            scopeType,
		"scope_ref":             scopeRef,
		"package_id":            postgreslib.NullableUUID(filter.PackageID),
		"package_kind":          optionalString(filter.PackageKind),
		"installation_status":   optionalString(filter.InstallationStatus),
		"secret_binding_status": optionalString(filter.SecretBindingStatus),
	})
}

func packageVerificationFilterArgs(filter query.PackageVerificationFilter) pageQueryArgs {
	return withPage(filter.Page, pgx.NamedArgs{
		"package_version_id":  filter.PackageVersionID,
		"verification_status": optionalString(filter.VerificationStatus),
	})
}

func withVersionedBaseArgs(base entity.VersionedBase, args pgx.NamedArgs) pgx.NamedArgs {
	return postgreslib.AddBaseArgs(args, base.ID, base.Version, base.CreatedAt, base.UpdatedAt)
}

func withPage(page value.PageRequest, args pgx.NamedArgs) pageQueryArgs {
	pageSize := page.PageSize
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	offset := decodePageToken(page.PageToken)
	args["limit"] = pageSize + 1
	args["offset"] = offset
	return pageQueryArgs{
		NamedArgs: args,
		PageSize:  pageSize,
		Offset:    offset,
	}
}

func pageResult[T any](items []T, pageSize int32, offset int64) value.PageResult {
	if len(items) <= int(pageSize) {
		return value.PageResult{}
	}
	return value.PageResult{NextPageToken: encodePageToken(offset + int64(pageSize))}
}

func trimPage[T any](items []T, pageSize int32, _ int64) []T {
	if len(items) <= int(pageSize) {
		return items
	}
	return items[:pageSize]
}

func encodePageToken(offset int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(offset, 10)))
}

func decodePageToken(token string) int64 {
	if token == "" {
		return 0
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return 0
	}
	offset, err := strconv.ParseInt(string(decoded), 10, 64)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func optionalString[T ~string](value *T) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func localizedTextPayload(items []value.LocalizedText) string {
	if len(items) == 0 {
		return "[]"
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func secretFieldsPayload(items []value.PackageSecretField) string {
	if len(items) == 0 {
		return "[]"
	}
	payload, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(payload)
}

func objectPayload(payload []byte) string {
	if len(payload) == 0 {
		return "{}"
	}
	return string(payload)
}

func arrayPayload(payload []byte) string {
	if len(payload) == 0 {
		return "[]"
	}
	return string(payload)
}
