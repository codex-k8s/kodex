package service

import (
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func (s *Service) defaultFleetScopeID(preferred *uuid.UUID) (*uuid.UUID, error) {
	if preferred != nil {
		return preferred, nil
	}
	if s.config.DefaultFleetScopeID == uuid.Nil {
		return nil, errs.ErrPreconditionFailed
	}
	return &s.config.DefaultFleetScopeID, nil
}

func (s *Service) slotKey(slotID uuid.UUID) string {
	return "slot-" + shortID(slotID)
}

func (s *Service) namespaceName(slotID uuid.UUID) string {
	prefix := strings.Trim(strings.ToLower(s.config.NamespacePrefix), "-")
	if prefix == "" {
		prefix = "kodex-rt"
	}
	return prefix + "-" + shortID(slotID)
}

func shortID(id uuid.UUID) string {
	return strings.ReplaceAll(id.String(), "-", "")[:12]
}

func leaseOwner(meta value.CommandMeta) (string, error) {
	if strings.TrimSpace(meta.Actor.Type) == "" || strings.TrimSpace(meta.Actor.ID) == "" {
		return "", errs.ErrInvalidArgument
	}
	return strings.TrimSpace(meta.Actor.Type) + ":" + strings.TrimSpace(meta.Actor.ID), nil
}
