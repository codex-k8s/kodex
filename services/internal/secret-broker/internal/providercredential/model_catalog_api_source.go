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
	// apiCatalogRevision pins the embedded API capability document revision that must match its digest and
	// runtime version before use.
	// apiCatalogRevision закрепляет ревизию embedded API capability документа, которая до использования
	// должна соответствовать digest и runtime версии.
	apiCatalogRevision = "openai-api-2026-10-03.1"
	// apiCatalogDigest binds the exact embedded capability document bytes, including provenance and dates;
	// it is an integrity pin, not an online signature or evidence of current account access.
	// apiCatalogDigest связывает точные байты embedded capability документа с происхождением и датами; это
	// integrity-привязка, не онлайн-подпись и не доказательство текущего доступа account.
	apiCatalogDigest = "45ef3dc265c887d396d15b3a15233fc585d9e076e80cb525f18e305bd1e53381"
	// apiCatalogMaxAge caps the declared verified-to-expiry interval at 30 days; current time must also fall
	// within that explicit validity window.
	// apiCatalogMaxAge ограничивает заявленный интервал от проверки до истечения 30 днями; текущее время
	// также должно попадать в явное окно действия.
	apiCatalogMaxAge = 30 * 24 * time.Hour
)

// apiCatalogSource embeds the reviewed dated API capability document, updated only with its
// runtime/image pins. Runtime-policy defaults remain distinguished from documented API defaults;
// embedding does not perform live documentation discovery.
// apiCatalogSource встраивает проверенный датированный API capability документ, обновляемый вместе с
// runtime/image привязками. Runtime-policy defaults отделены от документированных API defaults;
// embedding не выполняет live поиск документации.
//
//go:embed api_model_capabilities.json
var apiCatalogSource []byte

// apiModelCapability records pinned model efforts, the optional API-documented default and explicit
// default provenance; a runtime-selected default is not attributed to the provider.
// apiModelCapability фиксирует закреплённые model efforts, необязательный API-документированный default
// и его явное происхождение; default, выбранный runtime, не приписывается provider.
type apiModelCapability struct {
	ID                     string   `json:"id"`
	ReasoningEfforts       []string `json:"reasoningEfforts"`
	DefaultReasoningEffort string   `json:"defaultReasoningEffort"`
	APIDefault             *string  `json:"apiDefault"`
	DefaultOrigin          string   `json:"defaultOrigin"`
	Source                 string   `json:"source"`
}

// apiCapabilitySource stores the versioned document revision, runtime pin, verification/expiry dates and
// model entries used for bounded API capability interpretation.
// apiCapabilitySource хранит версионированную ревизию документа, runtime-привязку, даты проверки и
// истечения и model записи для ограниченной интерпретации API возможностей.
type apiCapabilitySource struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	Revision       string               `json:"revision"`
	VerifiedAt     time.Time            `json:"verifiedAt"`
	ValidUntil     time.Time            `json:"validUntil"`
	RuntimeVersion string               `json:"runtimeVersion"`
	Models         []apiModelCapability `json:"models"`
}

// readAPICapabilities verifies exact bytes, schema/revision/runtime pins, validity dates and the fixed
// reviewed model count, then validates source/default provenance and model capabilities. Unknown fields
// and trailing JSON fail; the expected digest is the byte-level integrity boundary, not an independent
// signature.
// readAPICapabilities проверяет точные байты, schema/revision/runtime привязки, даты действия и
// фиксированное число проверенных моделей, затем происхождение source/default и возможности моделей.
// Неизвестные поля и хвост JSON отклоняются; ожидаемый digest задаёт байтовую integrity-границу, не
// независимую подпись.
func readAPICapabilities(raw []byte, expectedDigest string, now time.Time) ([]CatalogModel, error) {
	digest := sha256.Sum256(raw)
	if len(raw) > maximumModelCatalogBytes || hex.EncodeToString(digest[:]) != expectedDigest {
		return nil, errModelCatalogUnverified
	}
	var source apiCapabilitySource
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&source) != nil || decoder.Decode(new(any)) != io.EOF || source.SchemaVersion != 1 || source.Revision != apiCatalogRevision || source.RuntimeVersion != catalogCodexVersion || source.VerifiedAt.IsZero() || now.Before(source.VerifiedAt) || !now.Before(source.ValidUntil) || !source.ValidUntil.After(source.VerifiedAt) || source.ValidUntil.Sub(source.VerifiedAt) > apiCatalogMaxAge || len(source.Models) != 9 {
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
