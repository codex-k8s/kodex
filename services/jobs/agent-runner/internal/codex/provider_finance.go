package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// observeFinancial allows at most 63 ordinary observations, reserving the last slot for final collection.
// observeFinancial разрешает максимум63 обычных наблюдения, оставляя последний слот итоговому сбору.
func (server *appServer) observeFinancial(ctx context.Context, state *protocolState) error {
	// Последний из 64 слотов сохраняется для чтения перед stop/join.
	return server.observeFinancialWithinBudget(ctx, state, 63)
}

// observeFinancialFinal permits the reserved 64th observation; its caller owns process stop/join ordering.
// observeFinancialFinal разрешает зарезервированное64-е наблюдение; порядок stop/join задаёт вызывающий код.
func (server *appServer) observeFinancialFinal(ctx context.Context, state *protocolState) error {
	return server.observeFinancialWithinBudget(ctx, state, 64)
}

// observeFinancialWithinBudget clears the wake flag and reads only while enabled and below the limit.
// After an auth epoch change it rechecks ordinary account type, then accepts a financial snapshot only
// for the private expected identity and unchanged bound epoch. Method absence or the pinned no-snapshot
// RPC outcome yields UNAVAILABLE; other failures propagate. Notifications alone never grant admission.
// observeFinancialWithinBudget снимает флаг пробуждения и читает лишь при включённом сборе ниже лимита.
// После смены auth epoch она заново проверяет тип аккаунта и принимает финансовый снимок только для
// ожидаемой приватной идентичности при неизменной привязанной epoch. Отсутствующий метод или точный
// закреплённый ответ об отсутствии снимка дают UNAVAILABLE; другие ошибки сохраняются. Уведомление не даёт допуска.
func (server *appServer) observeFinancialWithinBudget(ctx context.Context, state *protocolState, maximumSequence int64) error {
	state.financialWake = false
	if !state.financialEnabled || state.financialSequence >= maximumSequence {
		return nil
	}
	// После account/updated прежняя epoch больше не связывает read. Свежий
	// account/read подтверждает только ordinary account type; identity по-прежнему
	// берётся из private binding и проверяется в rate-limit response.
	if state.financialEpoch != state.financialBoundEpoch {
		epoch := state.financialEpoch
		account, err := server.call(ctx, state, "account/read", map[string]bool{"refreshToken": false})
		if err := classifyAccountReadResponse(account, err); err != nil {
			return err
		}
		var response struct {
			Account *struct {
				Type string `json:"type"`
			} `json:"account"`
		}
		if json.Unmarshal(account, &response) != nil {
			return errAccountReadResponseInvalid
		}
		state.financialEnabled = response.Account != nil && response.Account.Type == "chatgpt"
		if state.financialEnabled && epoch == state.financialEpoch {
			state.financialBoundEpoch = epoch
		}
		if !state.financialEnabled {
			state.financialSequence++
			unavailable := runtimecontract.ProviderFinancialObservation{Sequence: state.financialSequence, State: "UNAVAILABLE"}
			return state.publishActivity(runtimecontract.RuntimeActivity{Financial: &unavailable})
		}
	}
	epoch := state.financialEpoch
	raw, err := server.call(ctx, state, "account/rateLimits/read", map[string]bool{"supportsLunaReserve": false, "excludeResetCreditDetails": true})
	state.financialSequence++
	observation := runtimecontract.ProviderFinancialObservation{Sequence: state.financialSequence, State: "UNAVAILABLE"}
	if err != nil {
		var failure *appServerCallFailure
		// Только method absence либо точный pinned no-snapshots outcome
		// завершённого коррелированного RPC.
		// Auth, transport и policy failure сохраняют закрытый gate.
		if !errors.As(err, &failure) || failure.detail != "RPC_ERROR" || (failure.code != -32601 && !failure.financialUnavailable) {
			return err
		}
	} else {
		bound, bindErr := financialResponseBound(raw, state.financialExpectedAccount)
		if bindErr != nil {
			return bindErr
		}
		if bound && epoch == state.financialEpoch && epoch == state.financialBoundEpoch {
			observation = runtimecontract.ParseCodexFinancialObservation(raw, state.financialSequence)
		}
	}
	return state.publishActivity(runtimecontract.RuntimeActivity{Financial: &observation})
}

// financialResponseBound checks bounded UTF-8 JSON without duplicate keys and compares accountId to
// the private expected identity. Missing/null identity or no expected binding means unavailable, not
// trusted measurement; malformed or foreign identity is an error without exposing either identifier.
// Other snapshot fields are parsed by the financial contract, not authenticated by this comparison.
// financialResponseBound проверяет ограниченный UTF-8 JSON без повторных ключей и сравнивает accountId
// с приватной ожидаемой идентичностью. Отсутствующая или null-идентичность либо отсутствие привязки
// дают недоступность, а не доверенное измерение; неверная или чужая идентичность отклоняется без вывода
// идентификаторов. Остальные поля снимка разбирает финансовый контракт, а не это сравнение.
func financialResponseBound(raw json.RawMessage, expected string) (bool, error) {
	if len(raw) > 64<<10 || !utf8.Valid(raw) || rejectDuplicateJSONKeys(raw) != nil {
		return false, errors.New("Codex financial response is invalid")
	}
	var response map[string]json.RawMessage
	if json.Unmarshal(raw, &response) != nil || response == nil {
		return false, errors.New("Codex financial response is invalid")
	}
	id, present := response["accountId"]
	if !present || bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
		return false, nil
	}
	var value string
	if json.Unmarshal(id, &value) != nil || value == "" || len(value) > 512 {
		return false, errors.New("Codex financial identity is invalid")
	}
	if expected == "" {
		return false, nil
	}
	if value != expected {
		return false, errors.New("Codex financial identity mismatch")
	}
	return true, nil
}
