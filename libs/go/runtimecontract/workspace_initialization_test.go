package runtimecontract

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// initializationFixture provides only synthetic commitments, never authority or private bytes.
// initializationFixture задаёт синтетические commitments без authority либо private bytes.
func initializationFixture() (RunnerInput, InitializationFailure) {
	input := RunnerInput{RuntimeRevisionDigest: strings.Repeat("a", 64), InputDigest: strings.Repeat("b", 64), ExecutionBindingDigest: strings.Repeat("c", 64), SessionRef: "ses_fixture123", TurnRef: "trn_fixture123", Attempt: 2}
	return input, NewInitializationObservation(input, "ARTIFACT_TRANSFER", "AUTHORITY_REJECTED")
}

// TestInitializationRecordStrictWireAndExactBinding exercises the actual bounded codec and tuple.
// TestInitializationRecordStrictWireAndExactBinding проверяет настоящий bounded codec и tuple.
func TestInitializationRecordStrictWireAndExactBinding(t *testing.T) {
	input, record := initializationFixture()
	raw, err := EncodeInitializationFailure(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeInitializationFailure(raw)
	if err != nil || !decoded.Matches(input) || InitializationFailureSummary(decoded.Stage, decoded.Reason) != "i18n:RUNTIME_INIT_ARTIFACT_TRANSFER_AUTHORITY_REJECTED" {
		t.Fatal("valid record lost exact provenance")
	}
	for _, change := range []func(*InitializationFailure){
		func(v *InitializationFailure) { v.RuntimeRevisionDigest = strings.Repeat("d", 64) },
		func(v *InitializationFailure) { v.InputDigest = strings.Repeat("d", 64) },
		func(v *InitializationFailure) { v.ExecutionBindingDigest = strings.Repeat("d", 64) },
		func(v *InitializationFailure) { v.SessionRef = "ses_other123" },
		func(v *InitializationFailure) { v.TurnRef = "trn_other123" },
		func(v *InitializationFailure) { v.Attempt++ },
	} {
		copy := record
		change(&copy)
		if copy.Matches(input) {
			t.Fatal("foreign tuple accepted")
		}
	}
	for _, bad := range [][]byte{
		nil, []byte(`{}`), []byte(`null`), append(append([]byte{}, raw...), []byte(`{}`)...),
		bytes.Replace(raw, []byte(`"stage":"ARTIFACT_TRANSFER"`), []byte(`"stage":"PRIVATE_STAGE"`), 1),
		bytes.Replace(raw, []byte(`"reason":"AUTHORITY_REJECTED"`), []byte(`"reason":"PRIVATE_CAUSE"`), 1),
		bytes.Replace(raw, []byte(`"stage":"ARTIFACT_TRANSFER"`), []byte(`"stage":"ARTIFACT_TRANSFER","stage":"ARTIFACT_TRANSFER"`), 1),
		append([]byte(`{"private":"PRIVATE_SENTINEL",`), raw[1:]...),
		append(bytes.Repeat([]byte(" "), MaximumInitializationFailureBytes), raw...),
	} {
		if _, err := DecodeInitializationFailure(bad); err == nil {
			t.Fatal("malformed record accepted")
		}
	}
	if ValidInitializationFailureSummary("i18n:RUNTIME_INIT_ARTIFACT_TRANSFER_PRIVATE") || InitializationFailureSummary("PRIVATE", "PRIVATE") != "i18n:RUNTIME_INIT_UNKNOWN_UNKNOWN" {
		t.Fatal("summary namespace not closed")
	}
	for stage, reasons := range initializationReasons {
		for _, reason := range reasons {
			if !ValidInitializationFailureSummary(InitializationFailureSummary(stage, reason)) {
				t.Fatal("valid pair has no owner summary")
			}
		}
	}
}

// TestInitializationErrorPreservesCauseWithoutFormattingIt covers every fmt escape route.
// TestInitializationErrorPreservesCauseWithoutFormattingIt проверяет все fmt escape routes.
func TestInitializationErrorPreservesCauseWithoutFormattingIt(t *testing.T) {
	cause := errors.New("PRIVATE_SENTINEL")
	err := InitializationError("INPUT_MANIFEST", "INPUT_INVALID", cause)
	if !errors.Is(err, cause) {
		t.Fatal("original error identity lost")
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		if strings.Contains(fmt.Sprintf(format, err), "PRIVATE_SENTINEL") {
			t.Fatal("private cause leaked")
		}
	}
	if stage, reason := InitializationErrorDetails(err); stage != "INPUT_MANIFEST" || reason != "INPUT_INVALID" {
		t.Fatal("typed pair lost")
	}
	if stage, reason := InitializationErrorDetails(errors.New("AUTHORITY_REJECTED")); stage != "UNKNOWN" || reason != "UNKNOWN" {
		t.Fatal("untyped text became cause evidence")
	}
}
