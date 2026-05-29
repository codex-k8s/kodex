package app

import (
	"strings"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/governance-manager/internal/domain/errs"
)

func parseReleaseDecisionPackageID(ref string) (uuid.UUID, error) {
	for _, candidate := range externalRefIDCandidates(ref) {
		parsed, err := uuid.Parse(candidate)
		if err == nil && parsed != uuid.Nil {
			return parsed, nil
		}
	}
	return uuid.Nil, errs.ErrInvalidArgument
}

func externalRefIDCandidates(ref string) []string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return nil
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/' || r == ':' || r == '#'
	})
	candidates := []string{trimmed}
	for index := len(parts) - 1; index >= 0; index-- {
		part := strings.TrimSpace(parts[index])
		if part != "" {
			candidates = append(candidates, part)
		}
	}
	return candidates
}
