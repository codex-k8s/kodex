package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestEmailRejectsHistoricalPackageBeforeBridgeEffect(t *testing.T) {
	adapter := testAdapter(t)
	calls := 0
	emailFixture(t, adapter, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"status":"accepted","message_id":"fixture"}`)
	})
	current := adapter.definitions["email"]
	request := invocationRequest(t, current, "email.message.send", map[string]any{"to":"recipient@example.test","subject":"Fixture","body_text":"Bounded fixture"}, nil)
	if _, err := adapter.Execute(t.Context(), request); err != nil || calls != 1 { t.Fatalf("current canonical email failed: %v", err) }
	for _, version := range []string{"1.4.0","1.4.1"} {
		bad := request
		old := current
		old.Metadata.Version = version
		bad.DefinitionPackage, _ = json.Marshal(old)
		bad.DefinitionVersion = version
		if _, err := adapter.Execute(t.Context(), bad); err == nil || calls != 1 { t.Fatal("historical revision reached bridge") }
	}
}
