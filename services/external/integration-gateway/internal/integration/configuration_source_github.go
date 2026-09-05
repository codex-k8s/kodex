package integration

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func (reader configurationSourceReader) readGitHub(ctx context.Context, configuration map[string]string, work *cp.ManagedConfigurationSourceWork) (ConfigurationSourceResult, error) {
	base := reader.adapter.githubBaseURL
	root := "/repos/" + url.PathEscape(configuration["owner"]) + "/" + url.PathEscape(configuration["repository"])
	raw, err := reader.get(ctx, reader.adapter.githubHTTPClient, base, root+"/commits/"+url.PathEscape(work.GetRefName()), nil, "application/vnd.github.v3.sha")
	if err != nil {
		return ConfigurationSourceResult{}, err
	}
	commit := strings.TrimSpace(string(raw))
	clear(raw)
	if !sourceCommitPattern.MatchString(commit) {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	ancestry := cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_INITIAL
	previous := work.GetPreviousCommitSha()
	if previous == commit {
		ancestry = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_UNCHANGED
	} else if previous != "" {
		var comparison struct {
			Status string `json:"status"`
			Ahead  int    `json:"ahead_by"`
			Behind int    `json:"behind_by"`
			Base   struct {
				SHA string `json:"sha"`
			} `json:"base_commit"`
			MergeBase struct {
				SHA string `json:"sha"`
			} `json:"merge_base_commit"`
		}
		if err := reader.githubJSON(ctx, base, root+"/compare/"+previous+"..."+commit, url.Values{"per_page": {"1"}, "page": {"1"}}, &comparison); err != nil {
			return ConfigurationSourceResult{}, err
		}
		if comparison.Base.SHA != previous {
			return ConfigurationSourceResult{}, sourceResponseInvalid()
		}
		if comparison.Status != "ahead" || comparison.Ahead < 1 || comparison.Behind != 0 || comparison.MergeBase.SHA != previous {
			return ConfigurationSourceResult{}, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_DIVERGED)
		}
		ancestry = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_FAST_FORWARD
	}
	var pinned struct {
		SHA  string `json:"sha"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := reader.githubJSON(ctx, base, root+"/git/commits/"+commit, nil, &pinned); err != nil {
		return ConfigurationSourceResult{}, err
	}
	if pinned.SHA != commit || !sourceCommitPattern.MatchString(pinned.Tree.SHA) {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	current := pinned.Tree.SHA
	parts := strings.Split(work.GetPath(), "/")
	var blobSize int
	for index, part := range parts {
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
		if err := reader.githubJSON(ctx, base, root+"/git/trees/"+current, nil, &tree); err != nil {
			return ConfigurationSourceResult{}, err
		}
		if tree.SHA != current || tree.Truncated || tree.Entries == nil || len(*tree.Entries) > 10000 {
			return ConfigurationSourceResult{}, sourceResponseInvalid()
		}
		found := false
		seen := map[string]bool{}
		for _, entry := range *tree.Entries {
			if entry.Path == "" || strings.Contains(entry.Path, "/") || seen[entry.Path] {
				return ConfigurationSourceResult{}, sourceResponseInvalid()
			}
			seen[entry.Path] = true
			if entry.Path != part {
				continue
			}
			if !sourceCommitPattern.MatchString(entry.SHA) {
				return ConfigurationSourceResult{}, sourceResponseInvalid()
			}
			if index < len(parts)-1 {
				if entry.Type != "tree" || entry.Mode != "040000" {
					return ConfigurationSourceResult{}, sourceResponseInvalid()
				}
			} else if entry.Type != "blob" || entry.Mode != "100644" && entry.Mode != "100755" || entry.Size < 1 || entry.Size > reader.maximumContent {
				return ConfigurationSourceResult{}, sourceResponseInvalid()
			}
			current, blobSize, found = entry.SHA, entry.Size, true
		}
		if !found {
			return ConfigurationSourceResult{}, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_NOT_FOUND)
		}
	}
	var blob struct {
		SHA      string `json:"sha"`
		Size     int    `json:"size"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := reader.githubJSON(ctx, base, root+"/git/blobs/"+current, nil, &blob); err != nil {
		return ConfigurationSourceResult{}, err
	}
	if blob.SHA != current || blob.Size != blobSize || blob.Encoding != "base64" {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	content, err := base64.StdEncoding.Strict().DecodeString(blob.Content)
	if err != nil || len(content) != blobSize || !matchesGitBlobSHA(content, current) {
		clear(content)
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	return ConfigurationSourceResult{CommitSHA: commit, Content: content, Ancestry: ancestry}, nil
}

func (reader configurationSourceReader) githubJSON(ctx context.Context, base *url.URL, path string, query url.Values, target any) error {
	raw, err := reader.get(ctx, reader.adapter.githubHTTPClient, base, path, query, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer clear(raw)
	if json.Unmarshal(raw, target) != nil {
		return sourceResponseInvalid()
	}
	return nil
}

// SHA-1 требуется Git object protocol; public provenance отдельно получает SHA-256.
func matchesGitBlobSHA(content []byte, expected string) bool {
	hash := sha1.New()
	_, _ = hash.Write([]byte("blob " + strconv.Itoa(len(content)) + "\x00"))
	_, _ = hash.Write(content)
	return hex.EncodeToString(hash.Sum(nil)) == expected
}
