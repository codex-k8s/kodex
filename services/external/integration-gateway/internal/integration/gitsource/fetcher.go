package gitsource

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/gitfetcher"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/egressproxy"
)

type (
	FetcherConfig struct {
		GitExecutable, TemporaryRoot, CAFile, HTTPSProxy, NoProxy string
		Timeout                                                   time.Duration
	}
	Fetcher struct {
		catalog *Catalog
		secrets secretstore.Store
		config  FetcherConfig
	}
)

func NewFetcher(catalog *Catalog, secrets secretstore.Store, config FetcherConfig) (*Fetcher, error) {
	proxy, proxyErr := url.Parse(config.HTTPSProxy)
	if catalog == nil || secrets == nil || !filepath.IsAbs(config.GitExecutable) || !filepath.IsAbs(config.TemporaryRoot) || !filepath.IsAbs(config.CAFile) || proxyErr != nil || proxy.Scheme != "http" || proxy.Hostname() == "" || proxy.Port() == "" || proxy.User != nil || proxy.Path != "" || proxy.RawQuery != "" || proxy.Fragment != "" || config.NoProxy == "" || config.Timeout < time.Second || config.Timeout > time.Minute {
		return nil, errors.New("Git fetcher configuration is invalid")
	}
	return &Fetcher{catalog: catalog, secrets: secrets, config: config}, nil
}

func (fetcher *Fetcher) Fetch(ctx context.Context, repositoryKey, refKey, pathKey string) (gitfetcher.Fetched, error) {
	source, ok := fetcher.catalog.FetchSource(repositoryKey, refKey, pathKey)
	if !ok {
		return gitfetcher.Fetched{}, errors.New("Git source is not allowlisted")
	}
	credential, secretVersion, err := fetcher.secrets.Get(ctx, source.CredentialSecretRef)
	if err != nil {
		return gitfetcher.Fetched{}, err
	}
	defer zero(credential)
	if secretVersion.Ref != source.CredentialSecretRef || secretVersion.Version != source.CredentialBindingVersion || secretVersion.ContentDigest == "" {
		return gitfetcher.Fetched{}, errors.New("Git credential version-pinned readback mismatch")
	}
	work, err := os.MkdirTemp(fetcher.config.TemporaryRoot, "git-fetch-")
	if err != nil {
		return gitfetcher.Fetched{}, errors.New("create Git fetch directory")
	}
	defer os.RemoveAll(work)
	if err = os.Chmod(work, 0o700); err != nil {
		return gitfetcher.Fetched{}, errors.New("secure Git fetch directory")
	}
	tokenPath := filepath.Join(work, "credential")
	askPassPath := filepath.Join(work, "askpass")
	if err = os.WriteFile(tokenPath, credential, 0o600); err != nil {
		return gitfetcher.Fetched{}, errors.New("stage Git credential")
	}
	if err = os.WriteFile(askPassPath, []byte("#!/bin/sh\ncase \"$1\" in *Username*) printf '%s' oauth2 ;; *) exec /bin/cat \"$MATTERCODEX_GIT_TOKEN_FILE\" ;; esac\n"), 0o700); err != nil {
		return gitfetcher.Fetched{}, errors.New("stage Git credential helper")
	}
	repositoryPath := filepath.Join(work, "repository")
	if err = os.Mkdir(repositoryPath, 0o700); err != nil {
		return gitfetcher.Fetched{}, errors.New("create Git repository directory")
	}
	runCtx, cancel := context.WithTimeout(ctx, fetcher.config.Timeout)
	defer cancel()
	environment := fetcher.environment(work, askPassPath, tokenPath)
	if _, err = run(runCtx, environment, 64<<10, fetcher.config.GitExecutable, "-C", repositoryPath, "init"); err != nil {
		return gitfetcher.Fetched{}, err
	}
	if _, err = run(runCtx, environment, 64<<10, fetcher.config.GitExecutable, "-C", repositoryPath, "remote", "add", "origin", source.URL); err != nil {
		return gitfetcher.Fetched{}, err
	}
	if _, err = run(runCtx, environment, 64<<10, fetcher.config.GitExecutable,
		"-c", "http.sslVerify=true", "-c", "http.followRedirects=false", "-c", "credential.useHttpPath=true",
		"-C", repositoryPath, "fetch", "--depth=1", "--no-tags", "origin", source.Ref); err != nil {
		return gitfetcher.Fetched{}, err
	}
	commitRaw, err := run(runCtx, environment, 1024, fetcher.config.GitExecutable, "-C", repositoryPath, "rev-parse", "FETCH_HEAD^{commit}")
	if err != nil {
		return gitfetcher.Fetched{}, err
	}
	commit := strings.TrimSpace(string(commitRaw))
	if len(commit) != 40 && len(commit) != 64 {
		return gitfetcher.Fetched{}, errors.New("Git fetched commit is invalid")
	}
	content, err := run(runCtx, environment, source.MaximumBytes, fetcher.config.GitExecutable, "-C", repositoryPath, "show", commit+":"+source.Path)
	if err != nil {
		return gitfetcher.Fetched{}, err
	}
	if len(content) == 0 {
		return gitfetcher.Fetched{}, errors.New("Git source document is empty")
	}
	return gitfetcher.Fetched{Commit: commit, SourceRef: source.URL + "#" + commit + ":" + source.Path, Content: content}, nil
}

func (fetcher *Fetcher) environment(work, askPassPath, tokenPath string) []string {
	return []string{
		"HOME=" + work, "PATH=" + filepath.Dir(fetcher.config.GitExecutable) + ":/usr/bin:/bin",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=" + askPassPath,
		"MATTERCODEX_GIT_TOKEN_FILE=" + tokenPath, "HTTPS_PROXY=" + fetcher.config.HTTPSProxy,
		"NO_PROXY=" + fetcher.config.NoProxy, "GIT_SSL_CAINFO=" + fetcher.config.CAFile,
	}
}

func (fetcher *Fetcher) Check(ctx context.Context) error {
	checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	output, err := run(checkCtx, []string{"HOME=" + fetcher.config.TemporaryRoot, "PATH=" + filepath.Dir(fetcher.config.GitExecutable) + ":/usr/bin:/bin", "GIT_CONFIG_NOSYSTEM=1"}, 4096, fetcher.config.GitExecutable, "--version")
	if err != nil || !strings.HasPrefix(string(output), "git version ") {
		return errors.New("Git provider adapter is unavailable")
	}
	if err = fetcher.catalog.Check(checkCtx); err != nil {
		return err
	}
	return egressproxy.Check(checkCtx, fetcher.config.HTTPSProxy)
}

func run(ctx context.Context, environment []string, limit int64, executable string, arguments ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Env = environment
	var stdout limitedBuffer
	stdout.maximum = limit
	var stderr limitedBuffer
	stderr.maximum = 8 << 10
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return nil, errors.New("Git provider effect failed")
	}
	if stdout.overflow {
		return nil, errors.New("Git provider output exceeds limit")
	}
	return bytes.Clone(stdout.buffer.Bytes()), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	maximum  int64
	overflow bool
}

func (writer *limitedBuffer) Write(value []byte) (int, error) {
	size := len(value)
	remaining := writer.maximum - int64(writer.buffer.Len())
	if remaining <= 0 {
		writer.overflow = true
		return size, nil
	}
	part := value
	if int64(len(part)) > remaining {
		part = part[:remaining]
		writer.overflow = true
	}
	_, _ = writer.buffer.Write(part)
	return size, nil
}

func zero(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
