package integration

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"path"
	"strconv"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

func (reader configurationSourceReader) readGitLab(ctx context.Context, configuration map[string]string, work *cp.ManagedConfigurationSourceWork) (ConfigurationSourceResult, error) {
	base, err := parseProviderBaseURL(configuration["base_url"])
	if err != nil {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	root := "/api/v4/projects/" + url.PathEscape(configuration["project_path"]) + "/repository"
	var commit struct {
		ID string `json:"id"`
	}
	if err := reader.gitLabSourceJSON(ctx, base, root+"/commits/"+url.PathEscape(work.GetRefName()), nil, &commit); err != nil {
		return ConfigurationSourceResult{}, err
	}
	if !sourceCommitPattern.MatchString(commit.ID) {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	ancestry := cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_INITIAL
	previous := work.GetPreviousCommitSha()
	if previous == commit.ID {
		ancestry = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_UNCHANGED
	} else if previous != "" {
		var ancestor struct {
			ID string `json:"id"`
		}
		if err := reader.gitLabSourceJSON(ctx, base, root+"/merge_base", url.Values{"refs[]": {previous, commit.ID}}, &ancestor); err != nil {
			return ConfigurationSourceResult{}, err
		}
		if !sourceCommitPattern.MatchString(ancestor.ID) {
			return ConfigurationSourceResult{}, sourceResponseInvalid()
		}
		if ancestor.ID != previous {
			return ConfigurationSourceResult{}, sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_DIVERGED)
		}
		ancestry = cp.ManagedConfigurationSourceAncestry_MANAGED_CONFIGURATION_SOURCE_ANCESTRY_FAST_FORWARD
	}
	blob, err := reader.gitLabSourceBlob(ctx, base, root, work.GetPath(), commit.ID)
	if err != nil {
		return ConfigurationSourceResult{}, err
	}
	var file struct {
		Path     string `json:"file_path"`
		Ref      string `json:"ref"`
		Commit   string `json:"commit_id"`
		Blob     string `json:"blob_id"`
		Digest   string `json:"content_sha256"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
		Content  string `json:"content"`
	}
	if err := reader.gitLabSourceJSON(ctx, base, root+"/files/"+url.PathEscape(work.GetPath()), url.Values{"ref": {commit.ID}}, &file); err != nil {
		return ConfigurationSourceResult{}, err
	}
	if file.Path != work.GetPath() || file.Ref != commit.ID || file.Commit != commit.ID || file.Blob != blob || file.Encoding != "base64" || file.Size < 1 || file.Size > reader.maximumContent {
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	content, err := base64.StdEncoding.Strict().DecodeString(file.Content)
	digest := sha256.Sum256(content)
	if err != nil || len(content) != file.Size || hex.EncodeToString(digest[:]) != file.Digest || !matchesGitBlobSHA(content, blob) {
		clear(content)
		return ConfigurationSourceResult{}, sourceResponseInvalid()
	}
	return ConfigurationSourceResult{CommitSHA: commit.ID, Content: content, Ancestry: ancestry}, nil
}

func (reader configurationSourceReader) gitLabSourceBlob(ctx context.Context, base *url.URL, root, wanted, commit string) (string, error) {
	parent := path.Dir(wanted)
	if parent == "." {
		parent = ""
	}
	seen := map[string]bool{}
	for page := 1; page <= 10; page++ {
		var entries []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
			Type string `json:"type"`
			Mode string `json:"mode"`
		}
		query := url.Values{"ref": {commit}, "path": {parent}, "per_page": {"100"}, "page": {strconv.Itoa(page)}}
		if err := reader.gitLabSourceJSON(ctx, base, root+"/tree", query, &entries); err != nil {
			return "", err
		}
		if entries == nil || len(entries) > 100 {
			return "", sourceResponseInvalid()
		}
		found := ""
		for _, entry := range entries {
			entryParent := path.Dir(entry.Path)
			if entryParent == "." {
				entryParent = ""
			}
			if !validRepositoryPath(entry.Path, false) || entryParent != parent || seen[entry.Path] {
				return "", sourceResponseInvalid()
			}
			seen[entry.Path] = true
			if entry.Path != wanted {
				continue
			}
			if entry.Type != "blob" || entry.Mode != "100644" && entry.Mode != "100755" || !sourceCommitPattern.MatchString(entry.ID) {
				return "", sourceResponseInvalid()
			}
			found = entry.ID
		}
		if found != "" {
			return found, nil
		}
		if len(entries) < 100 {
			return "", sourceFailure(cp.ManagedConfigurationSourceFailure_MANAGED_CONFIGURATION_SOURCE_FAILURE_NOT_FOUND)
		}
	}
	return "", sourceResponseInvalid()
}

func (reader configurationSourceReader) gitLabSourceJSON(ctx context.Context, base *url.URL, path string, query url.Values, target any) error {
	raw, err := reader.get(ctx, reader.adapter.providerHTTPClient, base, path, query, "application/json")
	if err != nil {
		return err
	}
	defer clear(raw)
	if json.Unmarshal(raw, target) != nil {
		return sourceResponseInvalid()
	}
	return nil
}
