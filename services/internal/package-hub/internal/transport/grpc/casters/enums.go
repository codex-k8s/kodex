package casters

import (
	packagesv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/packages/v1"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
)

type enumPair[Proto comparable, Domain ~string] struct {
	Proto  Proto
	Domain Domain
}

var sourceKindMap = []enumPair[packagesv1.PackageSourceKind, enum.PackageSourceKind]{
	{packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_BUILT_IN, enum.PackageSourceKindBuiltIn},
	{packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_STORE_PACKAGE, enum.PackageSourceKindStorePackage},
	{packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_CUSTOM_REPOSITORY, enum.PackageSourceKindCustomRepository},
	{packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_PROXY, enum.PackageSourceKindProxy},
}

var sourceStatusMap = []enumPair[packagesv1.PackageSourceStatus, enum.PackageSourceStatus]{
	{packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_ACTIVE, enum.PackageSourceStatusActive},
	{packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_DISABLED, enum.PackageSourceStatusDisabled},
	{packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_BLOCKED, enum.PackageSourceStatusBlocked},
	{packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_SYNC_FAILED, enum.PackageSourceStatusSyncFailed},
}

var packageKindMap = []enumPair[packagesv1.PackageKind, enum.PackageKind]{
	{packagesv1.PackageKind_PACKAGE_KIND_PLUGIN, enum.PackageKindPlugin},
	{packagesv1.PackageKind_PACKAGE_KIND_GUIDANCE, enum.PackageKindGuidance},
	{packagesv1.PackageKind_PACKAGE_KIND_STORE, enum.PackageKindStore},
	{packagesv1.PackageKind_PACKAGE_KIND_PLATFORM_CONTENT, enum.PackageKindPlatformContent},
}

var packageStatusMap = []enumPair[packagesv1.PackageStatus, enum.PackageStatus]{
	{packagesv1.PackageStatus_PACKAGE_STATUS_AVAILABLE, enum.PackageStatusAvailable},
	{packagesv1.PackageStatus_PACKAGE_STATUS_HIDDEN, enum.PackageStatusHidden},
	{packagesv1.PackageStatus_PACKAGE_STATUS_REVOKED, enum.PackageStatusRevoked},
	{packagesv1.PackageStatus_PACKAGE_STATUS_BLOCKED, enum.PackageStatusBlocked},
}

var commercialStatusMap = []enumPair[packagesv1.PackageCommercialStatus, enum.PackageCommercialStatus]{
	{packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_FREE, enum.PackageCommercialStatusFree},
	{packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_PAID, enum.PackageCommercialStatusPaid},
	{packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_RESTRICTED, enum.PackageCommercialStatusRestricted},
	{packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_UNKNOWN, enum.PackageCommercialStatusUnknown},
}

var trustStatusMap = []enumPair[packagesv1.PackageTrustStatus, enum.PackageTrustStatus]{
	{packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_BUILT_IN, enum.PackageTrustStatusBuiltIn},
	{packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_VERIFIED, enum.PackageTrustStatusVerified},
	{packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_UNVERIFIED, enum.PackageTrustStatusUnverified},
	{packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_BLOCKED, enum.PackageTrustStatusBlocked},
}

var sourceRefKindMap = []enumPair[packagesv1.PackageVersionSourceRefKind, enum.PackageVersionSourceRefKind]{
	{packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_GIT_TAG, enum.PackageVersionSourceRefKindGitTag},
	{packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_GIT_COMMIT, enum.PackageVersionSourceRefKindGitCommit},
	{packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_GITLINK, enum.PackageVersionSourceRefKindGitlink},
	{packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_PROXY_REF, enum.PackageVersionSourceRefKindProxyRef},
}

var verificationStatusMap = []enumPair[packagesv1.PackageVerificationStatus, enum.PackageVerificationStatus]{
	{packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_VERIFIED, enum.PackageVerificationStatusVerified},
	{packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_UNVERIFIED, enum.PackageVerificationStatusUnverified},
	{packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_REJECTED, enum.PackageVerificationStatusRejected},
	{packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_REVOKED, enum.PackageVerificationStatusRevoked},
}

var releaseStatusMap = []enumPair[packagesv1.PackageReleaseStatus, enum.PackageReleaseStatus]{
	{packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_ACTIVE, enum.PackageReleaseStatusActive},
	{packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_DEPRECATED, enum.PackageReleaseStatusDeprecated},
	{packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_REVOKED, enum.PackageReleaseStatusRevoked},
	{packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_BLOCKED, enum.PackageReleaseStatusBlocked},
}

var manifestValidationStatusMap = []enumPair[packagesv1.PackageManifestValidationStatus, enum.PackageManifestValidationStatus]{
	{packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_VALID, enum.PackageManifestValidationStatusValid},
	{packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_INVALID, enum.PackageManifestValidationStatusInvalid},
	{packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_WARNING, enum.PackageManifestValidationStatusWarning},
}

func SourceKindFromProto(value packagesv1.PackageSourceKind) (enum.PackageSourceKind, error) {
	return domainEnum(value, sourceKindMap)
}

func SourceKindToProto(value enum.PackageSourceKind) packagesv1.PackageSourceKind {
	return protoEnum(value, sourceKindMap, packagesv1.PackageSourceKind_PACKAGE_SOURCE_KIND_UNSPECIFIED)
}

func SourceStatusFromProto(value packagesv1.PackageSourceStatus) (enum.PackageSourceStatus, error) {
	return domainEnum(value, sourceStatusMap)
}

func SourceStatusToProto(value enum.PackageSourceStatus) packagesv1.PackageSourceStatus {
	return protoEnum(value, sourceStatusMap, packagesv1.PackageSourceStatus_PACKAGE_SOURCE_STATUS_UNSPECIFIED)
}

func PackageKindFromProto(value packagesv1.PackageKind) (enum.PackageKind, error) {
	return domainEnum(value, packageKindMap)
}

func PackageKindToProto(value enum.PackageKind) packagesv1.PackageKind {
	return protoEnum(value, packageKindMap, packagesv1.PackageKind_PACKAGE_KIND_UNSPECIFIED)
}

func PackageStatusFromProto(value packagesv1.PackageStatus) (enum.PackageStatus, error) {
	return domainEnum(value, packageStatusMap)
}

func PackageStatusToProto(value enum.PackageStatus) packagesv1.PackageStatus {
	return protoEnum(value, packageStatusMap, packagesv1.PackageStatus_PACKAGE_STATUS_UNSPECIFIED)
}

func CommercialStatusFromProto(value packagesv1.PackageCommercialStatus) (enum.PackageCommercialStatus, error) {
	return domainEnum(value, commercialStatusMap)
}

func CommercialStatusToProto(value enum.PackageCommercialStatus) packagesv1.PackageCommercialStatus {
	return protoEnum(value, commercialStatusMap, packagesv1.PackageCommercialStatus_PACKAGE_COMMERCIAL_STATUS_UNSPECIFIED)
}

func TrustStatusFromProto(value packagesv1.PackageTrustStatus) (enum.PackageTrustStatus, error) {
	return domainEnum(value, trustStatusMap)
}

func TrustStatusToProto(value enum.PackageTrustStatus) packagesv1.PackageTrustStatus {
	return protoEnum(value, trustStatusMap, packagesv1.PackageTrustStatus_PACKAGE_TRUST_STATUS_UNSPECIFIED)
}

func SourceRefKindToProto(value enum.PackageVersionSourceRefKind) packagesv1.PackageVersionSourceRefKind {
	return protoEnum(value, sourceRefKindMap, packagesv1.PackageVersionSourceRefKind_PACKAGE_VERSION_SOURCE_REF_KIND_UNSPECIFIED)
}

func SourceRefKindFromProto(value packagesv1.PackageVersionSourceRefKind) (enum.PackageVersionSourceRefKind, error) {
	return domainEnum(value, sourceRefKindMap)
}

func VerificationStatusFromProto(value packagesv1.PackageVerificationStatus) (enum.PackageVerificationStatus, error) {
	return domainEnum(value, verificationStatusMap)
}

func VerificationStatusToProto(value enum.PackageVerificationStatus) packagesv1.PackageVerificationStatus {
	return protoEnum(value, verificationStatusMap, packagesv1.PackageVerificationStatus_PACKAGE_VERIFICATION_STATUS_UNSPECIFIED)
}

func ReleaseStatusFromProto(value packagesv1.PackageReleaseStatus) (enum.PackageReleaseStatus, error) {
	return domainEnum(value, releaseStatusMap)
}

func ReleaseStatusToProto(value enum.PackageReleaseStatus) packagesv1.PackageReleaseStatus {
	return protoEnum(value, releaseStatusMap, packagesv1.PackageReleaseStatus_PACKAGE_RELEASE_STATUS_UNSPECIFIED)
}

func ManifestValidationStatusToProto(value enum.PackageManifestValidationStatus) packagesv1.PackageManifestValidationStatus {
	return protoEnum(value, manifestValidationStatusMap, packagesv1.PackageManifestValidationStatus_PACKAGE_MANIFEST_VALIDATION_STATUS_UNSPECIFIED)
}

func optionalPackageKind(value *packagesv1.PackageKind) (*enum.PackageKind, error) {
	return optionalEnum(value, PackageKindFromProto)
}

func optionalPackageStatus(value *packagesv1.PackageStatus) (*enum.PackageStatus, error) {
	return optionalEnum(value, PackageStatusFromProto)
}

func optionalCommercialStatus(value *packagesv1.PackageCommercialStatus) (*enum.PackageCommercialStatus, error) {
	return optionalEnum(value, CommercialStatusFromProto)
}

func optionalTrustStatus(value *packagesv1.PackageTrustStatus) (*enum.PackageTrustStatus, error) {
	return optionalEnum(value, TrustStatusFromProto)
}

func optionalVerificationStatus(value *packagesv1.PackageVerificationStatus) (*enum.PackageVerificationStatus, error) {
	return optionalEnum(value, VerificationStatusFromProto)
}

func optionalReleaseStatus(value *packagesv1.PackageReleaseStatus) (*enum.PackageReleaseStatus, error) {
	return optionalEnum(value, ReleaseStatusFromProto)
}

func optionalSourceKind(value *packagesv1.PackageSourceKind) (*enum.PackageSourceKind, error) {
	return optionalEnum(value, SourceKindFromProto)
}

func optionalSourceStatus(value *packagesv1.PackageSourceStatus) (*enum.PackageSourceStatus, error) {
	return optionalEnum(value, SourceStatusFromProto)
}

func domainEnum[Proto comparable, Domain ~string](value Proto, pairs []enumPair[Proto, Domain]) (Domain, error) {
	for _, pair := range pairs {
		if pair.Proto == value {
			return pair.Domain, nil
		}
	}
	var zero Domain
	return zero, errs.ErrInvalidArgument
}

func protoEnum[Proto comparable, Domain ~string](value Domain, pairs []enumPair[Proto, Domain], fallback Proto) Proto {
	for _, pair := range pairs {
		if pair.Domain == value {
			return pair.Proto
		}
	}
	return fallback
}

func optionalEnum[Proto comparable, Domain ~string](value *Proto, convert func(Proto) (Domain, error)) (*Domain, error) {
	if value == nil {
		return nil, nil
	}
	converted, err := convert(*value)
	if err != nil {
		return nil, err
	}
	return &converted, nil
}
