package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// nativeCheckpointFixture supplies synthetic worker metadata for owner tests, not proof of native file custody or execution.
// nativeCheckpointFixture задаёт synthetic worker metadata для owner tests, не доказывая native file custody или execution.
func nativeCheckpointFixture(id string, generation int64, digest string, size int64) *runtimecontract.NativeSessionCheckpoint {
	if !strings.HasPrefix(id, "ses_") {
		id = "ses_" + strings.ReplaceAll(id, "-", "")
	}
	path, _ := runtimecontract.NativeSessionExportPath(id, generation)
	return &runtimecontract.NativeSessionCheckpoint{SessionID: id, Version: runtimecontract.OpenCodeVersion, Generation: generation, RelativePath: path, SHA256: digest, SizeBytes: size}
}

// nativeCheckpointForClaimFixture supplies the next synthetic stopped-writer receipt for an actual owner claim.
// It never creates a lease, changes authority or proves file custody; dedicated tests cover those boundaries.
// nativeCheckpointForClaimFixture задаёт следующую synthetic stopped-writer receipt настоящего owner claim.
// Не создаёт lease, не меняет authority и не доказывает file custody; эти границы проверяются отдельно.
func nativeCheckpointForClaimFixture(lease map[string]any) *runtimecontract.NativeSessionCheckpoint {
	digest := sha256.Sum256([]byte(stringMap(lease, "sessionRef")))
	id := "ses_" + hex.EncodeToString(digest[:16])
	generation := int64(1)
	if prior := lease["nativeSession"]; prior != nil {
		raw, err := json.Marshal(prior)
		if err != nil {
			panic("invalid synthetic native claim")
		}
		checkpoint, err := runtimecontract.DecodeNativeSessionCheckpoint(raw)
		if err != nil {
			panic("invalid synthetic native checkpoint")
		}
		id = checkpoint.SessionID
		generation = checkpoint.Generation + 1
	}
	return nativeCheckpointFixture(id, generation, strings.Repeat("a", 64), 128)
}
