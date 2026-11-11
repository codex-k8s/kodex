package codex

import (
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

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
