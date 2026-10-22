package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// currentInstructionChunkBytes keeps UTF-8 fragments at a conservative 3000-byte bound below the existing pinned profile's approximately 4000-byte per-value truncation budget; it is not a universal limit for every provider version.
// currentInstructionChunkBytes ограничивает UTF-8 fragments консервативными 3000 байтами ниже примерно 4000-байтового per-value truncation budget прежнего pinned profile; это не универсальный предел всех provider versions.
const currentInstructionChunkBytes = 3000

// currentInstructionContext splits valid UTF-8 instructions at rune boundaries into ordered application fragments and adds exact byte/chunk counts and SHA-256 revision metadata. Empty text yields metadata only; the mapper does not impose a separate total-input bound, send a request or manufacture instruction authority.
// currentInstructionContext делит корректные UTF-8 instructions по rune boundaries на упорядоченные application fragments и добавляет точные byte/chunk counts и SHA-256 revision metadata. Пустой текст даёт лишь metadata; mapper не задаёт отдельный total-input bound, не отправляет запрос и не создаёт instruction authority.
func currentInstructionContext(instructions string) (map[string]any, error) {
	if !utf8.ValidString(instructions) {
		return nil, ErrRuntimeProfile
	}
	values := map[string]any{}
	remainder := instructions
	chunks := 0
	for len(remainder) > 0 {
		end := min(len(remainder), currentInstructionChunkBytes)
		for end < len(remainder) && !utf8.RuneStart(remainder[end]) {
			end--
		}
		values[fmt.Sprintf("kodex_runtime_instructions_%06d", chunks)] = map[string]any{
			"kind": "application", "value": remainder[:end],
		}
		remainder = remainder[end:]
		chunks++
	}
	digest := sha256.Sum256([]byte(instructions))
	identity, err := json.Marshal(struct {
		Schema string `json:"schema"`
		Bytes  int    `json:"bytes"`
		Chunks int    `json:"chunks"`
		SHA256 string `json:"sha256"`
	}{"kodex.current-runtime-instructions.v1", len(instructions), chunks, hex.EncodeToString(digest[:])})
	if err != nil {
		return nil, ErrRuntimeProfile
	}
	values["kodex_runtime_revision"] = map[string]any{"kind": "application", "value": runtimeInstructionRevisionPreamble + string(identity)}
	return values, nil
}
