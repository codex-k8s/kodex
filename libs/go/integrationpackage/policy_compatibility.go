package integrationpackage

import "embed"

// policyCompatibilitySnapshots keeps complete immutable shipped contracts; selecting a historical release never synthesizes authority from successor fields.
// policyCompatibilitySnapshots хранит полные immutable shipped contracts; выбор historical release не выводит полномочия из полей successor.
//
//go:embed compatibility/*.yaml
var policyCompatibilitySnapshots embed.FS

// historicalPolicyRevisions is a closed successor-to-predecessor registry, independent of versions or paths supplied by a caller.
// historicalPolicyRevisions — закрытый registry successor-to-predecessor, независимый от переданных caller версий и путей.
var historicalPolicyRevisions = map[string]struct {
	current, adapter string
	versions         []string
}{
	"gitlab":               {"2.0.0", "GITLAB", []string{"1.2.1"}},
	"github":               {"6.5.0", "GITHUB", []string{"6.0.0", "6.1.0", "6.2.0", "6.3.0", "6.4.0"}},
	"jira":                 {"1.3.0", "JIRA", []string{"1.2.1"}},
	"jira-server-dc":       {"1.2.0", "JIRA", []string{"1.0.0", "1.1.0"}},
	"confluence":           {"1.3.0", "CONFLUENCE", []string{"1.2.1"}},
	"confluence-server-dc": {"1.1.0", "CONFLUENCE", []string{"1.0.0"}},
	"context7":             {"1.1.0", "CONTEXT7", []string{"1.0.0"}},
	"https-json":           {"1.1.0", "HTTPS_JSON_READ", []string{"1.0.1"}},
}

// trustedHistoricalPolicyPackages returns only complete validated predecessor snapshots under their exact known successor adapter; errors close the whole historical route.
// trustedHistoricalPolicyPackages возвращает только полные проверенные predecessor snapshots под точным known successor adapter; ошибка закрывает historical route целиком.
func trustedHistoricalPolicyPackages(current Package) []Package {
	entry, ok := historicalPolicyRevisions[current.Metadata.Key]
	if !ok {
		return nil
	}
	packages := make([]Package, 0, len(entry.versions))
	for _, version := range entry.versions {
		previous, ok := trustedHistoricalPolicyPackage(current, version)
		if !ok {
			return nil
		}
		packages = append(packages, previous)
	}
	return packages
}

// historicalPolicySnapshotFor selects the immutable source snapshot under its closed successor identity and predecessor pin.
// historicalPolicySnapshotFor выбирает неизменяемый снимок по закрытой идентичности преемника и закреплённой версии предшественника.
func historicalPolicySnapshotFor(current Package, version string) (historicalPolicySnapshot, bool) {
	entry, ok := historicalPolicyRevisions[current.Metadata.Key]
	if !ok || current.Metadata.Version != entry.current || current.Metadata.Origin != Origin || current.Spec.Adapter != entry.adapter {
		return historicalPolicySnapshot{}, false
	}
	read, exists := historicalPolicyCanonicalSnapshots[historicalPolicySnapshotPin{key: current.Metadata.Key, version: version}]
	if !exists {
		return historicalPolicySnapshot{}, false
	}
	return read()
}

// parseHistoricalPolicySnapshot reparses only a selected immutable snapshot so exported packages own all nested fields.
// parseHistoricalPolicySnapshot заново разбирает только выбранный неизменяемый снимок, чтобы выдаваемый пакет владел всеми вложенными полями.
func parseHistoricalPolicySnapshot(snapshot historicalPolicySnapshot, current Package, version string) (Package, bool) {
	previous, err := Parse([]byte(snapshot.canonical))
	if err != nil || previous.Digest != snapshot.digest || previous.Metadata.Key != current.Metadata.Key || previous.Metadata.Version != version || previous.Metadata.Origin != Origin || previous.Spec.Adapter != current.Spec.Adapter {
		return Package{}, false
	}
	return previous, true
}

// trustedHistoricalPolicyPackage returns owned fields for one explicitly selected predecessor without scanning unrelated snapshots.
// trustedHistoricalPolicyPackage возвращает независимые поля выбранного предшественника без чтения остальных снимков.
func trustedHistoricalPolicyPackage(current Package, version string) (Package, bool) {
	snapshot, ok := historicalPolicySnapshotFor(current, version)
	if !ok {
		return Package{}, false
	}
	return parseHistoricalPolicySnapshot(snapshot, current, version)
}

// resolveHistoricalPolicyRevision requires the frozen version and digest before parsing owned fields.
// resolveHistoricalPolicyRevision сверяет закреплённые версию и дайджест до разбора независимых полей.
func resolveHistoricalPolicyRevision(current Package, version, digest string) (Package, bool) {
	snapshot, ok := historicalPolicySnapshotFor(current, version)
	if !ok || snapshot.digest != digest {
		return Package{}, false
	}
	return parseHistoricalPolicySnapshot(snapshot, current, version)
}

// matchesShippedRevision verifies an already canonicalized candidate against the exact current or embedded predecessor pin.
// This metadata comparison returns no Package and cannot replace parsing or validating the candidate before execution.
// matchesShippedRevision сверяет уже канонизированный пакет с точной версией текущего либо встроенного выпуска.
// Сверка метаданных не возвращает Package и не заменяет разбор или проверку пакета перед исполнением.
func matchesShippedRevision(current Package, version, digest string) bool {
	if current.Metadata.Version == version && current.Digest == digest {
		return true
	}
	snapshot, ok := historicalPolicySnapshotFor(current, version)
	return ok && snapshot.digest == digest
}
