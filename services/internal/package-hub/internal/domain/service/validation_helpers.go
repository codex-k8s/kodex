package service

import (
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

func requireVerificationStatus(status enum.PackageVerificationStatus) error {
	switch status {
	case enum.PackageVerificationStatusVerified, enum.PackageVerificationStatusUnverified, enum.PackageVerificationStatusRejected, enum.PackageVerificationStatusRevoked:
		return nil
	default:
		return errs.ErrInvalidArgument
	}
}

func defaultActorRef(actorType string, actorID string) string {
	actorType = strings.TrimSpace(actorType)
	actorID = strings.TrimSpace(actorID)
	if actorType == "" || actorID == "" {
		return ""
	}
	return actorType + ":" + actorID
}
