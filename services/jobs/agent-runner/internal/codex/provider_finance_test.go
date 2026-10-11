package codex

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestRateLimitNotificationOnlyWakesAuthoritativeRead(t *testing.T) {
	state := newProtocolState("")
	calls := 0
	state.onActivity = func(runtimecontract.RuntimeActivity) error { calls++; return nil }
	for _, raw := range []json.RawMessage{json.RawMessage(`{"rateLimits":null}`), json.RawMessage(`{"rateLimits":{"credits":{"balance":"invented"}}}`)} {
		if err := state.notification("account/rateLimits/updated", raw); err != nil {
			t.Fatal(err)
		}
	}
	if !state.financialWake || calls != 0 {
		t.Fatal("notification became an account measurement")
	}
}
func TestBrokerFinancialSequenceRejectsReplayAndMixedActivity(t *testing.T) {
	for _, sequences := range [][]int64{{1, 2}, {1, 1}, {2}} {
		var stream bytes.Buffer
		for index, sequence := range sequences {
			frame := brokerFrame{Version: providerBrokerVersion, Sequence: uint64(index + 1), Kind: brokerFrameActivity, Activity: &runtimecontract.RuntimeActivity{Financial: &runtimecontract.ProviderFinancialObservation{Sequence: sequence, State: "UNAVAILABLE"}}}
			if err := json.NewEncoder(&stream).Encode(frame); err != nil {
				t.Fatal(err)
			}
		}
		// Отсутствующий terminal остаётся invalid независимо от корректности prefix;
		// до EOF callback показывает, был ли replay отвергнут до доставки.
		calls := 0
		_, err := readProviderBrokerResponse(&stream, func(runtimecontract.RuntimeActivity) error { calls++; return nil })
		if err == nil {
			t.Fatal("unterminated broker accepted")
		}
		want := 2
		if len(sequences) == 1 {
			want = 0
		} else if sequences[1] == 1 {
			want = 1
		}
		if calls != want {
			t.Fatalf("sequences %v delivered %d observations, want %d", sequences, calls, want)
		}
	}
	activity := runtimecontract.RuntimeActivity{Financial: &runtimecontract.ProviderFinancialObservation{Sequence: 1, State: "UNAVAILABLE"}, Message: &runtimecontract.RuntimeAgentMessage{ItemID: "fixture", Phase: "FINAL", Revision: 1, Text: "safe"}}
	if activity.Validate() == nil {
		t.Fatal("mixed financial/model activity accepted")
	}
}
