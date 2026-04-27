package service

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/helpers"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/enum"
)

func (s *Service) findAllowlistEntry(ctx context.Context, email string) (entity.AllowlistEntry, string, error) {
	normalized := helpers.NormalizeEmail(email)
	entry, err := s.repository.FindAllowlistEntry(ctx, enum.AllowlistMatchEmail, normalized)
	if err == nil {
		if entry.Status == enum.AllowlistStatusDisabled {
			return entity.AllowlistEntry{}, reasonAllowlistDisabled, errs.ErrUnauthorizedSubject
		}
		return entry, reasonAllowlistEmail, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.AllowlistEntry{}, "", err
	}
	domain := helpers.EmailDomain(normalized)
	if domain != "" {
		entry, err = s.repository.FindAllowlistEntry(ctx, enum.AllowlistMatchDomain, domain)
		if err == nil {
			if entry.Status == enum.AllowlistStatusDisabled {
				return entity.AllowlistEntry{}, reasonAllowlistDisabled, errs.ErrUnauthorizedSubject
			}
			return entry, reasonAllowlistDomain, nil
		}
		if !errors.Is(err, errs.ErrNotFound) {
			return entity.AllowlistEntry{}, "", err
		}
	}
	return entity.AllowlistEntry{}, reasonAllowlistMiss, errs.ErrUnauthorizedSubject
}
