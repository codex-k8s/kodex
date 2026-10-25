package httptransport

import (
	"encoding/hex"
	"hash"
	"io"
)

// forwardHashedBody consumes an already bounded reader with a nonempty caller-owned buffer,
// initialized hash and nonnil sender. It counts bytes before checking declared/maximum bounds,
// hashes and sends independent copies, and preserves send-error precedence over read errors.
// Failure returns the observed count with no digest; success requires EOF at the exact declared
// size. Callers retain admission, read budget, cancellation and sentinel classification; this
// helper adds no retries, deadlines or zero-progress-read bound and does not log payloads.
// forwardHashedBody читает уже ограниченный reader с непустым буфером caller, готовым hash
// и ненулевым sender. Учитывает bytes до проверки declared/maximum, хеширует и передаёт
// независимые копии, сохраняя приоритет send-ошибки над read-ошибкой. Отказ возвращает
// наблюдённый count без digest; успех требует EOF на точном declared size. Caller сохраняет
// допуск, бюджет чтения, отмену и классификацию sentinel; helper не добавляет retries,
// deadlines или предел пустых чтений и не журналирует payload.
func forwardHashedBody(reader io.Reader, buffer []byte, digest hash.Hash, declaredSize, maximumSize int64,
	lengthMismatch, bodyReadError error, send func([]byte) error) (int64, string, error) {
	var received int64
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			received += int64(count)
			if received > declaredSize || received > maximumSize {
				return received, "", lengthMismatch
			}
			_, _ = digest.Write(buffer[:count])
			if err := send(append([]byte(nil), buffer[:count]...)); err != nil {
				return received, "", err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return received, "", bodyReadError
		}
	}
	if received != declaredSize {
		return received, "", lengthMismatch
	}
	return received, hex.EncodeToString(digest.Sum(nil)), nil
}
