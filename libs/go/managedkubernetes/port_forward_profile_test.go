package managedkubernetes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

// TestPortForwardProfilePreservesOrderedMappings checks exact slots and rejects ambiguous or unsupported options before native I/O.
// TestPortForwardProfilePreservesOrderedMappings проверяет exact slots и отклоняет ambiguous/unsupported options до native I/O.
func TestPortForwardProfilePreservesOrderedMappings(t *testing.T) {
	input := OperationInput{Version: "v1", Resource: "pods", Options: json.RawMessage(`{"ports":[{"port":19090,"targetPort":9090},{"port":18080,"targetPort":8080},{"port":28080,"targetPort":8080}],"timeoutSeconds":30}`)}
	profile, err := DecodePortForwardProfile(input)
	if err != nil || len(profile.Ports) != 3 || profile.Ports[0].TargetPort != 9090 || profile.Ports[2].TargetPort != 8080 {
		t.Fatal("ordered mappings were changed")
	}
	for _, raw := range []string{
		`{"ports":[],"timeoutSeconds":30}`,
		`{"ports":[{"port":1,"targetPort":0}],"timeoutSeconds":30}`,
		`{"ports":[{"port":1,"targetPort":1,"protocol":"UDP"}],"timeoutSeconds":30}`,
		`{"ports":[{"port":1,"targetPort":1},{"port":1,"targetPort":2}],"timeoutSeconds":30}`,
		`{"ports":[{"port":1,"targetPort":1,"host":"example.invalid"}],"timeoutSeconds":30}`,
		`{"ports":[{"port":1,"targetPort":1}],"timeoutSeconds":301}`,
		`{"ports":[{"port":1,"targetPort":1}],"ports":[],"timeoutSeconds":30}`,
	} {
		input.Options = json.RawMessage(raw)
		if _, err := DecodePortForwardProfile(input); err == nil {
			t.Fatal("invalid profile accepted")
		}
	}
	profile.Ports = make([]ForwardPort, MaximumForwardPorts+1)
	if profile.Validate() == nil {
		t.Fatal("unbounded slots accepted")
	}
}

// TestPortForwardFrameBindsSlotAndBudget distinguishes a valid syntactic slot from one authorized by an exact profile.
// TestPortForwardFrameBindsSlotAndBudget различает syntactically valid slot и slot, разрешённый exact profile.
func TestPortForwardFrameBindsSlotAndBudget(t *testing.T) {
	for _, test := range []struct {
		payload []byte
		eof     bool
		slots   int
		valid   bool
	}{
		{[]byte{0, 255}, false, 1, true}, {[]byte{1, 255}, false, 1, false},
		{[]byte{15, 255}, false, 16, true}, {[]byte{16, 255}, false, 16, false},
		{[]byte{0}, false, 1, false}, {nil, false, 1, false},
		{nil, true, 1, true}, {[]byte{0, 255}, true, 1, false},
		{make([]byte, MaximumFrameBytes), false, 1, true}, {make([]byte, MaximumFrameBytes+1), false, 1, false},
		{[]byte{0, 255}, false, 0, false}, {[]byte{0, 255}, false, 17, false},
	} {
		if (ValidatePortForwardFrame(test.payload, test.eof, test.slots) == nil) != test.valid {
			t.Fatal("slot or budget boundary changed")
		}
	}
	for _, payload := range [][]byte{{0}, {16, 255}, {0, 255}} {
		digest := sha256.Sum256(payload)
		err := ValidatePublicStreamInput("PORT_FORWARD", payload, hex.EncodeToString(digest[:]), false)
		if err == nil {
			t.Fatal("public input bypassed tunnel framing")
		}
	}
}
