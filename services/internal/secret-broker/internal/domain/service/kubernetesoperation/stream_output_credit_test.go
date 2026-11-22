package kubernetesoperation

import (
	"context"
	"testing"
	"time"
)

// TestOutputWindowCarriesActualMinimumCredit binds wire-eligible bytes to both owner admission and current native authorization.
// TestOutputWindowCarriesActualMinimumCredit связывает wire-eligible bytes с owner admission и current native authorization.
func TestOutputWindowCarriesActualMinimumCredit(t *testing.T) {
	for _, source := range []string{"native", "owner"} {
		t.Run(source, func(t *testing.T) {
			window, owner := outputWindowFixture(t)
			first, second := time.Now().Add(time.Second), time.Now().Add(2*time.Second)
			owner.admission.ExpiresAt = first
			deadline := first
			if source == "owner" {
				owner.admission.ExpiresAt = first
				deadline = second
			}
			if err := window.Stage(t.Context(), "STDOUT", []byte("actual-output")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithDeadline(t.Context(), deadline)
			defer cancel()
			output, err := window.Deliver(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer clear(output.Payload)
			if !output.AuthorizedUntil.Equal(first) || output.DeliveryOrdinal != 1 || !owner.nativeUntil.Equal(deadline) {
				t.Fatal("output escaped owner/native minimum credit")
			}
			if !window.frame.AuthorizedUntil.IsZero() || window.frame.DeliveryOrdinal != 0 {
				t.Fatal("fresh delivery credit became cached buffer authority")
			}
		})
	}
}

// TestOutputDeliveryRejectsUnboundWitnesses prevents stale/native-less or regressing owner credit from delivering bytes.
// TestOutputDeliveryRejectsUnboundWitnesses запрещает bytes при отсутствии native witness, просрочке либо регрессии owner credit.
func TestOutputDeliveryRejectsUnboundWitnesses(t *testing.T) {
	for _, scenario := range []string{"missing-native", "expired-native", "beyond-native", "beyond-five-seconds", "zero-ordinal", "excess-ordinal", "repeated-ordinal"} {
		t.Run(scenario, func(t *testing.T) {
			window, owner := outputWindowFixture(t)
			if err := window.Stage(t.Context(), "STDOUT", []byte("private-output")); err != nil {
				t.Fatal(err)
			}
			ctx := outputDeliveryContext(t)
			switch scenario {
			case "missing-native":
				ctx = t.Context()
			case "expired-native":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()
			case "beyond-native":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				defer cancel()
			case "beyond-five-seconds":
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(t.Context(), 20*time.Second)
				defer cancel()
				owner.admission.ExpiresAt = time.Now().Add(10 * time.Second)
			case "zero-ordinal":
				owner.beginHook = func() { owner.ordinal = 0 }
			case "excess-ordinal":
				owner.beginHook = func() { owner.ordinal = maximumOutputDeliveries + 1 }
			case "repeated-ordinal":
				first, err := window.Deliver(ctx)
				if err != nil {
					t.Fatal(err)
				}
				clear(first.Payload)
				owner.beginHook = func() { owner.ordinal = 1 }
			}
			frame, err := window.Deliver(ctx)
			if err == nil || len(frame.Payload) != 0 || frame.DeliveryOrdinal != 0 || !frame.AuthorizedUntil.IsZero() {
				t.Fatal("unbound delivery escaped")
			}
			if (scenario == "missing-native" || scenario == "expired-native") && owner.begins != 0 {
				t.Fatal("missing native credit reached owner")
			}
		})
	}
}
