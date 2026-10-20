package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// Codex0.160 ограничивает value1000 approximate tokens (4000bytes по
// utils/string/truncate.rs). Каждый UTF-8 chunk меньше точного byte-предела.
const currentInstructionChunkBytes = 3000

// Полные bytes поступают в native developer fragments каждого turn, без
// зависимости от sticky generic instructions и catalog collaboration presets.
// Чанки не являются самостоятельными источниками: это один точный snapshot.
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
