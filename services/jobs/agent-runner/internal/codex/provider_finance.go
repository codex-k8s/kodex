package codex

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func (server *appServer) observeFinancial(ctx context.Context, state *protocolState) error {
	if !state.financialEnabled || state.financialSequence >= 64 {
		return nil
	}
	state.financialWake = false
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
		observation = runtimecontract.ParseCodexFinancialObservation(raw, state.financialSequence)
	}
	return state.publishActivity(runtimecontract.RuntimeActivity{Financial: &observation})
}
