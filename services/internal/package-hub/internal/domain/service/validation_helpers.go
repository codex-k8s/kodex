package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/enum"
)

func requireID(id uuid.UUID) error {
	if id == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func requireOptionalID(id *uuid.UUID) error {
	if id != nil && *id == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func requireText(value string) error {
	if strings.TrimSpace(value) == "" {
		return errs.ErrInvalidArgument
	}
	return nil
}

func requireSourceKind(kind enum.PackageSourceKind) error {
	switch kind {
	case enum.PackageSourceKindBuiltIn, enum.PackageSourceKindStorePackage, enum.PackageSourceKindCustomRepository, enum.PackageSourceKindProxy:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireSourceStatus(status enum.PackageSourceStatus) error {
	switch status {
	case enum.PackageSourceStatusActive, enum.PackageSourceStatusDisabled, enum.PackageSourceStatusBlocked, enum.PackageSourceStatusSyncFailed:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireSourceUpdateStatus(status enum.PackageSourceStatus) error {
	if status == enum.PackageSourceStatusDisabled {
		return errs.ErrInvalidArgument
	}
	return requireSourceStatus(status)
}

func requireVerificationStatus(status enum.PackageVerificationStatus) error {
	switch status {
	case enum.PackageVerificationStatusVerified, enum.PackageVerificationStatusUnverified, enum.PackageVerificationStatusRejected, enum.PackageVerificationStatusRevoked:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requirePackageKind(kind enum.PackageKind) error {
	switch kind {
	case enum.PackageKindPlugin, enum.PackageKindGuidance, enum.PackageKindStore, enum.PackageKindPlatformContent:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requirePackageStatus(status enum.PackageStatus) error {
	switch status {
	case enum.PackageStatusAvailable, enum.PackageStatusHidden, enum.PackageStatusRevoked, enum.PackageStatusBlocked:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireCommercialStatus(status enum.PackageCommercialStatus) error {
	switch status {
	case enum.PackageCommercialStatusFree, enum.PackageCommercialStatusPaid, enum.PackageCommercialStatusRestricted, enum.PackageCommercialStatusUnknown:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireTrustStatus(status enum.PackageTrustStatus) error {
	switch status {
	case enum.PackageTrustStatusBuiltIn, enum.PackageTrustStatusVerified, enum.PackageTrustStatusUnverified, enum.PackageTrustStatusBlocked:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireSourceRefKind(kind enum.PackageVersionSourceRefKind) error {
	switch kind {
	case enum.PackageVersionSourceRefKindGitTag, enum.PackageVersionSourceRefKindGitCommit, enum.PackageVersionSourceRefKindGitlink, enum.PackageVersionSourceRefKindProxyRef:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireReleaseStatus(status enum.PackageReleaseStatus) error {
	switch status {
	case enum.PackageReleaseStatusActive, enum.PackageReleaseStatusDeprecated, enum.PackageReleaseStatusRevoked, enum.PackageReleaseStatusBlocked:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func requireManifestPayload(payload []byte) error {
	trimmed := bytes.TrimSpace(payload)
	if len(trimmed) == 0 || trimmed[0] != '{' || !json.Valid(trimmed) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func defaultActorRef(actorType string, actorID string) string {
	actorType = strings.TrimSpace(actorType)
	actorID = strings.TrimSpace(actorID)
	if actorType == "" || actorID == "" {
		return ""
	}
	return actorType + ":" + actorID
}
