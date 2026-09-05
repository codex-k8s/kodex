package prompt

import (
	"strings"
	"testing"
)

func TestContinuationDiffPinsAllChangesAndTurnIdentity(t *testing.T) {
	previous, current := map[string][]RuntimeDescriptor{}, map[string][]RuntimeDescriptor{}
	for _, component := range runtimeComponentOrder {
		previous[component] = []RuntimeDescriptor{}
		current[component] = []RuntimeDescriptor{}
	}
	previous["MODEL"] = []RuntimeDescriptor{{Value: "old-model"}}
	current["MODEL"] = []RuntimeDescriptor{{Value: "new-model"}}
	current["REASONING"] = []RuntimeDescriptor{{Value: "high"}}
	identity := RuntimeDiff{PreviousRevisionRef: "rrev_previous", CurrentRevisionRef: "rrev_current", SessionRef: "ses_example", TurnRef: "turn_example", Attempt: 1}
	diff, err := CompareRuntimeContexts(previous, current, identity)
	if err != nil || len(diff.Changes) != 2 || diff.Changes[0].Component != "MODEL" || diff.Changes[1].Component != "REASONING" || !validDigest(diff.Digest) {
		t.Fatalf("invalid typed diff: %#v err=%v", diff, err)
	}
	identity.Attempt++
	changed, err := CompareRuntimeContexts(previous, current, identity)
	if err != nil || changed.Digest == diff.Digest {
		t.Fatal("attempt not bound")
	}
	current["CREDENTIAL"] = []RuntimeDescriptor{{Value: "forbidden"}}
	if _, err := CompareRuntimeContexts(previous, current, identity); err == nil {
		t.Fatal("unknown/private component accepted")
	}
	delete(current, "CREDENTIAL")
	current["IMAGE"] = []RuntimeDescriptor{{Digest: strings.Repeat("X", 64)}}
	if _, err := CompareRuntimeContexts(previous, current, identity); err == nil {
		t.Fatal("malformed digest accepted")
	}
}
