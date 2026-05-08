package casters

import (
	"strings"
	"time"

	"github.com/google/uuid"

	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

type IDQueryInput struct {
	ID   uuid.UUID
	Meta value.QueryMeta
}

func ConnectPackageSourceInput(request *packagesv1.ConnectPackageSourceRequest) (service.ConnectPackageSourceInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.ConnectPackageSourceInput{}, err
	}
	organizationID, err := optionalUUIDPtr(request.GetOrganizationId())
	if err != nil {
		return service.ConnectPackageSourceInput{}, err
	}
	kind, err := SourceKindFromProto(request.GetSourceKind())
	if err != nil {
		return service.ConnectPackageSourceInput{}, err
	}
	return service.ConnectPackageSourceInput{
		OrganizationID:     organizationID,
		Slug:               strings.TrimSpace(request.GetSlug()),
		DisplayName:        strings.TrimSpace(request.GetDisplayName()),
		Kind:               kind,
		RepositoryRef:      strings.TrimSpace(request.GetRepositoryRef()),
		CatalogEndpointRef: strings.TrimSpace(request.GetCatalogEndpointRef()),
		Meta:               meta,
	}, nil
}

func UpdatePackageSourceInput(request *packagesv1.UpdatePackageSourceRequest) (service.UpdatePackageSourceInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.UpdatePackageSourceInput{}, err
	}
	sourceID, err := requiredUUID(request.GetSourceId())
	if err != nil {
		return service.UpdatePackageSourceInput{}, err
	}
	status, err := optionalSourceStatus(request.Status)
	if err != nil {
		return service.UpdatePackageSourceInput{}, err
	}
	return service.UpdatePackageSourceInput{
		SourceID:           sourceID,
		DisplayName:        optionalPresentString(request.DisplayName),
		RepositoryRef:      optionalPresentString(request.RepositoryRef),
		CatalogEndpointRef: optionalPresentString(request.CatalogEndpointRef),
		Status:             status,
		Meta:               meta,
	}, nil
}

func DisablePackageSourceInput(request *packagesv1.DisablePackageSourceRequest) (service.DisablePackageSourceInput, error) {
	return sourceCommandInput(request.GetSourceId(), request.GetMeta(), func(sourceID uuid.UUID, meta value.CommandMeta) service.DisablePackageSourceInput {
		return service.DisablePackageSourceInput{SourceID: sourceID, Meta: meta}
	})
}

func GetPackageSourceInput(request *packagesv1.GetPackageSourceRequest) (IDQueryInput, error) {
	return queryByIDInput(request.GetSourceId(), request.GetMeta())
}

func ListPackageSourcesInput(request *packagesv1.ListPackageSourcesRequest) (service.ListPackageSourcesInput, error) {
	var input service.ListPackageSourcesInput
	if err := setQueryMeta(&input.Meta, request.GetMeta()); err != nil {
		return input, err
	}
	organizationID, err := optionalUUIDPtr(request.GetOrganizationId())
	if err != nil {
		return input, err
	}
	input.OrganizationID = organizationID
	kind, err := optionalSourceKind(request.SourceKind)
	if err != nil {
		return input, err
	}
	input.Kind = kind
	status, err := optionalSourceStatus(request.Status)
	if err != nil {
		return input, err
	}
	input.Status = status
	input.Page = pageRequestFromProto(request.GetPage())
	return input, nil
}

func SyncAvailablePackagesInput(request *packagesv1.SyncAvailablePackagesRequest) (service.SyncAvailablePackagesInput, error) {
	input, err := sourceCommandInput(request.GetSourceId(), request.GetMeta(), func(sourceID uuid.UUID, meta value.CommandMeta) service.SyncAvailablePackagesInput {
		return service.SyncAvailablePackagesInput{SourceID: sourceID, Meta: meta}
	})
	if err != nil {
		return service.SyncAvailablePackagesInput{}, err
	}
	snapshot, err := catalogSnapshotFromProto(request.GetSnapshot())
	if err != nil {
		return service.SyncAvailablePackagesInput{}, err
	}
	input.Snapshot = snapshot
	return input, nil
}

func GetPackageInput(request *packagesv1.GetPackageRequest) (IDQueryInput, error) {
	return queryByIDInput(request.GetPackageId(), request.GetMeta())
}

func ListPackagesInput(request *packagesv1.ListPackagesRequest) (service.ListPackagesInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	sourceID, err := optionalUUIDPtr(request.GetSourceId())
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	kind, err := optionalPackageKind(request.PackageKind)
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	status, err := optionalPackageStatus(request.Status)
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	commercialStatus, err := optionalCommercialStatus(request.CommercialStatus)
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	trustStatus, err := optionalTrustStatus(request.TrustStatus)
	if err != nil {
		return service.ListPackagesInput{}, err
	}
	return service.ListPackagesInput{
		SourceID:         sourceID,
		Kind:             kind,
		Status:           status,
		CommercialStatus: commercialStatus,
		TrustStatus:      trustStatus,
		Query:            strings.TrimSpace(request.GetQuery()),
		Page:             pageRequestFromProto(request.GetPage()),
		Meta:             meta,
	}, nil
}

func GetPackageVersionInput(request *packagesv1.GetPackageVersionRequest) (IDQueryInput, error) {
	return queryByIDInput(request.GetPackageVersionId(), request.GetMeta())
}

func ListPackageVersionsInput(request *packagesv1.ListPackageVersionsRequest) (service.ListPackageVersionsInput, error) {
	meta, err := QueryMetaFromProto(request.GetMeta())
	if err != nil {
		return service.ListPackageVersionsInput{}, err
	}
	packageID, err := requiredUUID(request.GetPackageId())
	if err != nil {
		return service.ListPackageVersionsInput{}, err
	}
	verificationStatus, err := optionalVerificationStatus(request.VerificationStatus)
	if err != nil {
		return service.ListPackageVersionsInput{}, err
	}
	releaseStatus, err := optionalReleaseStatus(request.ReleaseStatus)
	if err != nil {
		return service.ListPackageVersionsInput{}, err
	}
	return service.ListPackageVersionsInput{PackageID: packageID, VerificationStatus: verificationStatus, ReleaseStatus: releaseStatus, Page: pageRequestFromProto(request.GetPage()), Meta: meta}, nil
}

func GetPackageManifestInput(request *packagesv1.GetPackageManifestRequest) (IDQueryInput, error) {
	return queryByIDInput(request.GetPackageVersionId(), request.GetMeta())
}

func SetPackageVerificationInput(request *packagesv1.SetPackageVerificationRequest) (service.SetPackageVerificationInput, error) {
	meta, err := CommandMetaFromProto(request.GetMeta())
	if err != nil {
		return service.SetPackageVerificationInput{}, err
	}
	versionID, err := requiredUUID(request.GetPackageVersionId())
	if err != nil {
		return service.SetPackageVerificationInput{}, err
	}
	verificationStatus, err := VerificationStatusFromProto(request.GetVerificationStatus())
	if err != nil {
		return service.SetPackageVerificationInput{}, err
	}
	releaseStatus, err := optionalReleaseStatus(request.ReleaseStatus)
	if err != nil {
		return service.SetPackageVerificationInput{}, err
	}
	return service.SetPackageVerificationInput{
		PackageVersionID:   versionID,
		VerificationStatus: verificationStatus,
		VerificationNotes:  strings.TrimSpace(request.GetVerificationNotes()),
		ReleaseStatus:      releaseStatus,
		Meta:               meta,
	}, nil
}

func queryByIDInput(idText string, metaMessage *packagesv1.QueryMeta) (IDQueryInput, error) {
	id, err := requiredUUID(idText)
	if err != nil {
		return IDQueryInput{}, err
	}
	meta, err := QueryMetaFromProto(metaMessage)
	if err != nil {
		return IDQueryInput{}, err
	}
	return IDQueryInput{ID: id, Meta: meta}, nil
}

func sourceCommandInput[T any](idText string, metaMessage *packagesv1.CommandMeta, build func(uuid.UUID, value.CommandMeta) T) (T, error) {
	sourceID, err := requiredUUID(idText)
	if err != nil {
		var zero T
		return zero, err
	}
	meta, err := CommandMetaFromProto(metaMessage)
	if err != nil {
		var zero T
		return zero, err
	}
	return build(sourceID, meta), nil
}

func setQueryMeta(target *value.QueryMeta, message *packagesv1.QueryMeta) error {
	meta, err := QueryMetaFromProto(message)
	if err != nil {
		return err
	}
	*target = meta
	return nil
}

func catalogSnapshotFromProto(snapshot *packagesv1.CatalogSnapshot) (service.CatalogSnapshot, error) {
	if snapshot == nil {
		return service.CatalogSnapshot{}, nil
	}
	observedAt, err := optionalTimeValue(snapshot.GetObservedAt())
	if err != nil {
		return service.CatalogSnapshot{}, err
	}
	result := service.CatalogSnapshot{ObservedAt: observedAt}
	for _, item := range snapshot.GetPackages() {
		converted, err := catalogPackageFromProto(item)
		if err != nil {
			return service.CatalogSnapshot{}, err
		}
		result.Packages = append(result.Packages, converted)
	}
	return result, nil
}

func catalogPackageFromProto(item *packagesv1.CatalogPackageSnapshot) (service.CatalogPackageSnapshot, error) {
	if item == nil {
		return service.CatalogPackageSnapshot{}, errs.ErrInvalidArgument
	}
	kind, err := PackageKindFromProto(item.GetPackageKind())
	if err != nil {
		return service.CatalogPackageSnapshot{}, err
	}
	commercialStatus, err := CommercialStatusFromProto(item.GetCommercialStatus())
	if err != nil {
		return service.CatalogPackageSnapshot{}, err
	}
	trustStatus, err := TrustStatusFromProto(item.GetTrustStatus())
	if err != nil {
		return service.CatalogPackageSnapshot{}, err
	}
	status, err := PackageStatusFromProto(item.GetStatus())
	if err != nil {
		return service.CatalogPackageSnapshot{}, err
	}
	result := service.CatalogPackageSnapshot{
		Slug:             strings.TrimSpace(item.GetSlug()),
		Kind:             kind,
		PublisherRef:     strings.TrimSpace(item.GetPublisherRef()),
		DisplayName:      localizedTextFromProto(item.GetDisplayName()),
		Description:      localizedTextFromProto(item.GetDescription()),
		IconObjectURI:    strings.TrimSpace(item.GetIconObjectUri()),
		CommercialStatus: commercialStatus,
		TrustStatus:      trustStatus,
		Status:           status,
	}
	for _, version := range item.GetVersions() {
		converted, err := catalogVersionFromProto(version)
		if err != nil {
			return service.CatalogPackageSnapshot{}, err
		}
		result.Versions = append(result.Versions, converted)
	}
	return result, nil
}

func catalogVersionFromProto(item *packagesv1.CatalogVersionSnapshot) (service.CatalogVersionSnapshot, error) {
	if item == nil || item.GetSourceRef() == nil {
		return service.CatalogVersionSnapshot{}, errs.ErrInvalidArgument
	}
	sourceRef, err := sourceRefFromProto(item.GetSourceRef())
	if err != nil {
		return service.CatalogVersionSnapshot{}, err
	}
	verificationStatus := enum.PackageVerificationStatusUnverified
	if item.VerificationStatus != nil {
		verificationStatus, err = VerificationStatusFromProto(item.GetVerificationStatus())
		if err != nil {
			return service.CatalogVersionSnapshot{}, err
		}
	}
	releaseStatus := enum.PackageReleaseStatusActive
	if item.ReleaseStatus != nil {
		releaseStatus, err = ReleaseStatusFromProto(item.GetReleaseStatus())
		if err != nil {
			return service.CatalogVersionSnapshot{}, err
		}
	}
	publishedAt, err := optionalTimeValue(item.GetPublishedAt())
	if err != nil {
		return service.CatalogVersionSnapshot{}, err
	}
	schemaVersion := int32(1)
	if item.ManifestSchemaVersion != nil {
		schemaVersion = item.GetManifestSchemaVersion()
	}
	return service.CatalogVersionSnapshot{
		VersionLabel:       strings.TrimSpace(item.GetVersionLabel()),
		SourceRef:          sourceRef,
		ManifestDigest:     strings.TrimSpace(item.GetManifestDigest()),
		ManifestSchema:     schemaVersion,
		ManifestPayload:    []byte(strings.TrimSpace(item.GetManifestPayloadJson())),
		VerificationStatus: verificationStatus,
		ReleaseStatus:      releaseStatus,
		PublishedAt:        publishedAt,
	}, nil
}

func sourceRefFromProto(ref *packagesv1.SourceRef) (value.SourceRef, error) {
	kind, err := SourceRefKindFromProto(ref.GetKind())
	if err != nil {
		return value.SourceRef{}, err
	}
	return value.SourceRef{Kind: kind, Ref: strings.TrimSpace(ref.GetRef()), CommitSHA: strings.TrimSpace(ref.GetCommitSha())}, nil
}

func localizedTextFromProto(items []*packagesv1.LocalizedText) []value.LocalizedText {
	result := make([]value.LocalizedText, len(items))
	for index, item := range items {
		if item != nil {
			result[index] = value.LocalizedText{Locale: strings.TrimSpace(item.GetLocale()), Text: strings.TrimSpace(item.GetText())}
		}
	}
	return result
}

func optionalTimeValue(text string) (*time.Time, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return &parsed, nil
}
