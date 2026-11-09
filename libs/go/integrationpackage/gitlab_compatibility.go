package integrationpackage

import _ "embed"

// gitLabOneTwoOneCompatibility preserves the exact single-project contract before configurable scopes and human policies; old pins never inherit instance access.
// gitLabOneTwoOneCompatibility сохраняет точный single-project contract до configurable scopes и human policies; прежние pins не наследуют instance access.
//
//go:embed compatibility/gitlab-1.2.1.yaml
var gitLabOneTwoOneCompatibility []byte

// resolveGitLabOneTwoOneCompatibility accepts only the frozen version/digest under the trusted successor adapter, without synthesizing old authority from new fields.
// resolveGitLabOneTwoOneCompatibility допускает лишь закреплённые version/digest под trusted successor adapter без синтеза прежних полномочий из новых полей.
func resolveGitLabOneTwoOneCompatibility(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Key != "gitlab" || current.Metadata.Version != "2.0.0" || current.Metadata.Origin != Origin || current.Spec.Adapter != string(AdapterGitLab) || version != "1.2.1" {
		return Package{}, false
	}
	previous, err := Parse(gitLabOneTwoOneCompatibility)
	if err != nil || previous.Metadata.Key != current.Metadata.Key || previous.Metadata.Version != version || previous.Digest != digest {
		return Package{}, false
	}
	return previous, true
}
