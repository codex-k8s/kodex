package httptransport

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func TestPublishedRunMessagePreservesLiteralText(t *testing.T) {
	for _, text := range []string{"RUN_STATE_CANCELLED", "i18n:PRIVATE_TEST", "Проверяю текущие инструменты."} {
		projection, err := ProtoMap(&cp.RunMessage{Ref: "msg_fixture01", Phase: cp.RunMessagePhase_RUN_MESSAGE_PHASE_COMMENTARY, Revision: 1, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		LocalizeSafeErrors(projection, func(string) string { return "must-not-localize" })
		if projection["text"] != text || projection["phase"] != "COMMENTARY" || projection["revision"] != float64(1) {
			t.Fatalf("published message changed: %#v", projection)
		}
	}
}
