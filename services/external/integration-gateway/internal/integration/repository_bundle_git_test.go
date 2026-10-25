package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
)

// TestRepositoryBundleGitSingleCommitAndIndependentReadback publishes 108 approved files over the existing isolated HTTPS Git fixture and proves one parent/commit, exact file bytes/modes, unchanged source ref and rejection of a replacement proposal. This is local Git proof, not a live repository publication.
// TestRepositoryBundleGitSingleCommitAndIndependentReadback публикует108 approved files через прежнюю isolated HTTPS Git fixture, доказывая one parent/commit, exact file bytes/modes, прежний source ref и отказ replacement proposal. Это local Git proof, не live repository publication.
func TestRepositoryBundleGitSingleCommitAndIndependentReadback(t *testing.T) {
	testRepositoryBundleGitSingleCommitAndIndependentReadback(t)
}

// testRepositoryBundleGitSingleCommitAndIndependentReadback shares the isolated HTTPS Git scenario between the standalone test and the shipped capability inventory, preserving exact source bytes, single-commit CAS and independent readback assertions.
// testRepositoryBundleGitSingleCommitAndIndependentReadback совместно использует изолированный HTTPS Git сценарий в отдельном тесте и каталоге возможностей, сохраняя проверки точных bytes, single-commit CAS и независимого readback.
func testRepositoryBundleGitSingleCommitAndIndependentReadback(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	repository := filepath.Join(root, "repo.git")
	gitFixtureRun(t, root, nil, "init", "--bare", "--quiet", repository)
	gitFixtureRun(t, repository, nil, "config", "http.receivepack", "true")
	gitFixtureRun(t, repository, nil, "config", "uploadpack.allowFilter", "true")
	gitFixtureRun(t, repository, nil, "config", "uploadpack.allowReachableSHA1InWant", "true")
	blob := gitFixtureRun(t, repository, []byte("Original repository\n"), "hash-object", "-w", "--stdin")
	tree := gitFixtureRun(t, repository, []byte("100644 blob "+blob+"\tREADME.md\n"), "mktree")
	base := gitFixtureRun(t, repository, []byte("Original\n"), "commit-tree", tree)
	gitFixtureRun(t, repository, nil, "update-ref", "refs/heads/main", base)
	server, ca, credential := gitFixtureHTTPServer(t, root)
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	open := func() *writeBackGitWorkspace {
		workspace, err := newWriteBackGitWorkspace(ctx, server.URL+"/repo.git", "", []byte(credential), "Kodex", "repository-bundle@kodex.invalid", time.Unix(1700000000, 0))
		if err != nil {
			t.Fatal(err)
		}
		workspace.environment = append(workspace.environment, "GIT_SSL_CAINFO="+ca)
		return workspace
	}
	files := make([]repositorybundle.File, 108)
	for index := range files {
		files[index] = repositorybundle.File{Entry: repositorybundle.Entry{Path: fmt.Sprintf("source/%03d.go", index), Mode: "100644"}, Bytes: []byte("package item\n// " + strings.Repeat("я", 1340) + "\n")}
	}
	files[107].Entry.Mode = "100755"
	container, rawManifest, err := repositorybundle.CanonicalContainer(files)
	if err != nil {
		t.Fatal(err)
	}
	manifest, files, err := repositorybundle.Open(container, rawManifest)
	if err != nil {
		t.Fatal(err)
	}
	writer := open()
	defer writer.close()
	branch := "kodex/bundle/rbp_gitfixture000"
	candidate, err := writer.bundleCandidate(ctx, base, manifest, files)
	if err != nil {
		t.Fatal("prepare full Git bundle", err)
	}
	if candidate.CommitSHA == base || candidate.TreeSHA == tree {
		t.Fatal("candidate did not contain the project")
	}
	if err := writer.push(ctx, branch, candidate.CommitSHA); err != nil {
		t.Fatal("sole proposal push", err)
	}
	if got := gitFixtureRun(t, repository, nil, "rev-parse", "refs/heads/main"); got != base {
		t.Fatal("source branch changed")
	}
	if got := gitFixtureRun(t, repository, nil, "show", "-s", "--format=%P", candidate.CommitSHA); got != base {
		t.Fatal("commit is not single-parent on the pinned base")
	}
	if got := gitFixtureRun(t, repository, nil, "rev-list", "--count", base+".."+candidate.CommitSHA); got != "1" {
		t.Fatal("publication created more than one commit")
	}
	if got := gitFixtureRun(t, repository, nil, "diff-tree", "--no-commit-id", "--name-only", "-r", candidate.CommitSHA); len(strings.Split(got, "\n")) != 108 {
		t.Fatal("commit omitted or introduced unapproved changed paths")
	}
	if got := gitFixtureRun(t, repository, nil, "cat-file", "blob", candidate.CommitSHA+":README.md"); got != "Original repository" {
		t.Fatal("unapproved base file changed")
	}
	reader := open()
	defer reader.close()
	if _, err := reader.run(ctx, nil, "cat-file", "-e", candidate.CommitSHA); err == nil {
		t.Fatal("independent reader inherited the prepared candidate cache")
	}
	if err := reader.verifyBundle(ctx, branch, base, candidate, manifest); err != nil {
		t.Fatal("full independent remote proof", err)
	}
	wrong := manifest
	wrong.Files = append([]repositorybundle.Entry(nil), manifest.Files...)
	wrong.Files[40].SHA256 = strings.Repeat("0", 64)
	if err := reader.verifyBundle(ctx, branch, base, candidate, wrong); err == nil {
		t.Fatal("substituted file digest accepted")
	}
	if err := reader.verifyBundle(ctx, "kodex/bundle/rbp_absentfixture", base, candidate, manifest); err == nil {
		t.Fatal("missing remote ref treated as success")
	}
	if err := reader.verifyBundle(ctx, branch, strings.Repeat("0", 40), candidate, manifest); err == nil {
		t.Fatal("foreign parent accepted")
	}
	// A distinct candidate cannot overwrite an already existing proposal, even with the same human-readable branch label.
	// Другой candidate не перезаписывает existing proposal даже с тем же human-readable branch label.
	replacement := open()
	defer replacement.close()
	files[0].Bytes = []byte("package replacement\n")
	container, rawManifest, err = repositorybundle.CanonicalContainer(files)
	if err != nil {
		t.Fatal(err)
	}
	replacementManifest, replacementFiles, err := repositorybundle.Open(container, rawManifest)
	if err != nil {
		t.Fatal(err)
	}
	other, err := replacement.bundleCandidate(ctx, base, replacementManifest, replacementFiles)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.push(ctx, branch, other.CommitSHA); err == nil {
		t.Fatal("proposal replacement escaped empty-ref CAS")
	}
	if ref, err := reader.bundleRef(ctx, branch); err != nil || ref != candidate.CommitSHA {
		t.Fatal("failed CAS changed the proposal", err)
	}
}
