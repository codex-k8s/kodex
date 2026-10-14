package websockettransport

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestArtifactRevisionSignalHasExactClosedImmutableTuple(t *testing.T) {
	fixture := []byte(`{"eventId":"d561fbb0-02c0-4be7-af7c-5998925632bd","eventName":"ARTIFACT_CHANGED","eventVersion":1,"occurredAt":"2026-08-22T12:00:00Z","organizationRef":"org_example0001","projectRef":"prj_example0001","aggregateRef":"art_example0001","aggregateVersion":3,"sequence":9,"correlationRef":"d1713d76-566d-43c3-a0b2-0ca2307869d0","data":{"kind":"ARTIFACT","safeSummary":"i18n:ARTIFACT_REVISION_CREATED","artifactRevision":{"ref":"arv_example0001","revision":2,"digest":"sha256:` + strings.Repeat("a", 64) + `"}}}`)
	if signal, ok := decodePlatformSignal(fixture, "org_example0001"); !ok || signal.Kind != "ARTIFACT" || signal.Sequence != 9 {
		t.Fatal("exact immutable artifact revision signal rejected")
	}
	for _, tamper := range []func(map[string]any, map[string]any){
		func(_ map[string]any, revision map[string]any) { revision["revision"] = 0 },
		func(_ map[string]any, revision map[string]any) {
			revision["digest"] = "sha256:" + strings.Repeat("G", 64)
		},
		func(_ map[string]any, revision map[string]any) { revision["digest"] = "sha256:wrong" },
		func(_ map[string]any, revision map[string]any) { revision["ref"] = "" },
		func(_ map[string]any, revision map[string]any) { revision["objectKey"] = "private-locator" },
		func(_ map[string]any, revision map[string]any) { revision["content"] = "private-body" },
		func(envelope map[string]any, _ map[string]any) {
			envelope["eventName"] = "RUN_CHANGED"
			envelope["data"].(map[string]any)["kind"] = "RUN"
		},
	} {
		var envelope map[string]any
		if json.Unmarshal(fixture, &envelope) != nil {
			t.Fatal("fixture malformed")
		}
		revision := envelope["data"].(map[string]any)["artifactRevision"].(map[string]any)
		tamper(envelope, revision)
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := decodePlatformSignal(encoded, "org_example0001"); ok {
			t.Fatal("malformed or cross-kind immutable tuple accepted")
		}
	}
}
