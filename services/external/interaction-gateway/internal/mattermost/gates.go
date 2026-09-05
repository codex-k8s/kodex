package mattermost

import (
	"context"
	"strconv"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	gateRefProperty     = "kodex_gate_ref"
	gateVersionProperty = "kodex_gate_version"
	gateRunProperty     = "kodex_run_ref"
)

type gateContext struct {
	ref     string
	version int64
	runRef  string
}

func gateFromClaim(claim *controlplanev1.InteractionDeliveryClaim) (*gateContext, error) {
	if claim.GetCapabilityKey() != "mattermost.gate_decisions" {
		if claim.GetGateRef() != "" || claim.GetGateVersion() != 0 {
			return nil, errConfiguration
		}
		return nil, nil
	}
	gate := &gateContext{ref: claim.GetGateRef(), version: claim.GetGateVersion(), runRef: claim.GetRunRef()}
	if !gate.valid() {
		return nil, errConfiguration
	}
	return gate, nil
}

func (gate *gateContext) valid() bool {
	return gate != nil && boundedReference(gate.ref) && boundedReference(gate.runRef) && gate.version > 0
}

func boundedReference(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (gate *gateContext) addToPost(post *model.Post) {
	post.AddProp(gateRefProperty, gate.ref)
	post.AddProp(gateVersionProperty, strconv.FormatInt(gate.version, 10))
	post.AddProp(gateRunProperty, gate.runRef)
}

func gateFromPost(post *model.Post) (*gateContext, error) {
	ref, version, runRef := post.GetProp(gateRefProperty), post.GetProp(gateVersionProperty), post.GetProp(gateRunProperty)
	if ref == nil && version == nil && runRef == nil {
		return nil, nil
	}
	refString, refOK := ref.(string)
	versionString, versionOK := version.(string)
	runString, runOK := runRef.(string)
	if !refOK || !versionOK || !runOK {
		return nil, errResponse
	}
	number, err := strconv.ParseInt(versionString, 10, 64)
	gate := &gateContext{ref: refString, version: number, runRef: runString}
	if err != nil || !gate.valid() || strconv.FormatInt(number, 10) != versionString {
		return nil, errResponse
	}
	return gate, nil
}

func readGateContext(ctx context.Context, client *model.Client4, post *model.Post, channelID, botID string) (*gateContext, error) {
	if post.RootId == "" {
		return nil, nil
	}
	root, _, err := client.GetPost(ctx, post.RootId, "")
	if err != nil {
		return nil, classify(err)
	}
	if root == nil || root.Id != post.RootId || root.ChannelId != channelID || root.RootId != "" || root.DeleteAt != 0 {
		return nil, errResponse
	}
	if root.UserId != botID {
		return nil, nil
	}
	// Props только коррелируют с серверной delivery; полномочия и версия
	// повторно проверяются владельцем после разрешения пользовательской привязки.
	return gateFromPost(root)
}
