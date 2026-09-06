package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// WriteSkillFile использует существующий execution callback, но отдельную
// типизированную привязку к SkillBundle. Controller разрешает её из snapshot,
// а не из переданного ref либо обычного списка workspace inputs.
func (client *Client) WriteSkillFile(ctx context.Context, input model.Input, skill runtimecontract.RuntimeSkillBundle, pin runtimecontract.RuntimeSkillFile, destination io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, filetransfer.TotalTimeout)
	defer cancel()
	snapshot, err := input.RequiredContextSnapshot(time.Now())
	if err != nil {
		return err
	}
	found := false
	for _, bound := range snapshot.Skills {
		if bound.BundleRef != skill.BundleRef || bound.RevisionRef != skill.RevisionRef || bound.Revision != skill.Revision ||
			bound.Digest != skill.Digest || bound.BindingRef != skill.BindingRef || bound.BindingVersion != skill.BindingVersion {
			continue
		}
		for _, candidate := range bound.Files {
			if candidate == pin {
				found = true
			}
		}
	}
	if !found || !runtimecontract.ValidSkillPath(pin.Path) || pin.ArtifactRevision < 1 || pin.SizeBytes < 0 || pin.SizeBytes > runtimecontract.MaximumSkillFileBytes {
		return errors.New("runtime skill file pin is invalid")
	}
	endpoint := *client.base
	endpoint.Path = "/v1/executions/" + url.PathEscape(input.LeaseRef) + "/artifacts/" + url.PathEscape(pin.ArtifactRef)
	query := url.Values{"context_kind": {"SKILL_BUNDLE"}, "skill_ref": {skill.BundleRef}, "skill_revision_ref": {skill.RevisionRef},
		"skill_path": {pin.Path}, "artifact_revision": {strconv.FormatInt(pin.ArtifactRevision, 10)}}
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create runtime skill file request")
	}
	request.Header.Set("Authorization", "Bearer "+client.token)
	bindExecutionHeaders(request, input, "artifact")
	request.Header.Set("Accept", "application/octet-stream")
	response, err := client.files.Do(request)
	if err != nil {
		return errors.New("runtime skill file callback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") == "" ||
		response.Header.Get("X-Kodex-Artifact-Digest") != pin.Digest || response.ContentLength != pin.SizeBytes {
		return errors.New("runtime skill file callback rejected request")
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(response.Body, pin.SizeBytes+1))
	if err != nil || n != pin.SizeBytes || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != pin.Digest {
		return errors.New("runtime skill file response is invalid")
	}
	return nil
}
