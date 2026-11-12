package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TestNativeBrokerDeliversProgressAndMeasurements exercises the actual framed reader
// through recovery, compaction and both measurement sources. Observations preserve
// order and unknown usage without implying a successful terminal result.
// TestNativeBrokerDeliversProgressAndMeasurements проверяет настоящий framed reader
// для восстановления, compaction и обоих источников измерений. Наблюдения сохраняют
// порядок и неизвестный usage, не превращая terminal failure в успех.
func TestNativeBrokerDeliversProgressAndMeasurements(t *testing.T) {
	input := nativeHandshakeInput()
	observation := runtimecontract.BindProviderProcessObservation(input, runtimecontract.OpenCodeVersion)
	activities := []runtimecontract.RuntimeActivity{
		{Progress: nativeSessionRecovering},
		{ProviderProcess: &observation},
		{Progress: "MODEL_REQUEST_RUNNING"},
		{ModelMeasurement: &runtimecontract.ModelMeasurement{Ref: "request_synthetic1", Source: "HTTP_REQUEST", Phase: "NORMAL", Outcome: "FAILED", Completeness: "UNKNOWN"}},
		{Progress: "MODEL_CONTEXT_COMPACTING"},
		{ModelMeasurement: &runtimecontract.ModelMeasurement{Ref: "step_synthetic1", Source: "NATIVE_STEP", Phase: "COMPACTION", Outcome: "COMPLETED", Completeness: "UNKNOWN", Usage: &runtimecontract.TokenUsage{Completeness: runtimecontract.UsageUnknown}}},
		{Progress: "MODEL_REQUEST_RUNNING"},
	}
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
		t.Fatal("native observations changed or terminal failure was lost", err)
	}
}

// TestNativeBrokerRejectsInvalidObservations bypasses producer validation to prove
// the reader rejects unknown progress and ambiguous unions before callback delivery.
// TestNativeBrokerRejectsInvalidObservations обходит producer validation и доказывает,
// что reader отклоняет неизвестную фазу и неоднозначное объединение до callback.
func TestNativeBrokerRejectsInvalidObservations(t *testing.T) {
	for _, activity := range []runtimecontract.RuntimeActivity{
		{Progress: "UNKNOWN_PHASE"},
		{Progress: "MODEL_REQUEST_RUNNING", ModelMeasurement: &runtimecontract.ModelMeasurement{Ref: "request_synthetic1", Source: "HTTP_REQUEST", Phase: "NORMAL", Outcome: "FAILED", Completeness: "UNKNOWN"}},
	} {
		var wire bytes.Buffer
		if err := json.NewEncoder(&wire).Encode(brokerFrame{Version: providerBrokerVersion, Sequence: 1, Kind: brokerFrameActivity, Activity: &activity}); err != nil {
			t.Fatal(err)
		}
		called := false
		_, err := readBoundProviderBrokerResponse(&wire, &model.Input{Provider: "opencode"}, providerWriterUID, func(runtimecontract.RuntimeActivity) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("invalid native observation reached the callback")
		}
	}
}

// TestNativeBrokerStreamsBeyondLegacyLifetimeBudgets exercises backpressure across over64 MiB and7000 frames.
// The consumer stores no transcript; this transport-only test supplies no owner or engine acceptance.
// TestNativeBrokerStreamsBeyondLegacyLifetimeBudgets проверяет backpressure при более64 МиБ и7000 frames.
// Consumer не хранит transcript; transport-only test не доказывает owner или engine acceptance.
func TestNativeBrokerStreamsBeyondLegacyLifetimeBudgets(t *testing.T) {
	reader, output := io.Pipe()
	defer reader.Close()
	input := model.Input{Provider: "opencode"}
	done := make(chan error, 1)
	go func() {
		writer := &brokerFrameWriter{writer: output, input: &input}
		text := strings.Repeat("synthetic ", 1024)
		var err error
		for index := 0; index < 7000 && err == nil; index++ {
			err = writer.activity(runtimecontract.RuntimeActivity{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "native-" + strconv.Itoa(index), Phase: runtimecontract.RuntimeMessageCommentary, Revision: 1, Text: text}})
		}
		if err == nil {
			err = writer.finish(brokerResponse{OK: true, Result: Result{Outcome: "FAILED"}})
		}
		_ = output.CloseWithError(err)
		done <- err
	}()
	count := 0
	result, err := readBoundProviderBrokerResponse(reader, &input, providerWriterUID, func(activity runtimecontract.RuntimeActivity) error {
		if activity.Message != nil {
			count++
		}
		return nil
	})
	if err != nil || count != 7000 || result.Outcome != "FAILED" {
		t.Fatal("native stream hit a hidden lifetime budget", count, err)
	}
	if err := <-done; err != nil {
		t.Fatal("native producer lost backpressure", err)
	}
}
