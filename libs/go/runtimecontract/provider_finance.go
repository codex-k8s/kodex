package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"unicode/utf8"
)

// ProviderFinancialObservation — безопасный subset supported account/rateLimits/read.
// Identity и время получения назначает владелец lease; денежная единица неизвестна.
type ProviderFinancialObservation struct {
	Sequence             int64                     `json:"sequence"`
	State                string                    `json:"state"`
	Windows              []ProviderFinancialWindow `json:"windows,omitempty"`
	Credits              *ProviderFinancialCredits `json:"credits,omitempty"`
	OrdinaryUsageAllowed *bool                     `json:"ordinaryUsageAllowed,omitempty"`
	SpendControlReached  *bool                     `json:"spendControlReached,omitempty"`
	Restriction          string                    `json:"restriction,omitempty"`
}
type ProviderFinancialWindow struct {
	UsedPercent     *int32 `json:"usedPercent"`
	DurationMinutes *int64 `json:"durationMinutes,omitempty"`
	ResetsAt        *int64 `json:"resetsAt,omitempty"`
}
type ProviderFinancialCredits struct {
	HasCredits *bool   `json:"hasCredits,omitempty"`
	Unlimited  *bool   `json:"unlimited,omitempty"`
	Balance    *string `json:"balance,omitempty"`
}

var financialBalance = regexp.MustCompile(`^[0-9]{1,64}(\.[0-9]{1,64})?$`)

// Stored/callback projection проверяется отдельно от upstream ответа. Ни один
// consumer не удаляет неизвестные поля до semantic validation.
func ParseProviderFinancialObservation(raw []byte) (ProviderFinancialObservation, error) {
	const maximumProjectionBytes = 4 << 10
	invalid := errors.New("provider financial projection is invalid")
	if len(raw) == 0 || len(raw) > maximumProjectionBytes || !utf8.Valid(raw) || financialJSONKeys(raw) != nil {
		return ProviderFinancialObservation{}, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil ||
		!financialKnownFields(fields, "sequence", "state", "windows", "credits", "ordinaryUsageAllowed", "spendControlReached", "restriction") {
		return ProviderFinancialObservation{}, invalid
	}
	var windows []map[string]json.RawMessage
	if value := fields["windows"]; len(value) > 0 {
		if json.Unmarshal(value, &windows) != nil {
			return ProviderFinancialObservation{}, invalid
		}
		for _, window := range windows {
			if !financialKnownFields(window, "usedPercent", "durationMinutes", "resetsAt") {
				return ProviderFinancialObservation{}, invalid
			}
		}
	}
	var credits map[string]json.RawMessage
	if value := fields["credits"]; len(value) > 0 &&
		(json.Unmarshal(value, &credits) != nil || !financialKnownFields(credits, "hasCredits", "unlimited", "balance")) {
		return ProviderFinancialObservation{}, invalid
	}
	// Alias исключает повторный вызов UnmarshalJSON при проверке wire payload.
	type plainObservation ProviderFinancialObservation
	var decoded plainObservation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || ProviderFinancialObservation(decoded).Validate() != nil {
		return ProviderFinancialObservation{}, invalid
	}
	return ProviderFinancialObservation(decoded), nil
}

// Вложенный callback проходит ту же закрытую проверку, что stored projection.
func (value *ProviderFinancialObservation) UnmarshalJSON(raw []byte) error {
	observation, err := ParseProviderFinancialObservation(raw)
	if err != nil {
		return err
	}
	*value = observation
	return nil
}

func (value ProviderFinancialObservation) Validate() error {
	if value.Sequence < 1 || value.Sequence > 64 || (value.State != "AVAILABLE" && value.State != "UNAVAILABLE") || len(value.Windows) > 2 {
		return errors.New("provider financial observation is invalid")
	}
	if value.State == "UNAVAILABLE" && (len(value.Windows) > 0 || value.Credits != nil || value.OrdinaryUsageAllowed != nil || value.SpendControlReached != nil || value.Restriction != "") {
		return errors.New("unavailable financial observation contains measurements")
	}
	for _, window := range value.Windows {
		if window.UsedPercent == nil || *window.UsedPercent < 0 || *window.UsedPercent > 100 || window.DurationMinutes != nil && (*window.DurationMinutes <= 0 || *window.DurationMinutes > 5256000) || window.ResetsAt != nil && (*window.ResetsAt < 0 || *window.ResetsAt > 253402300799) {
			return errors.New("provider financial window is invalid")
		}
	}
	if value.Credits != nil && value.Credits.Balance != nil && !financialBalance.MatchString(*value.Credits.Balance) {
		return errors.New("provider credit balance is invalid")
	}
	switch value.Restriction {
	case "", "rate_limit_reached", "workspace_owner_credits_depleted", "workspace_member_credits_depleted", "workspace_owner_usage_limit_reached", "workspace_member_usage_limit_reached":
	default:
		return errors.New("provider restriction is invalid")
	}
	return nil
}
