package integrationpackage

import _ "embed"

// githubSixCompatibility retains the exact pre-bundle shipped definition so an old pinned connection remains usable during the CP/IG rolling upgrade; this is a closed release snapshot, not a latest fallback.
// githubSixCompatibility сохраняет exact pre-bundle shipped definition для старой pinned connection при rolling upgrade CP/IG; это закрытый release snapshot, не latest fallback.
//
//go:embed compatibility/github-6.0.0.yaml
var githubSixCompatibility []byte

// githubSixOneCompatibility retains the shipped bundle contract before autonomous file publication was introduced; historical grants keep their original approval policies.
// githubSixOneCompatibility сохраняет поставленный контракт bundle до автономной публикации файлов; исторические grants сохраняют прежние политики согласования.
//
//go:embed compatibility/github-6.1.0.yaml
var githubSixOneCompatibility []byte

// githubSixTwoCompatibility preserves the exact autonomous-write release without future PR comment or Ready authority.
// githubSixTwoCompatibility сохраняет точный выпуск автономных записей без новых полномочий PR comment или Ready.
//
//go:embed compatibility/github-6.2.0.yaml
var githubSixTwoCompatibility []byte

// resolveGitHubSixCompatibility resolves only exact immutable historical releases under the trusted shipped GitHub adapter; it never derives old authority from the current capability set.
// resolveGitHubSixCompatibility разрешает только точные неизменяемые исторические выпуски доверенного поставленного адаптера GitHub; прежние полномочия не выводятся из текущих возможностей.
func resolveGitHubSixCompatibility(current Package, version, digest string) (Package, bool) {
	if current.Metadata.Key != "github" || current.Metadata.Version != "6.3.0" || current.Metadata.Origin != Origin || current.Spec.Adapter != "GITHUB" {
		return Package{}, false
	}
	var snapshot []byte
	switch version {
	case "6.0.0":
		snapshot = githubSixCompatibility
	case "6.1.0":
		snapshot = githubSixOneCompatibility
	case "6.2.0":
		snapshot = githubSixTwoCompatibility
	default:
		return Package{}, false
	}
	previous, err := Parse(snapshot)
	if err != nil || previous.Metadata.Key != "github" || previous.Metadata.Version != version || previous.Digest != digest {
		return Package{}, false
	}
	return previous, true
}
