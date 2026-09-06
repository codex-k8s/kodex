package callback

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func (server *Server) contextArtifact(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput, artifactRef string) {
	_, pin, ok := contextArtifactPin(input, artifactRef, request.URL.RawQuery, time.Now())
	if !ok {
		http.NotFound(writer, request)
		return
	}
	server.serveArtifactTransfer(writer, request, input, artifactTransferPin{ref: pin.ArtifactRef, project: input.ProjectRef,
		digest: pin.Digest, size: pin.SizeBytes, revision: pin.ArtifactRevision}, "application/octet-stream")
}

// contextArtifactPin разрешает только точный файл immutable snapshot активной
// execution. Query выбирает запись, но не выдаёт полномочия на её чтение.
func contextArtifactPin(input runtimecontract.RunnerInput, artifactRef, rawQuery string, now time.Time) (runtimecontract.RuntimeSkillBundle, runtimecontract.RuntimeSkillFile, bool) {
	snapshot, err := input.RequiredContextSnapshot(now)
	if err != nil || len(rawQuery) > 2048 {
		return runtimecontract.RuntimeSkillBundle{}, runtimecontract.RuntimeSkillFile{}, false
	}
	query, err := url.ParseQuery(rawQuery)
	if err != nil || len(query) != 5 {
		return runtimecontract.RuntimeSkillBundle{}, runtimecontract.RuntimeSkillFile{}, false
	}
	for _, key := range []string{"context_kind", "skill_ref", "skill_revision_ref", "skill_path", "artifact_revision"} {
		if len(query[key]) != 1 || query.Get(key) == "" {
			return runtimecontract.RuntimeSkillBundle{}, runtimecontract.RuntimeSkillFile{}, false
		}
	}
	revision, err := strconv.ParseInt(query.Get("artifact_revision"), 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != query.Get("artifact_revision") ||
		query.Get("context_kind") != "SKILL_BUNDLE" || !runtimecontract.ValidSkillPath(query.Get("skill_path")) {
		return runtimecontract.RuntimeSkillBundle{}, runtimecontract.RuntimeSkillFile{}, false
	}
	for _, skill := range snapshot.Skills {
		if skill.BundleRef != query.Get("skill_ref") || skill.RevisionRef != query.Get("skill_revision_ref") {
			continue
		}
		for _, file := range skill.Files {
			if file.ArtifactRef == artifactRef && file.ArtifactRevision == revision && file.Path == query.Get("skill_path") {
				return skill, file, true
			}
		}
	}
	return runtimecontract.RuntimeSkillBundle{}, runtimecontract.RuntimeSkillFile{}, false
}
