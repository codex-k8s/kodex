package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc"
)

func (server *Server) contextArtifact(writer http.ResponseWriter, request *http.Request, input runtimecontract.RunnerInput, artifactRef string) {
	_, pin, ok := contextArtifactPin(input, artifactRef, request.URL.RawQuery, time.Now())
	if !ok {
		http.NotFound(writer, request)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), server.config.RequestTimeout)
	defer cancel()
	response, err := server.control.Runtime.ReadExecutionArtifact(ctx, &cp.ReadExecutionArtifactRequest{
		LeaseRef: input.LeaseRef, Fence: input.LeaseFence, Generation: input.LeaseGeneration, ArtifactRef: pin.ArtifactRef,
	}, grpc.MaxCallRecvMsgSize(int(pin.SizeBytes)+(64<<10)))
	if err != nil {
		writeControlError(writer, err)
		return
	}
	artifact, content := response.GetArtifact(), response.GetContent()
	digest := sha256.Sum256(content)
	if artifact.GetRef() != pin.ArtifactRef || artifact.GetProjectRef() != input.ProjectRef ||
		int64(artifact.GetRevision()) != pin.ArtifactRevision || artifact.GetSizeBytes() != pin.SizeBytes ||
		artifact.GetDigest() != pin.Digest || int64(len(content)) != pin.SizeBytes ||
		"sha256:"+hex.EncodeToString(digest[:]) != pin.Digest {
		http.Error(writer, "runtime context artifact binding is invalid", http.StatusConflict)
		return
	}
	writer.Header().Set("Content-Type", "application/octet-stream")
	writer.Header().Set("Content-Length", strconv.FormatInt(pin.SizeBytes, 10))
	writer.Header().Set("X-Kodex-Artifact-Digest", pin.Digest)
	writer.WriteHeader(http.StatusOK)
	_, _ = writer.Write(content)
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
