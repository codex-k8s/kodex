package kubernetestarget

import (
	"errors"
	"strings"
	"testing"
)

// TestNativeExecStatusRequiresExplicitUnambiguousExit distinguishes the actual remote exit
// protocol from clean transport close, upstream errors and duplicate-field success spoofing.
// TestNativeExecStatusRequiresExplicitUnambiguousExit разделяет actual remote exit protocol,
// clean transport close, upstream errors и duplicate-field success spoofing.
func TestNativeExecStatusRequiresExplicitUnambiguousExit(t *testing.T) {
	for _, test := range []struct {
		raw  string
		code int32
	}{
		{`{"status":"Success","metadata":{}}`, 0},
		{`{"kind":"Status","apiVersion":"v1","status":"Success"}`, 0},
		{`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"137"}]}}`, 137},
	} {
		if code, err := decodeNativeExecStatus([]byte(test.raw)); err != nil || code != test.code {
			t.Fatal("explicit native exit receipt was not recognized")
		}
	}
	for _, raw := range []string{
		"", " ", "null", "[]", "{}", `{"status":"Success"} {}`,
		`{"status":"Failure","status":"Success"}`,
		`{"status":"Success","code":500}`,
		`{"status":"Success","code":null}`,
		`{"status":"Success","details":null}`,
		`{"status":"Success","reason":null}`,
		`{"Status":"Success"}`,
		`{"status":"Success","unexpected":"synthetic-private-diagnostic"}`,
		`{"status":"Success","apiVersion":"v1"}`,
		`{"status":"Success","details":{"causes":[]}}`,
		`{"status":"Failure","reason":"Forbidden","message":"synthetic-private-diagnostic"}`,
		`{"status":"Failure","reason":"NonZeroExitCode"}`,
		`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"0"}]}}`,
		`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"256"}]}}`,
		`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"01"}]}}`,
		`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"1","message":"0"}]}}`,
		`{"status":"Failure","reason":"NonZeroExitCode","details":{"causes":[{"reason":"ExitCode","message":"1"},{"reason":"ExitCode","message":"0"}]}}`,
		strings.Repeat(" ", maximumExecStatusBytes+1), string([]byte{0xff}),
	} {
		if _, err := decodeNativeExecStatus([]byte(raw)); !errors.Is(err, errExecStatusUnknown) || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatal("ambiguous or missing native exit became success or disclosed remote text")
		}
	}
}
