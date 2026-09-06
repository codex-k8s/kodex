package providercredential

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
)

const (
	apiCatalogRevision = "openai-api-2026-09-06.1"
	apiCatalogDigest   = "bfb2bda2354b563968f77c4176032c5728ee450fdc2839cc6e591829a7c2cd63"
	apiCatalogMaxAge   = 30 * 24 * time.Hour
)

// Источник обновляется только вместе с проверенным runtime и новым image.
// Default Astra назначен Kodex; apiDefault=null не приписывает его OpenAI.
//
//go:embed api_model_capabilities.json
var apiCatalogSource []byte

type apiModelCapability struct {
	ID                     string   `json:"id"`
	ReasoningEfforts       []string `json:"reasoningEfforts"`
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	APIDefault             *string  `json:"apiDefault"`
	DefaultOrigin          string   `json:"defaultOrigin"`
	Source                 string   `json:"source"`
}

type apiCapabilitySource struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Revision       string               `json:"revision"`
	VerifiedAt     time.Time            `json:"verifiedAt"`
	ValidUntil     time.Time            `json:"validUntil"`
	RuntimeVersion string               `json:"runtimeVersion"`
	Models         []apiModelCapability `json:"models"`
}

func readAPICapabilities(raw []byte, expectedDigest string, now time.Time) ([]CatalogModel, error) {
	digest := sha256.Sum256(raw)
	if len(raw) > maximumModelCatalogBytes || hex.EncodeToString(digest[:]) != expectedDigest {
		return nil, errModelCatalogUnverified
	}
	var source apiCapabilitySource
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || decoder.Decode(new(any)) != io.EOF || source.SchemaVersion != 1 || source.Revision != apiCatalogRevision || source.RuntimeVersion != catalogCodexVersion || source.VerifiedAt.IsZero() || now.Before(source.VerifiedAt) || !now.Before(source.ValidUntil) || !source.ValidUntil.After(source.VerifiedAt) || source.ValidUntil.Sub(source.VerifiedAt) > apiCatalogMaxAge || len(source.Models) != 7 {
		return nil, errModelCatalogUnverified
	}
	models := make([]CatalogModel, 0, len(source.Models))
	for _, model := range source.Models {
		if model.Source != "https://developers.openai.com/api/docs/models/"+model.ID {
			return nil, errModelCatalogUnverified
		}
		switch model.DefaultOrigin {
		case "OPENAI_API_DOCUMENTATION":
			if model.APIDefault == nil || *model.APIDefault != model.DefaultReasoningEffort || model.ID == "gpt-6-astra" {
				return nil, errModelCatalogUnverified
			}
		case "KODEX_RUNTIME_POLICY":
			if model.ID != "gpt-6-astra" || model.APIDefault != nil || model.DefaultReasoningEffort != "low" {
				return nil, errModelCatalogUnverified
			}
		default:
			return nil, errModelCatalogUnverified
		}
		models = append(models, CatalogModel{ID: model.ID, ReasoningEfforts: model.ReasoningEfforts, DefaultReasoningEffort: model.DefaultReasoningEffort})
	}
	if validateCatalogModels(models) != nil {
		return nil, errModelCatalogUnverified
	}
	return models, nil
}
