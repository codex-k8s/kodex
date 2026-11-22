package grpc

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/services/internal/secret-broker/internal/domain/types/value"
)

// TestManagedKubernetesExchangeOutputCreditRequiresActualFrameAdmission rejects missing, expired and excessive frame credit before serialization.
// TestManagedKubernetesExchangeOutputCreditRequiresActualFrameAdmission отклоняет missing/expired/excessive frame credit до serialization.
func TestManagedKubernetesExchangeOutputCreditRequiresActualFrameAdmission(t *testing.T) {
	for _, variant := range []string{"exact", "missing", "expired", "excessive", "empty", "ordinal-zero", "ordinal-excessive", "ordinal-four"} {
		t.Run(variant, func(t *testing.T) {
			payload := []byte("actual-output")
			digest := sha256.Sum256(payload)
			expiry := time.Now().Add(time.Second)
			result := value.KubernetesStreamExchange{State: "ACTIVE", RemoteOutcome: "RUNNING", Output: []value.KubernetesStreamFrame{{Sequence: 1, Channel: "STDOUT", Payload: payload, PayloadDigest: hex.EncodeToString(digest[:]), DeliveryOrdinal: 1, AuthorizedUntil: expiry}}}
			switch variant {
			case "missing":
				result.Output[0].AuthorizedUntil = time.Time{}
			case "expired":
				result.Output[0].AuthorizedUntil = time.Now().Add(-time.Second)
			case "excessive":
				result.Output[0].AuthorizedUntil = time.Now().Add(time.Minute)
			case "ordinal-zero":
				result.Output[0].DeliveryOrdinal = 0
			case "ordinal-excessive":
				result.Output[0].DeliveryOrdinal = 5
			case "ordinal-four":
				result.Output[0].DeliveryOrdinal = 4
			case "empty":
				result.Output = nil
			}
			response, err := kubernetesExchangeToProto(result)
			if variant == "exact" || variant == "ordinal-four" {
				if err != nil || response.AuthorizedUntil == nil || !response.AuthorizedUntil.AsTime().Equal(expiry) || response.Output[0].DeliveryOrdinal != result.Output[0].DeliveryOrdinal {
					t.Fatal("actual frame credit was not preserved")
				}
			} else if variant == "empty" {
				if err != nil || response.AuthorizedUntil != nil {
					t.Fatal("empty output fabricated delivery credit")
				}
			} else if err == nil {
				t.Fatal("invalid output credit became a successful response")
			}
		})
	}
}
