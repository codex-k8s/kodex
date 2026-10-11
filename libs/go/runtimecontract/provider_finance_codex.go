package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// Обрабатываем только codex bucket; расширения upstream не становятся payload UI.
func ParseCodexFinancialObservation(raw json.RawMessage, sequence int64) ProviderFinancialObservation {
	unavailable := ProviderFinancialObservation{Sequence: sequence, State: "UNAVAILABLE"}
	if len(raw) > 64<<10 || financialJSONKeys(raw) != nil {
		return unavailable
	}
	type window struct {
		UsedPercent        *int32 `json:"usedPercent"`
		WindowDurationMins *int64 `json:"windowDurationMins"`
		ResetsAt           *int64 `json:"resetsAt"`
	}
	type bucket struct {
		LimitID   *string `json:"limitId"`
		Primary   *window `json:"primary"`
		Secondary *window `json:"secondary"`
		Credits   *struct {
			HasCredits *bool   `json:"hasCredits"`
			Unlimited  *bool   `json:"unlimited"`
			Balance    *string `json:"balance"`
		} `json:"credits"`
		SpendControlReached *bool   `json:"spendControlReached"`
		Restriction         *string `json:"rateLimitReachedType"`
	}
	var response struct {
		RateLimits *bucket            `json:"rateLimits"`
		ByID       map[string]*bucket `json:"rateLimitsByLimitId"`
		Ordinary   *bool              `json:"ordinaryUsageAllowed"`
	}
	if json.Unmarshal(raw, &response) != nil || response.RateLimits == nil || len(response.ByID) > 32 {
		return unavailable
	}
	selected := response.RateLimits
	if response.ByID != nil {
		selected = response.ByID["codex"]
		if selected == nil {
			return unavailable
		}
	}
	if selected.LimitID != nil && *selected.LimitID != "codex" {
		return unavailable
	}
	value := ProviderFinancialObservation{Sequence: sequence, State: "AVAILABLE", OrdinaryUsageAllowed: response.Ordinary, SpendControlReached: selected.SpendControlReached}
	if selected.Restriction != nil {
		value.Restriction = *selected.Restriction
	}
	for _, item := range []*window{selected.Primary, selected.Secondary} {
		if item == nil {
			continue
		}
		if item.UsedPercent == nil {
			return unavailable
		}
		value.Windows = append(value.Windows, ProviderFinancialWindow{UsedPercent: *item.UsedPercent, DurationMinutes: item.WindowDurationMins, ResetsAt: item.ResetsAt})
	}
	if credits := selected.Credits; credits != nil {
		if credits.HasCredits == nil || credits.Unlimited == nil {
			return unavailable
		}
		value.Credits = &ProviderFinancialCredits{HasCredits: *credits.HasCredits, Unlimited: *credits.Unlimited, Balance: credits.Balance}
	}
	if value.Validate() != nil {
		return unavailable
	}
	return value
}

// Закрываем повторные ключи до декодирования финансовых полей.
func financialJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("financial JSON depth exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate financial JSON key")
				}
				seen[name] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid financial JSON")
		}
		_, err = decoder.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing financial JSON")
	}
	return nil
}
