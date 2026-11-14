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

// trustedHistoricalPolicyPackage parses only the explicitly requested closed predecessor; resolving one pin never scans or validates unrelated release snapshots.
// trustedHistoricalPolicyPackage разбирает только явно запрошенный closed predecessor; разрешение одного pin не сканирует unrelated release snapshots.
func trustedHistoricalPolicyPackage(current Package, version string) (Package, bool) {
	entry, ok := historicalPolicyRevisions[current.Metadata.Key]
	if !ok || current.Metadata.Version != entry.current || current.Metadata.Origin != Origin || current.Spec.Adapter != entry.adapter {
		return Package{}, false
	}
	found := false
	for _, known := range entry.versions {
		found = found || known == version
	}
	if !found {
		return Package{}, false
	}
	raw, err := policyCompatibilitySnapshots.ReadFile("compatibility/" + current.Metadata.Key + "-" + version + ".yaml")
	if err != nil {
		return Package{}, false
	}
	previous, err := Parse(raw)
	if err != nil || previous.Metadata.Key != current.Metadata.Key || previous.Metadata.Version != version || previous.Metadata.Origin != Origin || previous.Spec.Adapter != current.Spec.Adapter {
		return Package{}, false
	}
	return previous, true
}

// resolveHistoricalPolicyRevision matches both frozen version and canonical digest; unknown or mixed old/new pins remain unusable.
// resolveHistoricalPolicyRevision сопоставляет frozen version и canonical digest; неизвестные и смешанные old/new pins остаются недопустимыми.
func resolveHistoricalPolicyRevision(current Package, version, digest string) (Package, bool) {
	previous, ok := trustedHistoricalPolicyPackage(current, version)
	if !ok || previous.Digest != digest {
		return Package{}, false
	}
	return previous, true
}
