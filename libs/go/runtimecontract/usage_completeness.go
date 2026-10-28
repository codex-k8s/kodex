package runtimecontract

import (
	"encoding/json"
	"errors"
)

// errUsageCompleteness preserves the existing closed broker diagnostic after sharing its enum.
// errUsageCompleteness сохраняет прежнюю закрытую диагностику broker после переноса enum.
var errUsageCompleteness = errors.New("Codex usage completeness is invalid")

// UsageCompleteness describes numeric response observations seen by this process, not the provider's
// entire bill or a successful execution. Zero is UNKNOWN and remains optional in Result JSON.
// UsageCompleteness описывает числовые response наблюдения этого процесса, не полный счёт провайдера
// и не успех исполнения. Нулевое UNKNOWN остаётся необязательным полем JSON Result.
type UsageCompleteness uint8

const (
	// UsageUnknown means no numeric response observation has been accepted, even if display estimates exist.
	// UsageUnknown означает отсутствие принятых числовых response наблюдений, даже при наличии display оценок.
	UsageUnknown UsageCompleteness = iota
	// UsagePartial records some numeric observations without a sole terminal or with a missing response usage.
	// UsagePartial отмечает часть числовых наблюдений без единственного terminal либо при пропущенном response usage.
	UsagePartial
	// UsageComplete requires numeric observations, exactly one observed terminal and no missing usage;
	// it does not certify provider billing completeness or independently validate the execution outcome.
	// UsageComplete требует числовые наблюдения, один отмеченный terminal и отсутствие пропущенного usage;
	// она не подтверждает полный биллинг провайдера и самостоятельно не проверяет исход исполнения.
	UsageComplete
)

// MarshalJSON emits only the closed UNKNOWN/PARTIAL/COMPLETE spelling and rejects unsupported numeric values.
// MarshalJSON выдаёт только закрытые UNKNOWN/PARTIAL/COMPLETE и отвергает неподдержанные числовые значения.
func (quality UsageCompleteness) MarshalJSON() ([]byte, error) {
	switch quality {
	case UsageUnknown:
		return []byte(`"UNKNOWN"`), nil
	case UsagePartial:
		return []byte(`"PARTIAL"`), nil
	case UsageComplete:
		return []byte(`"COMPLETE"`), nil
	default:
		return nil, errUsageCompleteness
	}
}

// UnmarshalJSON accepts only exact closed string values and assigns the nonnil receiver after validation.
// UnmarshalJSON принимает лишь точные закрытые строки и назначает ненулевой receiver после проверки.
func (quality *UsageCompleteness) UnmarshalJSON(raw []byte) error {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return errUsageCompleteness
	}
	switch value {
	case "UNKNOWN":
		*quality = UsageUnknown
	case "PARTIAL":
		*quality = UsagePartial
	case "COMPLETE":
		*quality = UsageComplete
	default:
		return errUsageCompleteness
	}
	return nil
}

// Valid accepts only the three closed observation qualities; zero means legacy UNKNOWN.
// Valid принимает только три закрытых качества наблюдений; ноль означает legacy UNKNOWN.
func (quality UsageCompleteness) Valid() bool { return quality <= UsageComplete }
