package integration

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
)

// RepositoryBundleCandidate records the exact single-parent commit and full tree before owner Begin; it carries no authority or permission to push.
// RepositoryBundleCandidate хранит exact single-parent commit и полное tree до owner Begin; authority либо разрешения push в нём нет.
type RepositoryBundleCandidate struct{ CommitSHA, TreeSHA string }

// bundleRef reads one exact destination ref and distinguishes authoritative absence from unavailable/malformed output; it never creates or changes a ref.
// bundleRef читает одну exact destination ref и отличает авторитетное отсутствие от unavailable/malformed output; ref не создаётся и не меняется.
func (workspace *writeBackGitWorkspace) bundleRef(ctx context.Context, branch string) (string, error) {
	if !validWriteBackRef(branch) || !strings.HasPrefix(branch, "kodex/bundle/rbp_") {
		return "", errWriteBackGit
	}
	raw, err := workspace.run(ctx, nil, "ls-remote", "--refs", "origin", "refs/heads/"+branch)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", nil
	}
	fields := strings.Split(strings.TrimSpace(string(raw)), "\t")
	if len(fields) != 2 || !sourceCommitPattern.MatchString(fields[0]) || fields[1] != "refs/heads/"+branch {
		return "", errWriteBackGit
	}
	return fields[0], nil
}

// bundleCandidate replaces only the complete approved regular-file set in a pinned base tree, then creates one deterministic single-parent commit. Only local Git objects change; source content is passed over stdin, never shell, argv or extracted paths.
// bundleCandidate заменяет только полный approved regular-file set в pinned base tree, затем создаёт один deterministic single-parent commit. Меняются лишь local Git objects; source content передаётся через stdin, не shell/argv/extracted paths.
func (workspace *writeBackGitWorkspace) bundleCandidate(ctx context.Context, base string, manifest repositorybundle.Manifest, files []repositorybundle.File) (RepositoryBundleCandidate, error) {
	var result RepositoryBundleCandidate
	if len(base) != 40 || !sourceCommitPattern.MatchString(base) || len(files) == 0 || len(files) != len(manifest.Files) || len(files) > repositorybundle.MaximumFiles {
		return result, errWriteBackGit
	}
	if _, err := workspace.run(ctx, nil, "fetch", "--quiet", "--depth=1", "--filter=blob:none", "--no-tags", "--no-recurse-submodules", "origin", base); err != nil {
		return result, err
	}
	if _, err := workspace.run(ctx, nil, "read-tree", base); err != nil {
		return result, err
	}
	var index bytes.Buffer
	defer func() { clear(index.Bytes()) }()
	for number, file := range files {
		entry := manifest.Files[number]
		if entry != file.Entry || !repositorybundle.ValidPath(entry.Path) || int64(len(file.Bytes)) != entry.Bytes || repositorybundle.Digest(file.Bytes) != entry.SHA256 || entry.Mode != "100644" && entry.Mode != "100755" {
			return result, errWriteBackGit
		}
		before, err := workspace.run(ctx, nil, "ls-tree", "-z", base, "--", entry.Path)
		if err != nil {
			return result, err
		}
		if len(before) != 0 {
			parts := strings.SplitN(strings.TrimSuffix(string(before), "\x00"), "\t", 2)
			if len(parts) != 2 || parts[1] != entry.Path {
				clear(before)
				return result, errWriteBackGit
			}
			metadata := strings.Fields(parts[0])
			if len(metadata) != 3 || metadata[1] != "blob" || metadata[0] != "100644" && metadata[0] != "100755" {
				clear(before)
				return result, errWriteBackGit
			}
		}
		clear(before)
		raw, err := workspace.run(ctx, file.Bytes, "hash-object", "-w", "--stdin")
		if err != nil {
			return result, err
		}
		blob := strings.TrimSpace(string(raw))
		clear(raw)
		if len(blob) != 40 || !sourceCommitPattern.MatchString(blob) {
			return result, errWriteBackGit
		}
		fmt.Fprintf(&index, "%s %s\t%s\x00", entry.Mode, blob, entry.Path)
	}
	if _, err := workspace.run(ctx, index.Bytes(), "update-index", "-z", "--index-info"); err != nil {
		return result, err
	}
	raw, err := workspace.run(ctx, nil, "write-tree")
	if err != nil {
		return result, err
	}
	result.TreeSHA = strings.TrimSpace(string(raw))
	clear(raw)
	if len(result.TreeSHA) != 40 || !sourceCommitPattern.MatchString(result.TreeSHA) {
		return result, errWriteBackGit
	}
	raw, err = workspace.run(ctx, []byte("Publish repository bundle\n"), "commit-tree", result.TreeSHA, "-p", base)
	if err != nil {
		return result, err
	}
	result.CommitSHA = strings.TrimSpace(string(raw))
	clear(raw)
	if len(result.CommitSHA) != 40 || !sourceCommitPattern.MatchString(result.CommitSHA) {
		return RepositoryBundleCandidate{}, errWriteBackGit
	}
	return result, nil
}

// verifyBundle reads the exact remote proposal ref, commit headers and every approved file's full bytes. Missing/changed refs never permit a retry; callers retain UNKNOWN until a later exact read succeeds.
// verifyBundle читает exact remote proposal ref, commit headers и полные bytes каждого approved file. Missing/changed refs не разрешают retry; caller сохраняет UNKNOWN до следующего успешного exact read.
func (workspace *writeBackGitWorkspace) verifyBundle(ctx context.Context, branch, base string, candidate RepositoryBundleCandidate, manifest repositorybundle.Manifest) error {
	ref, err := workspace.bundleRef(ctx, branch)
	if err != nil || ref != candidate.CommitSHA || len(ref) != 40 {
		return errWriteBackGit
	}
	if _, err := workspace.run(ctx, nil, "fetch", "--quiet", "--depth=1", "--no-tags", "--no-recurse-submodules", "origin", candidate.CommitSHA); err != nil {
		return err
	}
	raw, err := workspace.run(ctx, nil, "cat-file", "-p", candidate.CommitSHA)
	if err != nil {
		return err
	}
	headers := strings.SplitN(string(raw), "\n\n", 2)
	clear(raw)
	if len(headers) != 2 {
		return errWriteBackGit
	}
	var tree, parent string
	var parents int
	for _, line := range strings.Split(headers[0], "\n") {
		if strings.HasPrefix(line, "tree ") {
			tree = strings.TrimPrefix(line, "tree ")
		}
		if strings.HasPrefix(line, "parent ") {
			parents++
			parent = strings.TrimPrefix(line, "parent ")
		}
	}
	if tree != candidate.TreeSHA || parents != 1 || parent != base {
		return errWriteBackGit
	}
	for _, entry := range manifest.Files {
		metadata, err := workspace.run(ctx, nil, "ls-tree", "-z", candidate.CommitSHA, "--", entry.Path)
		if err != nil {
			return err
		}
		parts := strings.SplitN(strings.TrimSuffix(string(metadata), "\x00"), "\t", 2)
		clear(metadata)
		if len(parts) != 2 || parts[1] != entry.Path {
			return errWriteBackGit
		}
		fields := strings.Fields(parts[0])
		if len(fields) != 3 || fields[0] != entry.Mode || fields[1] != "blob" || !sourceCommitPattern.MatchString(fields[2]) {
			return errWriteBackGit
		}
		content, err := workspace.run(ctx, nil, "cat-file", "blob", fields[2])
		if err != nil {
			return err
		}
		valid := int64(len(content)) == entry.Bytes && repositorybundle.Digest(content) == entry.SHA256
		clear(content)
		if !valid {
			return errWriteBackGit
		}
	}
	return nil
}
