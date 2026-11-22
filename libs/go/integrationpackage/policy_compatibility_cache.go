package integrationpackage

import (
	"encoding/json"
	"sync"
)

// historicalPolicySnapshotPin selects only one source-registered embedded predecessor, never a caller authority or cache allocation key.
// historicalPolicySnapshotPin выбирает только source-registered embedded predecessor, не caller authority либо ключ нового cache allocation.
type historicalPolicySnapshotPin struct{ key, version string }

// historicalPolicySnapshot carries validated, immutable bytes and their canonical digest for one embedded predecessor.
// historicalPolicySnapshot хранит проверенные неизменяемые байты и канонический дайджест одного встроенного предшественника.
type historicalPolicySnapshot struct {
	canonical, digest string
}

// historicalPolicyCanonicalSnapshots contains only the finite embedded registry. Package readers receive independently parsed fields.
// historicalPolicyCanonicalSnapshots содержит только конечный встроенный реестр. Читатели пакета получают независимо разобранные поля.
var historicalPolicyCanonicalSnapshots = newHistoricalPolicyCanonicalSnapshots()

// newHistoricalPolicyCanonicalSnapshots validates each embedded YAML document once before canonical serialization.
// A failed built-in snapshot stays unavailable without allocating entries for callers.
// newHistoricalPolicyCanonicalSnapshots однократно проверяет каждый встроенный YAML перед канонической сериализацией.
// Ошибка встроенного снимка оставляет его недоступным без создания записей для вызывающих.
func newHistoricalPolicyCanonicalSnapshots() map[historicalPolicySnapshotPin]func() (historicalPolicySnapshot, bool) {
	snapshots := make(map[historicalPolicySnapshotPin]func() (historicalPolicySnapshot, bool))
	for key, entry := range historicalPolicyRevisions {
		for _, version := range entry.versions {
			pin := historicalPolicySnapshotPin{key: key, version: version}
			adapter := entry.adapter
			snapshots[pin] = sync.OnceValues(func() (historicalPolicySnapshot, bool) {
				raw, err := policyCompatibilitySnapshots.ReadFile("compatibility/" + pin.key + "-" + pin.version + ".yaml")
				if err != nil {
					return historicalPolicySnapshot{}, false
				}
				previous, err := Parse(raw)
				if err != nil || previous.Metadata.Key != pin.key || previous.Metadata.Version != pin.version || previous.Metadata.Origin != Origin || previous.Spec.Adapter != adapter {
					return historicalPolicySnapshot{}, false
				}
				canonical, err := json.Marshal(previous)
				if err != nil || len(canonical) == 0 || len(canonical) > maxBytes {
					return historicalPolicySnapshot{}, false
				}
				// A string prevents mutable byte aliases from escaping this process-local metadata cache.
				// String исключает выход mutable byte aliases из этого process-local metadata cache.
				return historicalPolicySnapshot{canonical: string(canonical), digest: previous.Digest}, true
			})
		}
	}
	return snapshots
}
