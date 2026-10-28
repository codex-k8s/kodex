package platform

import (
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// sshComposeReleaseDigest names one immutable release independently of inspect references or execution attempts.
// sshComposeReleaseDigest обозначает immutable release независимо от inspect refs и execution attempts.
func sshComposeReleaseDigest(snapshot sshcompose.Snapshot) string {
	if snapshot.Target.Validate() != nil || !sshComposeCommitPattern.MatchString(snapshot.MergedSHA) || !sshComposeCommitPattern.MatchString(snapshot.TreeSHA) || !validRuntimeSecretSHA256(snapshot.BundleSHA256) {
		return ""
	}
	raw, _ := json.Marshal([]string{snapshot.Target.Ref, snapshot.MergedSHA, snapshot.TreeSHA, snapshot.BundleSHA256, snapshot.Target.RuntimeImage})
	return sshcompose.Digest(raw)
}

// sshComposeAdmissionDigest preserves credential/grant provenance while allowing exact release rejoin across attempts.
// sshComposeAdmissionDigest сохраняет credential/grant provenance и допускает exact release rejoin между attempts.
func sshComposeAdmissionDigest(snapshot sshcompose.Snapshot) string {
	raw, _ := json.Marshal(struct {
		Target                                               sshcompose.Target
		Secret                                               sshcompose.SecretBinding
		Source                                               *sshcompose.SourceBinding
		MergeInvocationRef, MergedSHA, TreeSHA, BundleSHA256 string
	}{snapshot.Target, snapshot.SSHSecret, snapshot.Source, snapshot.MergeInvocationRef, snapshot.MergedSHA, snapshot.TreeSHA, snapshot.BundleSHA256})
	return sshcompose.Digest(raw)
}
