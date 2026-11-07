package integrationpackage

import _ "embed"

// jiraServerOneCompatibility freezes the pre-custom-fields contract during rolling CP/gateway
// upgrades. Old version/digest pins keep their exact input and configuration authority.
// jiraServerOneCompatibility закрепляет прежний контракт при rolling CP/gateway upgrade.
// Старые version/digest pins сохраняют точные полномочия ввода и конфигурации.
//
//go:embed compatibility/jira-server-dc-1.0.0.yaml
var jiraServerOneCompatibility []byte

// resolveJiraServerOneCompatibility accepts only the exact shipped historical definition,
// never a caller-supplied old version paired with new capabilities or a new digest.
// resolveJiraServerOneCompatibility допускает только точное историческое определение,
// а не старую версию с новыми capabilities либо новым digest.
func resolveJiraServerOneCompatibility(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Key != "jira-server-dc" || current.Metadata.Version != "1.1.0" || current.Metadata.Origin != Origin || current.Spec.Adapter != "JIRA" || version != "1.0.0" {
		return Package{}, false
	}
	previous, err := Parse(jiraServerOneCompatibility)
	if err != nil || previous.Metadata.Key != current.Metadata.Key || previous.Metadata.Version != version || previous.Digest != digest {
		return Package{}, false
	}
	return previous, true
}
