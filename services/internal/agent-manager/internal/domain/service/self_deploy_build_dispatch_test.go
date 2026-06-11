package service

import "testing"

func TestSelfDeployRuntimeBuildContextsFromResultDoesNotInventDockerfileDigest(t *testing.T) {
	t.Parallel()

	contexts, err := selfDeployRuntimeBuildContextsFromResult(SelfDeployBuildPlan{
		BuildItems: []SelfDeployBuildPlanItem{{
			ServiceKey:          "agent-manager",
			PlanItemFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		}},
	}, SelfDeployBuildContextResult{
		RuntimeBuildContextRef:     "runtime:build-context/ready",
		RuntimeBuildContextStatus:  "ready",
		BuildContextRef:            "runtime://build-contexts/agent-manager",
		BuildContextDigest:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ManifestBundleDigests:      map[string]string{"agent-manager": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
		SourceSnapshotDigest:       "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		MaterializationFingerprint: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	})
	if err != nil {
		t.Fatalf("selfDeployRuntimeBuildContextsFromResult(): %v", err)
	}
	if len(contexts) != 1 {
		t.Fatalf("contexts = %d, want 1", len(contexts))
	}
	if contexts[0].BuildContextDigest == "" ||
		contexts[0].DockerfileDigest != "" ||
		contexts[0].ManifestBundleDigest != "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" {
		t.Fatalf("context = %+v, want build context and manifest bundle digests without synthetic Dockerfile digest", contexts[0])
	}
}
