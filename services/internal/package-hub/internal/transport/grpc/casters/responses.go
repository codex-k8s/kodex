package casters

import (
	"time"

	"github.com/google/uuid"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func PackageSourceResponse(source entity.PackageSource) *packagesv1.PackageSourceResponse {
	return &packagesv1.PackageSourceResponse{Source: PackageSource(source)}
}

func ListPackageSourcesResponse(result service.ListPackageSourcesResult) *packagesv1.ListPackageSourcesResponse {
	return &packagesv1.ListPackageSourcesResponse{Items: mapProto(result.Sources, PackageSource), Page: pageResponseToProto(result.Page)}
}

func SyncAvailablePackagesResponse(result service.SyncAvailablePackagesResult) *packagesv1.SyncAvailablePackagesResponse {
	return &packagesv1.SyncAvailablePackagesResponse{
		Source:       PackageSource(result.Source),
		PackageCount: int32(result.PackageCount),
		VersionCount: int32(result.VersionCount),
		SyncedAt:     formatTime(result.SyncedAt),
	}
}

func PackageResponse(entry entity.PackageEntry) *packagesv1.PackageResponse {
	return &packagesv1.PackageResponse{PackageEntry: PackageEntry(entry)}
}

func ListPackagesResponse(result service.ListPackagesResult) *packagesv1.ListPackagesResponse {
	return &packagesv1.ListPackagesResponse{Items: mapProto(result.Packages, PackageEntry), Page: pageResponseToProto(result.Page)}
}

func PackageVersionResponse(version entity.PackageVersion) *packagesv1.PackageVersionResponse {
	return &packagesv1.PackageVersionResponse{Version: PackageVersion(version)}
}

func ListPackageVersionsResponse(result service.ListPackageVersionsResult) *packagesv1.ListPackageVersionsResponse {
	return &packagesv1.ListPackageVersionsResponse{Items: mapProto(result.Versions, PackageVersion), Page: pageResponseToProto(result.Page)}
}

func PackageManifestResponse(snapshot entity.PackageManifestSnapshot) *packagesv1.PackageManifestResponse {
	return &packagesv1.PackageManifestResponse{Manifest: PackageManifestSnapshot(snapshot)}
}

func PackageVerificationResponse(result service.SetPackageVerificationResult) *packagesv1.PackageVerificationResponse {
	return &packagesv1.PackageVerificationResponse{Verification: PackageVerification(result.Verification), Version: PackageVersion(result.Version)}
}

func PackageSource(source entity.PackageSource) *packagesv1.PackageSource {
	return &packagesv1.PackageSource{
		Id:                 source.ID.String(),
		OrganizationId:     optionalUUIDString(source.OrganizationID),
		Slug:               source.Slug,
		DisplayName:        source.DisplayName,
		SourceKind:         SourceKindToProto(source.Kind),
		RepositoryRef:      optionalStringPtr(source.RepositoryRef),
		CatalogEndpointRef: optionalStringPtr(source.CatalogEndpointRef),
		Status:             SourceStatusToProto(source.Status),
		LastSyncAt:         optionalTimeString(source.LastSyncAt),
		LastError:          optionalStringPtr(source.LastError),
		Version:            source.Version,
		CreatedAt:          formatTime(source.CreatedAt),
		UpdatedAt:          formatTime(source.UpdatedAt),
	}
}

func PackageEntry(entry entity.PackageEntry) *packagesv1.PackageEntry {
	return &packagesv1.PackageEntry{
		Id:               entry.ID.String(),
		SourceId:         optionalUUIDString(entry.SourceID),
		Slug:             entry.Slug,
		PackageKind:      PackageKindToProto(entry.Kind),
		PublisherRef:     optionalStringPtr(entry.PublisherRef),
		DisplayName:      localizedTextToProto(entry.DisplayName),
		Description:      localizedTextToProto(entry.Description),
		IconObjectUri:    optionalStringPtr(entry.IconObjectURI),
		CommercialStatus: CommercialStatusToProto(entry.CommercialStatus),
		TrustStatus:      TrustStatusToProto(entry.TrustStatus),
		Status:           PackageStatusToProto(entry.Status),
		Version:          entry.Version,
		CreatedAt:        formatTime(entry.CreatedAt),
		UpdatedAt:        formatTime(entry.UpdatedAt),
	}
}

func PackageVersion(version entity.PackageVersion) *packagesv1.PackageVersion {
	return &packagesv1.PackageVersion{
		Id:                 version.ID.String(),
		PackageId:          version.PackageID.String(),
		VersionLabel:       version.VersionLabel,
		SourceRef:          SourceRef(version.SourceRef),
		ManifestDigest:     version.ManifestDigest,
		VerificationStatus: VerificationStatusToProto(version.VerificationStatus),
		ReleaseStatus:      ReleaseStatusToProto(version.ReleaseStatus),
		Revision:           version.Revision,
		PublishedAt:        optionalTimeString(version.PublishedAt),
		CreatedAt:          formatTime(version.CreatedAt),
		UpdatedAt:          formatTime(version.UpdatedAt),
	}
}

func SourceRef(ref value.SourceRef) *packagesv1.SourceRef {
	return &packagesv1.SourceRef{
		Kind:      SourceRefKindToProto(ref.Kind),
		Ref:       ref.Ref,
		CommitSha: optionalStringPtr(ref.CommitSHA),
	}
}

func PackageManifestSnapshot(snapshot entity.PackageManifestSnapshot) *packagesv1.PackageManifestSnapshot {
	return &packagesv1.PackageManifestSnapshot{
		Id:                   snapshot.ID.String(),
		PackageVersionId:     snapshot.PackageVersionID.String(),
		SchemaVersion:        snapshot.SchemaVersion,
		PayloadJson:          string(snapshot.Payload),
		ValidationStatus:     ManifestValidationStatusToProto(snapshot.ValidationStatus),
		ValidationErrorsJson: string(snapshot.ValidationErrors),
		CreatedAt:            formatTime(snapshot.CreatedAt),
	}
}

func PackageVerification(verification entity.PackageVerification) *packagesv1.PackageVerification {
	return &packagesv1.PackageVerification{
		Id:                 verification.ID.String(),
		PackageVersionId:   verification.PackageVersionID.String(),
		VerificationStatus: VerificationStatusToProto(verification.VerificationStatus),
		VerifiedByActorRef: optionalStringPtr(verification.VerifiedByActorRef),
		VerificationNotes:  optionalStringPtr(verification.VerificationNotes),
		CreatedAt:          formatTime(verification.CreatedAt),
	}
}

func localizedTextToProto(items []value.LocalizedText) []*packagesv1.LocalizedText {
	return mapProto(items, func(item value.LocalizedText) *packagesv1.LocalizedText {
		return &packagesv1.LocalizedText{Locale: item.Locale, Text: item.Text}
	})
}

func mapProto[Source any, Target any](items []Source, cast func(Source) *Target) []*Target {
	result := make([]*Target, len(items))
	for index := range items {
		result[index] = cast(items[index])
	}
	return result
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func optionalTimeString(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := formatTime(*value)
	return &formatted
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}
