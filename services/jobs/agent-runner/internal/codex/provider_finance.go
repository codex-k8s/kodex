package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func (server *appServer) observeFinancial(ctx context.Context, state *protocolState) error {
	if !state.financialEnabled || state.financialSequence >= 64 {
		return nil
	}
	state.financialWake = false
	epoch := state.financialEpoch
	raw, err := server.call(ctx, state, "account/rateLimits/read", map[string]bool{"supportsLunaReserve": false, "excludeResetCreditDetails": true})
	state.financialSequence++
	observation := runtimecontract.ProviderFinancialObservation{Sequence: state.financialSequence, State: "UNAVAILABLE"}
	if err != nil {
		var failure *appServerCallFailure
		// Только точный pinned no-snapshots outcome завершённого RPC.
		// Auth, transport и policy failure сохраняют закрытый gate.
		if !errors.As(err, &failure) || failure.detail != "RPC_ERROR" || !failure.financialUnavailable {
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

// Nullable identity и смена sealed process/auth epoch не становятся measurement.
// Expected ID назначен private pinned snapshot; значение не включается в ошибки.
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
