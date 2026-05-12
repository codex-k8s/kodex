package service

import (
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
)

type placementCommandPayload struct {
	PlacementFingerprint string `json:"placement_fingerprint,omitempty"`
}

func commandPayloadWithPlacementFingerprint(fingerprint string) ([]byte, error) {
	return json.Marshal(placementCommandPayload{PlacementFingerprint: strings.TrimSpace(fingerprint)})
}

func validatePlacementReplayFingerprint(result entity.CommandResult, fingerprint string) error {
	if strings.TrimSpace(fingerprint) == "" {
		return errs.ErrConflict
	}
	var payload placementCommandPayload
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
		return errs.ErrConflict
	}
	if strings.TrimSpace(payload.PlacementFingerprint) != strings.TrimSpace(fingerprint) {
		return errs.ErrConflict
	}
	return nil
}
