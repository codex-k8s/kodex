package runtimecontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

// Поля supported codex bucket проверяются независимо; unknown не становится zero.
func ParseCodexFinancialObservation(raw json.RawMessage, sequence int64) ProviderFinancialObservation {
	unavailable := ProviderFinancialObservation{Sequence: sequence, State: "UNAVAILABLE"}
	if len(raw) > 64<<10 || !utf8.Valid(raw) || financialJSONKeys(raw) != nil {
		return unavailable
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(raw, &response) != nil || response == nil {
		return unavailable
	}
	if !financialKnownFields(response, "ordinaryUsageAllowed", "rateLimits", "rateLimitsByLimitId", "rateLimitResetCredits", "accountId", "rateLimitUpsell") {
		return unavailable
	}
	selected := response["rateLimits"]
	if byID := response["rateLimitsByLimitId"]; len(byID) > 0 && !bytes.Equal(bytes.TrimSpace(byID), []byte("null")) {
		var buckets map[string]json.RawMessage
		if json.Unmarshal(byID, &buckets) != nil || len(buckets) > 32 {
			return unavailable
		}
		selected = buckets["codex"]
	}
	var bucket map[string]json.RawMessage
	if json.Unmarshal(selected, &bucket) != nil || bucket == nil {
		return unavailable
	}
	if !financialKnownFields(bucket, "limitId", "limitName", "normalModelSlug", "primary", "secondary", "credits", "individualLimit", "spendControlReached", "planType", "rateLimitReachedType") {
		return unavailable
	}
	if id := bucket["limitId"]; len(id) > 0 && !bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
		var value string
		if json.Unmarshal(id, &value) != nil || value != "codex" {
			return unavailable
		}
	}
	value := ProviderFinancialObservation{Sequence: sequence, State: "AVAILABLE", OrdinaryUsageAllowed: financialBool(response["ordinaryUsageAllowed"]), SpendControlReached: financialBool(bucket["spendControlReached"])}
	var restriction string
	if json.Unmarshal(bucket["rateLimitReachedType"], &restriction) == nil {
		switch restriction {
		case "rate_limit_reached", "workspace_owner_credits_depleted", "workspace_member_credits_depleted", "workspace_owner_usage_limit_reached", "workspace_member_usage_limit_reached":
			value.Restriction = restriction
		}
	}
	for _, key := range []string{"primary", "secondary"} {
		var window map[string]json.RawMessage
		if json.Unmarshal(bucket[key], &window) != nil || window == nil {
			continue
		}
		if !financialKnownFields(window, "usedPercent", "windowDurationMins", "resetsAt") {
			return unavailable
		}
		used := financialInteger(window["usedPercent"], 0, 100)
		if used == nil {
			continue
		}
		value.Windows = append(value.Windows, ProviderFinancialWindow{UsedPercent: new(int32(*used)), DurationMinutes: financialInteger(window["windowDurationMins"], 1, 5256000), ResetsAt: financialInteger(window["resetsAt"], 0, 253402300799)})
	}
	var credits map[string]json.RawMessage
	if json.Unmarshal(bucket["credits"], &credits) == nil && credits != nil {
		if !financialKnownFields(credits, "hasCredits", "unlimited", "balance") {
			return unavailable
		}
		value.Credits = &ProviderFinancialCredits{HasCredits: financialBool(credits["hasCredits"]), Unlimited: financialBool(credits["unlimited"])}
		var balance string
		if json.Unmarshal(credits["balance"], &balance) == nil && financialBalance.MatchString(balance) {
			value.Credits.Balance = &balance
		}
	}
	if value.Validate() != nil {
		return unavailable
	}
	return value
}

// Allowlist закреплён account.rs Codex0.160.0; новые wire поля требуют проверки
// контракта. Известные presentation metadata не переносятся в проекцию.
func financialKnownFields(object map[string]json.RawMessage, allowed ...string) bool {
	for key := range object {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

func financialBool(raw json.RawMessage) *bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value bool
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func financialInteger(raw json.RawMessage, minimum, maximum int64) *int64 {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var value int64
	if json.Unmarshal(raw, &value) != nil || value < minimum || value > maximum {
		return nil
	}
	return &value
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
