package codex

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// nativeHandshakeInput supplies exact synthetic execution pins and a prior native checkpoint.
// It grants no authority and does not replace real checkpoint import qualification.
// nativeHandshakeInput задаёт точные synthetic execution pins и прежний native checkpoint.
// Он не выдаёт полномочий и не заменяет квалификацию настоящего import checkpoint.
func nativeHandshakeInput() model.Input {
	return model.Input{Provider: "opencode", OrganizationRef: "org_fixture123", ProjectRef: "prj_fixture123", RunRef: "run_fixture123", NodeRef: "nod_fixture123",
		SessionRef: "ses_fixture123", TurnRef: "trn_fixture123", Attempt: 1, RuntimeRevisionRef: "rev_fixture123", RuntimeRevisionVersion: 2,
		RuntimeRevisionDigest: strings.Repeat("a", 64), ImageReference: "pull.example.invalid/role@sha256:" + strings.Repeat("b", 64), ImageManifestDigest: "sha256:" + strings.Repeat("b", 64),
		InputDigest: strings.Repeat("c", 64), ExecutionBindingDigest: strings.Repeat("d", 64),
		NativeSession: &runtimecontract.NativeSessionCheckpoint{SessionID: "ses_native", Version: runtimecontract.OpenCodeVersion, Generation: 1,
			RelativePath: ".kodex/state/opencode/checkpoints/g1/ses_native.json", SHA256: strings.Repeat("e", 64), SizeBytes: 100}}
}

// TestNativeBrokerRestoredProcessHandshake reproduces the production order: recovery progress,
// exact process observation, then normal work. A failed terminal remains failed after delivery.
// TestNativeBrokerRestoredProcessHandshake воспроизводит порядок production: фаза восстановления,
// точное подтверждение процесса и обычная работа. Неуспешный итог не превращается в успешный.
func TestNativeBrokerRestoredProcessHandshake(t *testing.T) {
	input := nativeHandshakeInput()
	observation := runtimecontract.BindProviderProcessObservation(input, runtimecontract.OpenCodeVersion)
	activities := []runtimecontract.RuntimeActivity{{Progress: nativeSessionRecovering}, {ProviderProcess: &observation}, {Progress: "MODEL_REQUEST_RUNNING"}}
	var wire bytes.Buffer
	writer := brokerFrameWriter{writer: &wire, input: &input}
	for _, activity := range activities {
		if err := writer.activity(activity); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.finish(brokerResponse{OK: true, Result: Result{Outcome: "FAILED"}}); err != nil {
		t.Fatal(err)
	}
	var observed []runtimecontract.RuntimeActivity
	result, err := readBoundProviderBrokerResponse(&wire, &input, providerWriterUID, func(activity runtimecontract.RuntimeActivity) error {
		observed = append(observed, activity)
		return nil
	})
	if err != nil || !reflect.DeepEqual(observed, activities) || result.Outcome != "FAILED" {
		t.Fatal("restored native process handshake was rejected or changed outcome", BrokerResponseFailureCategory(err))
	}
}

// TestNativeBrokerHandshakeBoundaries checks the complete pre-process transition matrix against
// the real reader. Recovery can fail before startup, but cannot authorize work or a successful result.
// TestNativeBrokerHandshakeBoundaries проверяет полную матрицу переходов до процесса настоящим reader.
// Восстановление может отказать до startup, но не разрешает работу или успешный итог.
func TestNativeBrokerHandshakeBoundaries(t *testing.T) {
	for _, name := range []string{"cold", "cold-delayed", "missing-checkpoint", "corrupt-checkpoint", "wrong-uid", "wrong-provider", "foreign-process", "duplicate-recovery", "late-recovery", "intervening-progress", "duplicate-process", "restore-failed", "restore-false-success"} {
		t.Run(name, func(t *testing.T) {
			input := nativeHandshakeInput()
			observation := runtimecontract.BindProviderProcessObservation(input, runtimecontract.OpenCodeVersion)
			recovery := runtimecontract.RuntimeActivity{Progress: nativeSessionRecovering}
			process := runtimecontract.RuntimeActivity{ProviderProcess: &observation}
			progress := runtimecontract.RuntimeActivity{Progress: "MODEL_REQUEST_RUNNING"}
			activities := []runtimecontract.RuntimeActivity{recovery, process, progress}
			uid := uint32(providerWriterUID)
			outcome := "FAILED"
			accepted := false
			acceptedFrames := 0
			switch name {
			case "cold":
				input.NativeSession = nil
				activities = []runtimecontract.RuntimeActivity{process, progress}
				accepted, acceptedFrames = true, 2
			case "cold-delayed":
				input.NativeSession = nil
				activities = []runtimecontract.RuntimeActivity{progress, process}
				acceptedFrames = 1
			case "missing-checkpoint":
				input.NativeSession = nil
			case "corrupt-checkpoint":
				input.NativeSession.RelativePath = "../foreign.json"
			case "wrong-uid":
				uid++
			case "wrong-provider":
				input.Provider = "foreign"
			case "foreign-process":
				observation.ExecutionBindingDigest = strings.Repeat("f", 64)
				acceptedFrames = 1
			case "duplicate-recovery":
				activities = []runtimecontract.RuntimeActivity{recovery, recovery, process}
				acceptedFrames = 1
			case "late-recovery":
				activities = []runtimecontract.RuntimeActivity{process, recovery}
				acceptedFrames = 1
			case "intervening-progress":
				activities = []runtimecontract.RuntimeActivity{recovery, progress, process}
				acceptedFrames = 1
			case "duplicate-process":
				activities = []runtimecontract.RuntimeActivity{recovery, process, process}
				acceptedFrames = 2
			case "restore-failed":
				activities = []runtimecontract.RuntimeActivity{recovery}
				accepted, acceptedFrames = true, 1
			case "restore-false-success":
				activities = []runtimecontract.RuntimeActivity{recovery}
				outcome, acceptedFrames = "COMPLETED", 1
			}
			var wire bytes.Buffer
			encoder := json.NewEncoder(&wire)
			for index, activity := range activities {
				if err := encoder.Encode(brokerFrame{Version: providerBrokerVersion, Sequence: uint64(index + 1), Kind: brokerFrameActivity, Activity: &activity}); err != nil {
					t.Fatal(err)
				}
			}
			if err := encoder.Encode(brokerFrame{Version: providerBrokerVersion, Sequence: uint64(len(activities) + 1), Kind: brokerFrameTerminal, Terminal: &brokerResponse{OK: true, Result: Result{Outcome: outcome}}}); err != nil {
				t.Fatal(err)
			}
			called := 0
			_, err := readBoundProviderBrokerResponse(&wire, &input, uid, func(runtimecontract.RuntimeActivity) error { called++; return nil })
			if (err == nil) != accepted || called != acceptedFrames || !accepted && BrokerResponseFailureCategory(err) != string(brokerResponseProviderTuple) {
				t.Fatal("native handshake boundary mismatch", BrokerResponseFailureCategory(err), called)
			}
		})
	}
}
