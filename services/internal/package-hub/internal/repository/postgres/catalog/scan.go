package catalog

import (
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func scanPackageSource(row postgreslib.RowScanner) (entity.PackageSource, error) {
	var source entity.PackageSource
	var organizationID pgtype.UUID
	var lastSyncAt pgtype.Timestamptz
	var kind, status string
	err := row.Scan(
		&source.ID,
		&organizationID,
		&source.Slug,
		&source.DisplayName,
		&kind,
		&source.RepositoryRef,
		&source.CatalogEndpointRef,
		&status,
		&lastSyncAt,
		&source.LastError,
		&source.Version,
		&source.CreatedAt,
		&source.UpdatedAt,
	)
	source.OrganizationID = postgreslib.UUIDPtrFromPG(organizationID)
	source.LastSyncAt = postgreslib.TimePtrFromPG(lastSyncAt)
	source.Kind = enum.PackageSourceKind(kind)
	source.Status = enum.PackageSourceStatus(status)
	return source, err
}

func scanPackage(row postgreslib.RowScanner) (entity.PackageEntry, error) {
	var entry entity.PackageEntry
	var sourceID pgtype.UUID
	var displayName, description []byte
	var kind, commercialStatus, trustStatus, status string
	err := row.Scan(
		&entry.ID,
		&sourceID,
		&entry.Slug,
		&kind,
		&entry.PublisherRef,
		&displayName,
		&description,
		&entry.IconObjectURI,
		&commercialStatus,
		&trustStatus,
		&status,
		&entry.Version,
		&entry.CreatedAt,
		&entry.UpdatedAt,
	)
	entry.SourceID = postgreslib.UUIDPtrFromPG(sourceID)
	entry.Kind = enum.PackageKind(kind)
	entry.CommercialStatus = enum.PackageCommercialStatus(commercialStatus)
	entry.TrustStatus = enum.PackageTrustStatus(trustStatus)
	entry.Status = enum.PackageStatus(status)
	if err != nil {
		return entry, err
	}
	entry.DisplayName, err = localizedTextFromPayload(displayName)
	if err != nil {
		return entry, fmt.Errorf("scan package display_name: %w", err)
	}
	entry.Description, err = localizedTextFromPayload(description)
	if err != nil {
		return entry, fmt.Errorf("scan package description: %w", err)
	}
	return entry, err
}

func scanPackageVersion(row postgreslib.RowScanner) (entity.PackageVersion, error) {
	var version entity.PackageVersion
	var publishedAt pgtype.Timestamptz
	var sourceRefKind, verificationStatus, releaseStatus string
	err := row.Scan(
		&version.ID,
		&version.PackageID,
		&version.VersionLabel,
		&sourceRefKind,
		&version.SourceRef.Ref,
		&version.SourceRef.CommitSHA,
		&version.ManifestDigest,
		&verificationStatus,
		&releaseStatus,
		&version.Revision,
		&publishedAt,
		&version.CreatedAt,
		&version.UpdatedAt,
	)
	version.SourceRef.Kind = enum.PackageVersionSourceRefKind(sourceRefKind)
	version.VerificationStatus = enum.PackageVerificationStatus(verificationStatus)
	version.ReleaseStatus = enum.PackageReleaseStatus(releaseStatus)
	version.PublishedAt = postgreslib.TimePtrFromPG(publishedAt)
	return version, err
}

func scanManifestSnapshot(row postgreslib.RowScanner) (entity.PackageManifestSnapshot, error) {
	var snapshot entity.PackageManifestSnapshot
	var payload, validationErrors []byte
	var validationStatus string
	err := row.Scan(
		&snapshot.ID,
		&snapshot.PackageVersionID,
		&snapshot.SchemaVersion,
		&payload,
		&validationStatus,
		&validationErrors,
		&snapshot.CreatedAt,
	)
	snapshot.Payload = append(snapshot.Payload[:0], payload...)
	snapshot.ValidationStatus = enum.PackageManifestValidationStatus(validationStatus)
	snapshot.ValidationErrors = append(snapshot.ValidationErrors[:0], validationErrors...)
	return snapshot, err
}

func scanPricingMetadata(row postgreslib.RowScanner) (entity.PackagePricingMetadata, error) {
	var metadata entity.PackagePricingMetadata
	var pricePayload []byte
	var kind string
	err := row.Scan(
		&metadata.ID,
		&metadata.PackageID,
		&kind,
		&metadata.Currency,
		&pricePayload,
		&metadata.Version,
		&metadata.UpdatedAt,
	)
	metadata.Kind = enum.PackagePricingKind(kind)
	metadata.PricePayload = append(metadata.PricePayload[:0], pricePayload...)
	return metadata, err
}

func scanPackageInstallation(row postgreslib.RowScanner) (entity.PackageInstallation, error) {
	var installation entity.PackageInstallation
	var scopeType, installationStatus, desiredState, secretBindingStatus, lastHealthStatus string
	err := row.Scan(
		&installation.ID,
		&installation.PackageID,
		&installation.PackageVersionID,
		&scopeType,
		&installation.Scope.Ref,
		&installationStatus,
		&desiredState,
		&installation.RuntimeRequirementDigest,
		&secretBindingStatus,
		&lastHealthStatus,
		&installation.Version,
		&installation.CreatedAt,
		&installation.UpdatedAt,
	)
	installation.Scope.Type = enum.PackageInstallationScopeType(scopeType)
	installation.InstallationStatus = enum.PackageInstallationStatus(installationStatus)
	installation.DesiredState = enum.PackageDesiredState(desiredState)
	installation.SecretBindingStatus = enum.PackageSecretBindingStatus(secretBindingStatus)
	installation.LastHealthStatus = enum.PackageHealthStatus(lastHealthStatus)
	return installation, err
}

func scanPackageSecretSchema(row postgreslib.RowScanner) (entity.PackageSecretSchema, error) {
	var schema entity.PackageSecretSchema
	var fields []byte
	err := row.Scan(
		&schema.ID,
		&schema.PackageVersionID,
		&schema.SchemaDigest,
		&fields,
		&schema.CreatedAt,
	)
	if err != nil {
		return schema, err
	}
	schema.Fields, err = secretFieldsFromPayload(fields)
	if err != nil {
		return schema, fmt.Errorf("scan package secret schema fields: %w", err)
	}
	return schema, nil
}

func scanPackageVerification(row postgreslib.RowScanner) (entity.PackageVerification, error) {
	var verification entity.PackageVerification
	var status string
	err := row.Scan(
		&verification.ID,
		&verification.PackageVersionID,
		&status,
		&verification.VerifiedByActorRef,
		&verification.VerificationNotes,
		&verification.CreatedAt,
	)
	verification.VerificationStatus = enum.PackageVerificationStatus(status)
	return verification, err
}

func scanCommandResult(row postgreslib.RowScanner) (entity.CommandResult, error) {
	var result entity.CommandResult
	var commandID pgtype.UUID
	var aggregateType string
	var resultPayload []byte
	err := row.Scan(
		&result.Key,
		&commandID,
		&result.IdempotencyKey,
		&result.Operation,
		&aggregateType,
		&result.AggregateID,
		&resultPayload,
		&result.CreatedAt,
	)
	result.CommandID = postgreslib.UUIDPtrFromPG(commandID)
	result.AggregateType = enum.CommandAggregateType(aggregateType)
	result.ResultPayload = append(result.ResultPayload[:0], resultPayload...)
	return result, err
}

func localizedTextFromPayload(payload []byte) ([]value.LocalizedText, error) {
	return decodeJSONPayload[[]value.LocalizedText](payload)
}

func secretFieldsFromPayload(payload []byte) ([]value.PackageSecretField, error) {
	return decodeJSONPayload[[]value.PackageSecretField](payload)
}

func decodeJSONPayload[T any](payload []byte) (T, error) {
	var value T
	if len(payload) == 0 {
		return value, nil
	}
	err := json.Unmarshal(payload, &value)
	return value, err
}
