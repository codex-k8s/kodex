package platform

import (
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestManagedBindingExpectationIsExplicitAndClosed requires either explicit absence or an exact
// revision/version expectation, rejecting mixed, missing, unsafe-integer and malformed revision
// inputs.
// TestManagedBindingExpectationIsExplicitAndClosed требует явное отсутствие либо точное ожидание
// ревизии и версии, отклоняя смешанные, отсутствующие параметры, небезопасное целое и неверный ref
// ревизии.
func TestManagedBindingExpectationIsExplicitAndClosed(t *testing.T) {
	for _, test := range []struct {
		input entity.ManagedConfigurationConsumer
		valid bool
	}{
		{entity.ManagedConfigurationConsumer{ExpectedAbsent: true}, true},
		{entity.ManagedConfigurationConsumer{RevisionRef: "mrev_previous", Version: 1}, true},
		{entity.ManagedConfigurationConsumer{}, false},
		{entity.ManagedConfigurationConsumer{ExpectedAbsent: true, Version: 1}, false},
		{entity.ManagedConfigurationConsumer{ExpectedAbsent: true, RevisionRef: "mrev_previous"}, false},
		{entity.ManagedConfigurationConsumer{RevisionRef: "mrev_previous", Version: -1}, false},
		{entity.ManagedConfigurationConsumer{RevisionRef: "mrev_previous", Version: 9007199254740992}, false},
		{entity.ManagedConfigurationConsumer{RevisionRef: "other_previous", Version: 1}, false},
		{entity.ManagedConfigurationConsumer{RevisionRef: "mrev_bad\nref", Version: 1}, false},
	} {
		if validManagedBindingExpectation(test.input) != test.valid {
			t.Fatalf("unexpected binding expectation validation: %+v", test.input)
		}
	}
}
