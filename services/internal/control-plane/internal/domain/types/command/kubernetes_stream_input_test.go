package command

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
)

// TestKubernetesStreamInputPayloadBoundary covers binary stdin/tunnel bytes, explicit EOF, exact typed resize and forbidden artifact/control sources before storage admission.
// TestKubernetesStreamInputPayloadBoundary проверяет binary stdin/tunnel bytes, explicit EOF, exact typed resize и запрещённые artifact/control sources до storage admission.
func TestKubernetesStreamInputPayloadBoundary(t *testing.T) {
	encodeTunnel := func(action managedkubernetes.PortForwardConnectionAction, body []byte) string {
		raw, err := managedkubernetes.EncodePortForwardConnectionFrame(managedkubernetes.PortForwardConnectionFrame{Action: action, ConnectionRef: "kpf_AAAAAAAAAAAAAAAAAAAAAAAA", Generation: 1, Body: body})
		if err != nil {
			t.Fatal("canonical tunnel fixture rejected")
		}
		return string(raw)
	}
	cases := []struct {
		name, channel, payload string
		eof, valid             bool
	}{
		{"stdin", "STDIN", "private-input", false, true},
		{"binary stdin", "STDIN", "\x00\xff\x80", false, true},
		{"stdin EOF", "STDIN", "", true, true},
		{"tunnel", "PORT_FORWARD", encodeTunnel(managedkubernetes.PortForwardConnectionData, []byte{0xff, 0x80}), false, true},
		{"tunnel half close", "PORT_FORWARD", encodeTunnel(managedkubernetes.PortForwardConnectionHalfClose, nil), false, true},
		{"tunnel close", "PORT_FORWARD", encodeTunnel(managedkubernetes.PortForwardConnectionClose, nil), false, true},
		{"legacy tunnel", "PORT_FORWARD", "\x00\xff\x80", false, false},
		{"legacy tunnel EOF", "PORT_FORWARD", "", true, false},
		{"resize", "TERMINAL_RESIZE", `{"columns":1024,"rows":512}`, false, true},
		{"empty stdin", "STDIN", "", false, false},
		{"mixed EOF", "STDIN", "input", true, false},
		{"artifact source", "FILE_CONTENT", "input", false, false},
		{"raw control", "CONTROL", "input", false, false},
		{"output channel", "STDOUT", "input", false, false},
		{"channel alias", "stdin", "input", false, false},
		{"size", "STDIN", strings.Repeat("x", 32769), false, false},
		{"resize limit", "TERMINAL_RESIZE", `{"columns":1025,"rows":10}`, false, false},
		{"resize zero", "TERMINAL_RESIZE", `{"columns":80,"rows":0}`, false, false},
		{"resize omitted", "TERMINAL_RESIZE", `{"columns":80}`, false, false},
		{"resize null", "TERMINAL_RESIZE", `{"columns":80,"rows":null}`, false, false},
		{"resize duplicate", "TERMINAL_RESIZE", `{"columns":80,"rows":10,"rows":20}`, false, false},
		{"resize alias", "TERMINAL_RESIZE", `{"Columns":80,"rows":10}`, false, false},
		{"resize unknown", "TERMINAL_RESIZE", `{"columns":80,"rows":10,"command":"input"}`, false, false},
		{"resize UTF8", "TERMINAL_RESIZE", "{\"columns\":80,\"rows\":10,\"\xff\":0}", false, false},
		{"resize EOF", "TERMINAL_RESIZE", `{"columns":80,"rows":10}`, true, false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			digest := sha256.Sum256([]byte(item.payload))
			input := KubernetesStreamInputSubmit{Channel: item.channel, Payload: []byte(item.payload), PayloadDigest: hex.EncodeToString(digest[:]), EndOfInput: item.eof}
			if input.ValidPayload() != item.valid {
				t.Fatal("public input source validation mismatch")
			}
		})
	}
	input := KubernetesStreamInputSubmit{Channel: "STDIN", Payload: []byte("PRIVATE_INPUT_SENTINEL"), PayloadDigest: strings.Repeat("0", 64)}
	if input.ValidPayload() {
		t.Fatal("mismatched payload digest admitted")
	}
	diagnostic, _ := json.Marshal(input)
	if strings.Contains(fmt.Sprintf("%v %#v", input, &input), "PRIVATE_INPUT_SENTINEL") || strings.Contains(string(diagnostic), "PRIVATE_INPUT_SENTINEL") {
		t.Fatal("private input leaked into diagnostic projection")
	}
}
