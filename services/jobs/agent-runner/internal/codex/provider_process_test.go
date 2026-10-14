package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

const actualProcessUserAgent = "kodex-agent-runner/0.160.0 (HOST_PRIVATE_SENTINEL 12.0.0; x86_64) unknown (kodex-agent-runner; 1)"

func providerProcessInput() model.Input {
	input := diagnosticInput()
	input.OrganizationRef, input.ProjectRef, input.RunRef, input.NodeRef = "org_fixture123", "prj_fixture123", "run_fixture123", "nod_fixture123"
	input.RuntimeRevisionRef, input.RuntimeRevisionVersion = "rev_fixture123", 2
	input.ImageManifestDigest = "sha256:" + strings.Repeat("d", 64)
	input.ImageReference = "pull.fixture.invalid/role@" + input.ImageManifestDigest
	return input
}

func initializeProcessResponse(userAgent any) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"codexHome": "/private/provider", "platformFamily": "unix", "platformOs": "linux", "userAgent": userAgent})
	return raw
}

func TestInitializeObservesCurrentProviderProcessVersionWithoutRawMetadata(t *testing.T) {
	state := newProtocolState("")
	if state.initialize(initializeProcessResponse(actualProcessUserAgent), "/private/provider") != nil || state.processVersion != "0.160.0" {
		t.Fatal("actual server package prefix was not observed")
	}
	var wire []byte
	state.onActivity = func(activity runtimecontract.RuntimeActivity) error { wire, _ = json.Marshal(activity); return nil }
	input := providerProcessInput()
	if state.publishProviderProcess(input) != nil {
		t.Fatal("bound process observation was not published")
	}
	var activity runtimecontract.RuntimeActivity
	if json.Unmarshal(wire, &activity) != nil || activity.ProviderProcess == nil || !activity.ProviderProcess.Matches(input) {
		t.Fatal("typed process binding lost")
	}
	for _, private := range []string{"HOST_PRIVATE_SENTINEL", "/private/provider", "userAgent", "cliVersion", "clientInfo"} {
		if bytes.Contains(wire, []byte(private)) {
			t.Fatal("raw initialize metadata leaked")
		}
	}
	if state.initialize(initializeProcessResponse(strings.Replace(actualProcessUserAgent, "0.160.0", "0.161.0", 1)), "/private/provider") == nil || state.processVersion != "0.160.0" {
		t.Fatal("duplicate initialize replaced process identity")
	}
}

func TestInitializeRejectsHostileOrClientOnlyVersion(t *testing.T) {
	for _, ua := range []any{nil, 42, "kodex-agent-runner", "kodex-agent-runner/1 (Debian 12; x86_64) unknown (kodex-agent-runner; 0.160.0)",
		strings.Replace(actualProcessUserAgent, "0.160.0", "unknown", 1), strings.Replace(actualProcessUserAgent, "0.160.0", "00.160.0", 1),
		strings.Replace(actualProcessUserAgent, "kodex-agent-runner/", "codex_cli_rs/", 1), actualProcessUserAgent + " PRIVATE_SENTINEL", actualProcessUserAgent + "\n", strings.Repeat("x", 513),
		strings.Replace(actualProcessUserAgent, "unknown", "unknown\r\nAuthorization: PRIVATE_SENTINEL", 1), strings.Replace(actualProcessUserAgent, "0.160.0", "0.160.0+PRIVATE_SENTINEL", 1),
		strings.Replace(actualProcessUserAgent, "unknown", "неизвестно", 1), strings.Replace(actualProcessUserAgent, "; 1)", "; 0.160.0)", 1),
	} {
		state := newProtocolState("")
		if err := state.initialize(initializeProcessResponse(ua), "/private/provider"); err == nil || state.processVersion != "" || strings.Contains(err.Error(), "PRIVATE_SENTINEL") {
			t.Fatal("hostile initialize supplied process version or leaked metadata")
		}
	}
}

func TestProviderProcessBrokerRequiresCurrentWriterAndFirstExactActivity(t *testing.T) {
	input := providerProcessInput()
	value := runtimecontract.BindProviderProcessObservation(input, "0.160.0")
	stream := func(activities []runtimecontract.RuntimeActivity) []byte {
		var wire bytes.Buffer
		writer := &brokerFrameWriter{writer: &wire, input: &input}
		for _, activity := range activities {
			if writer.activity(activity) != nil {
				t.Fatal("fixture activity failed")
			}
		}
		if writeProviderBrokerFailureAtStage(writer, providerStageInitialize, errors.New("PRIVATE_SENTINEL")) != nil {
			t.Fatal("fixture terminal failed")
		}
		return wire.Bytes()
	}
	activity := runtimecontract.RuntimeActivity{ProviderProcess: &value}
	raw := stream([]runtimecontract.RuntimeActivity{activity})
	calls := 0
	_, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &input, providerWriterUID, func(actual runtimecontract.RuntimeActivity) error {
		calls++
		if actual.ProviderProcess == nil {
			t.Fatal("observation lost")
		}
		return nil
	})
	if err == nil || calls != 1 {
		t.Fatal("valid activity was lost or terminal failure became success")
	}
	for name, alter := range map[string]func(*model.Input){"attempt": func(v *model.Input) { v.Attempt++ }, "session": func(v *model.Input) { v.SessionRef = "ses_other123" }, "turn": func(v *model.Input) { v.TurnRef = "trn_other123" }, "image": func(v *model.Input) { v.ImageReference = "pull.fixture.invalid/other@" + v.ImageManifestDigest }, "revision": func(v *model.Input) { v.RuntimeRevisionVersion++ }, "binding": func(v *model.Input) { v.ExecutionBindingDigest = strings.Repeat("e", 64) }} {
		t.Run(name, func(t *testing.T) {
			other := input
			alter(&other)
			calls = 0
			_, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &other, providerWriterUID, func(runtimecontract.RuntimeActivity) error { calls++; return nil })
			if !errors.Is(err, errProviderBrokerResponseInvalid) || calls != 0 {
				t.Fatal("foreign activity reached callback")
			}
		})
	}
	for _, uid := range []uint32{0, 10001} {
		if _, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), &input, uid); !errors.Is(err, errProviderBrokerResponseInvalid) {
			t.Fatal("foreign provider peer accepted")
		}
	}
	if _, err := readBoundProviderBrokerResponse(bytes.NewReader(raw), nil, providerWriterUID); !errors.Is(err, errProviderBrokerResponseInvalid) {
		t.Fatal("unbound observation accepted")
	}
	for _, activities := range [][]runtimecontract.RuntimeActivity{{activity, activity}, {{Message: &runtimecontract.RuntimeAgentMessage{ItemID: "message-first", Phase: "COMMENTARY", Revision: 1, Text: "safe"}}, activity}} {
		if _, err := readBoundProviderBrokerResponse(bytes.NewReader(stream(activities)), &input, providerWriterUID); !errors.Is(err, errProviderBrokerResponseInvalid) {
			t.Fatal("duplicate/late process observation accepted")
		}
	}
}
