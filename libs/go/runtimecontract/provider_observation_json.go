package runtimecontract

import (
	"bytes"
	"encoding/json"
	"io"
)

// decodeCanonicalProviderObservation applies common wire checks before the caller
// commits an observation. destination is a local wire alias without UnmarshalJSON;
// valid checks that alias without changing the original receiver. Both consumers
// retain the same 4096-byte/depth-two limits, unique keys, EOF and canonical order.
// decodeCanonicalProviderObservation проверяет wire-формат до фиксации наблюдения.
// destination — локальный wire-alias без UnmarshalJSON; valid проверяет его,
// не изменяя исходного получателя. Оба потребителя сохраняют предел 4096 байт,
// глубину 2, уникальность ключей, полный EOF и канонический порядок полей.
func decodeCanonicalProviderObservation(raw []byte, destination any, valid func() bool) bool {
	if len(raw) > 4096 || boundedJSONUnique(json.NewDecoder(bytes.NewReader(raw)), 0, 2) != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil || decoder.Decode(new(any)) != io.EOF || !valid() {
		return false
	}
	canonical, err := json.Marshal(destination)
	var compact bytes.Buffer
	return err == nil && json.Compact(&compact, raw) == nil && bytes.Equal(compact.Bytes(), canonical)
}
