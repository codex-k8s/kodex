package providercredential

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const (
	maximumCatalogModels     = 128
	maximumCatalogEfforts    = 16
	maximumModelCatalogBytes = 4 << 20
	modelCatalogTimeout      = 15 * time.Second
	catalogCodexVersion      = "0.152.0"
)

type ModelCatalogSource string
type ModelCatalogFailure string

const (
	CatalogRemoteAPI            ModelCatalogSource  = "REMOTE_API"
	CatalogRemoteCodex          ModelCatalogSource  = "REMOTE_CODEX"
	CatalogFailureNone          ModelCatalogFailure = "NONE"
	CatalogFailureUnavailable   ModelCatalogFailure = "UNAVAILABLE"
	CatalogFailureUnverified    ModelCatalogFailure = "UNVERIFIED_SOURCE"
	CatalogFailureAuthorization ModelCatalogFailure = "AUTHORIZATION_REJECTED"
)

var (
	modelCatalogIDPattern        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)
	modelCatalogEffortPattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	errModelCatalogUnverified    = errors.New("provider model catalog source is unverified")
	errModelCatalogAuthorization = errors.New("provider model catalog authorization was rejected")
)

// ModelCatalog содержит только безопасные возможности, без provider payload.
type ModelCatalog struct {
	ObservedAt time.Time
	Source     ModelCatalogSource
	Models     []CatalogModel
	Failure    ModelCatalogFailure
}

type CatalogModel struct {
	ID                     string
	DefaultReasoningEffort string
	ReasoningEfforts       []string
}

func validateCatalogModels(models []CatalogModel) error {
	if len(models) > maximumCatalogModels {
		return errModelCatalogUnverified
	}
	ids := make(map[string]struct{}, len(models))
	for _, model := range models {
		if !modelCatalogIDPattern.MatchString(model.ID) || len(model.ReasoningEfforts) > maximumCatalogEfforts {
			return errModelCatalogUnverified
		}
		if _, exists := ids[model.ID]; exists {
			return errModelCatalogUnverified
		}
		ids[model.ID] = struct{}{}
		if len(model.ReasoningEfforts) == 0 {
			if model.DefaultReasoningEffort != "" {
				return errModelCatalogUnverified
			}
			continue
		}
		efforts := make(map[string]struct{}, len(model.ReasoningEfforts))
		for _, effort := range model.ReasoningEfforts {
			if !modelCatalogEffortPattern.MatchString(effort) {
				return errModelCatalogUnverified
			}
			if _, exists := efforts[effort]; exists {
				return errModelCatalogUnverified
			}
			efforts[effort] = struct{}{}
		}
		if _, supported := efforts[model.DefaultReasoningEffort]; !supported {
			return errModelCatalogUnverified
		}
	}
	return nil
}

func catalogFailure(ctx context.Context, err error) (ModelCatalog, error) {
	if ctx.Err() != nil {
		return ModelCatalog{}, ctx.Err()
	}
	failure := CatalogFailureUnavailable
	if errors.Is(err, errModelCatalogUnverified) {
		failure = CatalogFailureUnverified
	} else if errors.Is(err, errModelCatalogAuthorization) {
		failure = CatalogFailureAuthorization
	}
	return ModelCatalog{ObservedAt: time.Now().UTC(), Failure: failure}, nil
}
