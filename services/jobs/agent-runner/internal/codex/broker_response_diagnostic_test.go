package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// TestBrokerResponseFailureCategoryKeepsSentinelAndRedactsJoinedCause exercises every known category with joined sentinels/context/private causes, preserving errors.Is and primary classification while requiring fixed text, empty JSON fields and safe unknown/typed-nil fallback.
// TestBrokerResponseFailureCategoryKeepsSentinelAndRedactsJoinedCause проверяет все известные категории с joined sentinels/context/private causes, сохраняя errors.Is и primary classification и требуя fixed text, empty JSON fields и safe unknown/typed-nil fallback.
func TestBrokerResponseFailureCategoryKeepsSentinelAndRedactsJoinedCause(t *testing.T) {
	for _, category := range []brokerResponseFailureCategory{
		brokerResponseCallbacks, brokerResponsePostTerminal, brokerResponseFrameLimit,
		brokerResponseFrameSchema, brokerResponseFrameVersion, brokerResponseFrameSequence,
		brokerResponseFrameKind, brokerResponseActivitySchema, brokerResponseProviderTuple,
		brokerResponseMessageReplay, brokerResponseMessageLimit, brokerResponseToolLifecycle,
		brokerResponseToolLimit, brokerResponseTerminalSchema, brokerResponseDiagnostic,
		brokerResponseArchive, brokerResponseStreamRead, brokerResponseNoTerminal,
		brokerResponseTerminalResult, brokerResponseActivityFailed,
	} {
		for _, sentinel := range []error{errProviderBrokerResponseInvalid, errProviderBrokerFailed,
			ErrProviderAuthentication, ErrAuthorityRequestUnsupported, ErrRequiredMCPUnavailable, ErrRuntimeProfile} {
			for _, secondary := range []error{context.Canceled, context.DeadlineExceeded} {
				private := errors.New("PRIVATE_SENTINEL auth=private frame=private")
				primary := brokerResponseError(category, errors.Join(sentinel, private))
				err := fmt.Errorf("outer: %w", errors.Join(primary, secondary, private))
				if !errors.Is(err, sentinel) || !errors.Is(err, secondary) || !errors.Is(err, private) || BrokerResponseFailureCategory(err) != string(category) {
					t.Fatal("typed diagnostic lost original cause or primary category")
				}
				if strings.Contains(primary.Error(), "PRIVATE_SENTINEL") || strings.Contains(BrokerResponseFailureCategory(err), "private") {
					t.Fatal("typed diagnostic serialized private cause")
				}
				if BrokerResponseFailureCategory(errors.Join(secondary, private, primary)) != string(category) {
					t.Fatal("secondary context failure replaced primary category")
				}
				wire, encodeErr := json.Marshal(primary)
				if encodeErr != nil || string(wire) != "{}" {
					t.Fatal("typed diagnostic exposed private fields to serialization")
				}
			}
		}
	}
	for _, err := range []error{nil, errors.New("PRIVATE_SENTINEL"), brokerResponseError("PRIVATE_SENTINEL", errProviderBrokerFailed), (*brokerResponseFailure)(nil)} {
		if BrokerResponseFailureCategory(err) != "UNKNOWN" {
			t.Fatal("unknown category escaped closed diagnostics")
		}
	}
}

// TestBrokerResponseReadFailureCategories feeds malformed and out-of-order synthetic streams, replay/lifecycle/terminal/archive errors and callback failures to the real reader, requiring the exact closed category and original sentinel without private text.
// TestBrokerResponseReadFailureCategories передаёт real reader malformed/out-of-order synthetic streams, replay/lifecycle/terminal/archive errors и callback failures, требуя точную closed category и исходный sentinel без private text.
func TestBrokerResponseReadFailureCategories(t *testing.T) {
	frame := func(sequence uint64, kind string, activity *runtimecontract.RuntimeActivity, terminal *brokerResponse) string {
		t.Helper()
		raw, err := json.Marshal(brokerFrame{Version: providerBrokerVersion, Sequence: sequence, Kind: kind, Activity: activity, Terminal: terminal})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw) + "\n"
	}
	message := &runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{
		ItemID: "message-one", Phase: "COMMENTARY", Revision: 1, Text: "PRIVATE_SENTINEL"}}
	tool := &runtimecontract.RuntimeActivity{ToolCall: &runtimecontract.NativeToolCall{
		CallID: "call-one", Revision: 1, Kind: runtimecontract.NativeToolKindSleep,
		State:          runtimecontract.NativeToolStateRunning,
		SafeParameters: map[string]any{"requested_duration_ms": int64(1)}}}
	terminal := &brokerResponse{Failure: providerBrokerFailureProvider}
	activity := frame(1, brokerFrameActivity, message, nil)
	end := frame(1, brokerFrameTerminal, nil, terminal)
	input := diagnosticInput()
	diagnostic, _ := providerFailureDetails(providerStageTerminalWait, errors.New("PRIVATE_SENTINEL"))
	bound := bindProviderDiagnostic(input, diagnostic)
	for _, test := range []struct {
		name, wire, category string
		cause                error
	}{
		{"schema", `{"version":3,"sequence":1,"kind":"PRIVATE_SENTINEL","unknown":"PRIVATE_SENTINEL"}`, "FRAME_SCHEMA", errProviderBrokerResponseInvalid},
		{"duplicate key", `{"version":3,"sequence":1,"sequence":1,"kind":"PRIVATE_SENTINEL"}`, "FRAME_SCHEMA", errProviderBrokerResponseInvalid},
		{"version", strings.Replace(activity, `"version":3`, `"version":2`, 1), "FRAME_VERSION", errProviderBrokerResponseInvalid},
		{"sequence", frame(2, brokerFrameActivity, message, nil), "FRAME_SEQUENCE", errProviderBrokerResponseInvalid},
		{"kind", frame(1, "PRIVATE_SENTINEL", nil, nil), "FRAME_KIND", errProviderBrokerResponseInvalid},
		{"activity", frame(1, brokerFrameActivity, nil, nil), "ACTIVITY_SCHEMA", errProviderBrokerResponseInvalid},
		{"message replay", activity + frame(2, brokerFrameActivity, message, nil), "MESSAGE_REPLAY", errProviderBrokerResponseInvalid},
		{"tool lifecycle", frame(1, brokerFrameActivity, tool, nil) + frame(2, brokerFrameActivity, tool, nil), "TOOL_LIFECYCLE", errProviderBrokerResponseInvalid},
		{"terminal schema", frame(1, brokerFrameTerminal, nil, nil), "TERMINAL_SCHEMA", errProviderBrokerResponseInvalid},
		{"diagnostic tuple", frame(1, brokerFrameTerminal, nil, &brokerResponse{Failure: providerBrokerFailureProvider, Diagnostic: bound}), "DIAGNOSTIC_TUPLE", errProviderBrokerResponseInvalid},
		{"archive", frame(1, brokerFrameTerminal, nil, &brokerResponse{Result: Result{SessionID: "PRIVATE_SENTINEL"}}), "ARCHIVE_PROOF", errProviderBrokerResponseInvalid},
		{"post terminal", end + end, "POST_TERMINAL", errProviderBrokerResponseInvalid},
		{"absent terminal", activity, "TERMINAL_ABSENT", errProviderBrokerFailed},
		{"terminal result", end, "TERMINAL_RESULT", errProviderBrokerFailed},
		{"stream bound", strings.Repeat("x", maximumBrokerFrameBytes+1), "STREAM_READ", errProviderBrokerFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := readProviderBrokerResponse(strings.NewReader(test.wire))
			if !errors.Is(err, test.cause) || BrokerResponseFailureCategory(err) != test.category || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
				t.Fatalf("failure category=%s sentinel=%t", BrokerResponseFailureCategory(err), errors.Is(err, test.cause))
			}
		})
	}
	_, err := readProviderBrokerResponse(bytes.NewReader(nil), nil, nil)
	if !errors.Is(err, errProviderBrokerResponseInvalid) || BrokerResponseFailureCategory(err) != "CALLBACKS" {
		t.Fatal("callback arity category lost")
	}
	_, err = readProviderBrokerResponse(strings.NewReader(activity+frame(2, brokerFrameTerminal, nil, terminal)), func(runtimecontract.RuntimeActivity) error {
		return errors.Join(context.Canceled, errors.New("PRIVATE_SENTINEL"))
	})
	if BrokerResponseFailureCategory(err) != "ACTIVITY_DELIVERY" || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
		t.Fatal("activity delivery category lost or leaked")
	}
}
