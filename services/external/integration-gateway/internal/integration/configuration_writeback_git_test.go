package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func gitFixtureRun(t *testing.T, directory string, input []byte, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), writeBackGitBinary, args...)
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@kodex.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@kodex.invalid"}
	command.Stdin = bytes.NewReader(input)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Git fixture %s failed: %v", args[0], err)
	}
	return strings.TrimSpace(string(raw))
}

func TestWriteBackGitHTTPSExactProposalLeasePreservesSourceAndCleansCredential(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "repo.git")
	gitFixtureRun(t, root, nil, "init", "--bare", "--quiet", repository)
	gitFixtureRun(t, repository, nil, "config", "http.receivepack", "true")
	gitFixtureRun(t, repository, nil, "config", "uploadpack.allowFilter", "true")
	gitFixtureRun(t, repository, nil, "config", "uploadpack.allowReachableSHA1InWant", "true")
	baseContent := []byte("name: before\n")
	blob := gitFixtureRun(t, repository, baseContent, "hash-object", "-w", "--stdin")
	tree := gitFixtureRun(t, repository, []byte("100644 blob "+blob+"\tconfig.yaml\n"), "mktree")
	base := gitFixtureRun(t, repository, []byte("Initial\n"), "commit-tree", tree)
	gitFixtureRun(t, repository, nil, "update-ref", "refs/heads/main", base)
	const credential = "writeback-fixture-token"
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		username, password, ok := request.BasicAuth()
		if !ok || username != "oauth2" || password != credential {
			writer.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(io.LimitReader(request.Body, 16<<20))
		if err != nil {
			writer.WriteHeader(500)
			return
		}
		command := exec.CommandContext(request.Context(), writeBackGitBinary, "http-backend")
		command.Env = []string{"PATH=/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1", "PATH_INFO=" + request.URL.Path, "QUERY_STRING=" + request.URL.RawQuery, "REQUEST_METHOD=" + request.Method, "CONTENT_TYPE=" + request.Header.Get("Content-Type"), "CONTENT_LENGTH=" + strconv.Itoa(len(body)), "REMOTE_USER=fixture"}
		command.Stdin = bytes.NewReader(body)
		raw, err := command.Output()
		if err != nil {
			writer.WriteHeader(500)
			return
		}
		reader := bufio.NewReader(bytes.NewReader(raw))
		header, err := textproto.NewReader(reader).ReadMIMEHeader()
		if err != nil {
			writer.WriteHeader(500)
			return
		}
		status := 200
		for key, values := range header {
			if key == "Status" {
				status, _ = strconv.Atoi(strings.Fields(values[0])[0])
				continue
			}
			for _, value := range values {
				writer.Header().Add(key, value)
			}
		}
		writer.WriteHeader(status)
		_, _ = io.Copy(writer, reader)
	}))
	defer server.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	workspace, err := newWriteBackGitWorkspace(ctx, server.URL+"/repo.git", "", []byte(credential), "Kodex", "configuration@kodex.invalid", time.Unix(1700000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	directory := workspace.directory
	defer workspace.close()
	workspace.environment = append(workspace.environment, "GIT_SSL_CAINFO="+ca)
	for _, entry := range workspace.environment {
		if strings.Contains(entry, credential) {
			t.Fatal("credential entered process environment")
		}
	}
	stat, err := os.Stat(filepath.Join(directory, "credential"))
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("credential permissions")
	}
	source, proposal, err := workspace.refs(ctx, "main", "kodex/writeback/fixture")
	if err != nil || source != base || proposal != "" {
		t.Fatalf("initial refs: %v", err)
	}
	candidate, err := workspace.candidate(ctx, base, "config.yaml", sourceContentDigest(baseContent), "Update managed configuration", []byte("name: after\n"))
	if err != nil {
		t.Fatalf("candidate: %v", err)
	}
	if candidate.BaseBlob != blob || candidate.Blob == blob || candidate.Commit == base {
		t.Fatal("candidate identities are invalid")
	}
	if err := workspace.push(ctx, "kodex/writeback/fixture", candidate.Commit); err != nil {
		t.Fatal(err)
	}
	source, proposal, err = workspace.refs(ctx, "main", "kodex/writeback/fixture")
	if err != nil || source != base || proposal != candidate.Commit {
		t.Fatal("source changed or proposal missing")
	}
	parents := gitFixtureRun(t, repository, nil, "show", "-s", "--format=%P", candidate.Commit)
	if parents != base {
		t.Fatal("candidate has an unexpected parent")
	}
	// Новый другой commit не может заменить уже созданный proposal при empty-expect lease.
	other := gitFixtureRun(t, repository, []byte("Other\n"), "commit-tree", tree, "-p", base)
	if err := workspace.push(ctx, "kodex/writeback/fixture", other); err == nil {
		t.Fatal("proposal lease overwrote existing ref")
	}
	if got := gitFixtureRun(t, repository, nil, "rev-parse", "refs/heads/kodex/writeback/fixture"); got != candidate.Commit {
		t.Fatal("proposal was overwritten")
	}
	workspace.close()
	if _, err := os.Stat(directory); !os.IsNotExist(err) {
		t.Fatal("disposable credential/workspace survived close")
	}
}

func TestWriteBackGitCancellationIsBoundedAndIgnoresInheritedGitConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	workspace, err := newWriteBackGitWorkspace(ctx, "https://github.com/acme/repo.git", "", []byte("fixture"), "Kodex", "configuration@kodex.invalid", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.close()
	short, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	started := time.Now()
	_, err = workspace.run(short, nil, "-c", "alias.fixture=!sleep 30 & wait", "fixture")
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("process cancellation: %v", err)
	}
	for _, name := range []string{"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0"} {
		found := false
		for _, value := range workspace.environment {
			found = found || value == name
		}
		if !found {
			t.Fatal("inherited configuration isolation missing")
		}
	}
}
