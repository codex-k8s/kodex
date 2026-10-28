package integrationpackage

import _ "embed"

// githubSixCompatibility retains the exact pre-bundle shipped definition so an old pinned connection remains usable during the CP/IG rolling upgrade; this is a closed release snapshot, not a latest fallback.
// githubSixCompatibility сохраняет exact pre-bundle shipped definition для старой pinned connection при rolling upgrade CP/IG; это закрытый release snapshot, не latest fallback.
//
//go:embed compatibility/github-6.0.0.yaml
var githubSixCompatibility []byte

// resolveGitHubSixCompatibility resolves only the named old version and its own exact digest while the trusted current package is the 6.1 bundle release. The old package has no bulk capability and cannot inherit one through this path.
// resolveGitHubSixCompatibility разрешает лишь указанную старую version с её собственным exact digest, когда trusted current package является bundle release6.1. В старом package нет bulk capability, наследование новой через этот путь невозможно.
func resolveGitHubSixCompatibility(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Key != "github" || current.Metadata.Version != "6.1.0" || current.Metadata.Origin != Origin || current.Spec.Adapter != "GITHUB" || version != "6.0.0" {
		return Package{}, false
	}
	previous, err := Parse(githubSixCompatibility)
	if err != nil || previous.Metadata.Key != "github" || previous.Metadata.Version != version || previous.Digest != digest {
		return Package{}, false
	}
	return previous, true
}
