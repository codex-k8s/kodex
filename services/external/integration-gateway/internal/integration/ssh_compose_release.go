package integration

import (
	"context"
	"encoding/base64"
	"net/url"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
)

// ReadSSHComposeRelease reads the entire exact merged Git tree under an owner-selected connection.
// ReadSSHComposeRelease читает полное exact merged Git tree через выбранную owner connection.
func (adapter *Adapter) ReadSSHComposeRelease(ctx context.Context, snapshot sshcompose.Snapshot) (sshcompose.Bundle, error) {
	if snapshot.Target.Validate() != nil || snapshot.Source == nil || !sourceCommitPattern.MatchString(snapshot.MergedSHA) {
		return sshcompose.Bundle{}, sshcompose.ErrorInvalid
	}
	source := snapshot.Source
	credential, err := adapter.readCredential(ctx, &CredentialRevision{Ref: source.CredentialRef, Revision: source.CredentialRevision,
		SecretRef: source.SecretRef, SecretUID: source.SecretUID, SecretResourceVersion: source.SecretResourceVersion, ContentSHA256: source.CredentialSHA256})
	if err != nil {
		return sshcompose.Bundle{}, err
	}
	defer clear(credential)
	reader := configurationSourceReader{adapter: adapter, credential: credential, maximumContent: sshcompose.MaximumFileBytes}
	parts := strings.Split(snapshot.Target.Repository, "/")
	root := "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
	var commit struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if reader.githubJSON(ctx, adapter.githubBaseURL, root+"/git/commits/"+snapshot.MergedSHA, nil, &commit) != nil ||
		commit.SHA != snapshot.MergedSHA || !sourceCommitPattern.MatchString(commit.Tree.SHA) || snapshot.TreeSHA != "" && snapshot.TreeSHA != commit.Tree.SHA {
		return sshcompose.Bundle{}, sshcompose.ErrorInvalid
	}
	var tree struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Entries   *[]struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Size int    `json:"size"`
		} `json:"tree"`
	}
	if reader.githubJSON(ctx, adapter.githubBaseURL, root+"/git/trees/"+commit.Tree.SHA, url.Values{"recursive": {"1"}}, &tree) != nil ||
		tree.SHA != commit.Tree.SHA || tree.Truncated || tree.Entries == nil || len(*tree.Entries) > sshcompose.MaximumFiles*2 {
		return sshcompose.Bundle{}, sshcompose.ErrorInvalid
	}
	bundle := sshcompose.Bundle{Version: sshcompose.ProtocolVersion, Repository: snapshot.Target.Repository, MergedSHA: snapshot.MergedSHA, TreeSHA: commit.Tree.SHA}
	seen := map[string]bool{}
	total := 0
	for _, entry := range *tree.Entries {
		if !sshcompose.ValidPath(entry.Path) || seen[entry.Path] || !sourceCommitPattern.MatchString(entry.SHA) {
			return sshcompose.Bundle{}, sshcompose.ErrorInvalid
		}
		seen[entry.Path] = true
		if entry.Type == "tree" && entry.Mode == "040000" {
			continue
		}
		if entry.Type != "blob" || entry.Mode != "100644" && entry.Mode != "100755" || entry.Size < 0 || entry.Size > sshcompose.MaximumFileBytes || len(bundle.Files) >= sshcompose.MaximumFiles {
			return sshcompose.Bundle{}, sshcompose.ErrorInvalid
		}
		total += entry.Size
		if total > sshcompose.MaximumBundleBytes {
			return sshcompose.Bundle{}, sshcompose.ErrorInvalid
		}
		var blob struct {
			SHA      string `json:"sha"`
			Size     int    `json:"size"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if reader.githubJSON(ctx, adapter.githubBaseURL, root+"/git/blobs/"+entry.SHA, nil, &blob) != nil || blob.SHA != entry.SHA || blob.Size != entry.Size || blob.Encoding != "base64" {
			return sshcompose.Bundle{}, sshcompose.ErrorInvalid
		}
		raw, err := base64.StdEncoding.Strict().DecodeString(blob.Content)
		if err != nil || len(raw) != entry.Size || sshcompose.GitObjectID("blob", raw) != entry.SHA {
			clear(raw)
			return sshcompose.Bundle{}, sshcompose.ErrorInvalid
		}
		bundle.Files = append(bundle.Files, sshcompose.File{Path: entry.Path, Mode: entry.Mode, BlobSHA: entry.SHA, Content: base64.StdEncoding.EncodeToString(raw)})
		clear(raw)
	}
	bundle.Files = sshcompose.SortedFiles(bundle.Files)
	raw, manifest, err := bundle.Canonical()
	if err != nil || manifest.Image != snapshot.Target.RuntimeImage || snapshot.BundleSHA256 != "" && sshcompose.Digest(raw) != snapshot.BundleSHA256 {
		return sshcompose.Bundle{}, sshcompose.ErrorInvalid
	}
	return bundle, nil
}
