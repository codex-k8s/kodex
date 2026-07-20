package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const credentialRotationPollInterval = 5 * time.Millisecond

type runSafety struct {
	unsafe atomic.Bool
}

func (safety *runSafety) markUnsafe() {
	safety.unsafe.Store(true)
}

func (safety *runSafety) isUnsafe() bool {
	return safety.unsafe.Load()
}

type credentialRotationError struct{}

func (credentialRotationError) Error() string {
	return "credential source изменился во время выполнения; результат хода отклонён"
}

type unsupportedShortCredentialError struct{}

func (unsupportedShortCredentialError) Error() string {
	return "credential source содержит неподдерживаемое короткое значение; запуск отклонён"
}

type credentialSourceSnapshot struct {
	sourcePath string
	digest     [sha256.Size]byte
	exists     bool
	info       os.FileInfo
}

type credentialRunGuard struct {
	runner       *runner
	cancel       context.CancelFunc
	snapshotRoot string
	sources      []credentialSourceSnapshot
	stop         chan struct{}
	done         chan struct{}
	started      atomic.Bool
	rotated      atomic.Bool
	stopOnce     sync.Once
}

func (r *runner) guardedCommand(
	ctx context.Context,
	environment []string,
	name string,
	args ...string,
) (*exec.Cmd, *credentialRunGuard, error) {
	if err := r.prepareEphemeralRuntime(); err != nil {
		return nil, nil, err
	}
	if r.safety.isUnsafe() {
		return nil, nil, credentialRotationError{}
	}
	guardedEnvironment, sources, snapshotRoot, inventory, err := r.snapshotCredentialEnvironment(environment)
	if err != nil {
		return nil, nil, err
	}
	r.secrets = r.secrets.merge(inventory)
	if err := r.secrets.validateForExecution(); err != nil {
		_ = os.RemoveAll(snapshotRoot)
		return nil, nil, err
	}
	commandContext, cancel := context.WithCancel(ctx)
	guard := &credentialRunGuard{
		runner:       r,
		cancel:       cancel,
		snapshotRoot: snapshotRoot,
		sources:      sources,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	cmd := r.command(commandContext, name, args...)
	cmd.Env = guardedEnvironment
	return cmd, guard, nil
}

func (r *runner) snapshotCredentialEnvironment(environment []string) ([]string, []credentialSourceSnapshot, string, secretInventory, error) {
	snapshotRoot, err := os.MkdirTemp(r.ephemeralRoot, "credential-snapshot-")
	if err != nil {
		return nil, nil, "", secretInventory{}, fmt.Errorf("эфемерный снимок credential sources не создан")
	}
	if err := os.Chmod(snapshotRoot, 0o700); err != nil {
		_ = os.RemoveAll(snapshotRoot)
		return nil, nil, "", secretInventory{}, fmt.Errorf("эфемерный снимок credential sources не защищён")
	}
	values := map[string]struct{}{}
	collectEnvironmentSecretValues(values, environment)
	paths := append(credentialFileSources(environment), r.credentialFiles...)
	seen := map[string]struct{}{}
	rewrites := map[string]string{}
	sources := make([]credentialSourceSnapshot, 0, len(paths))
	for _, rawPath := range paths {
		rawPath = strings.TrimSpace(rawPath)
		if rawPath == "" {
			continue
		}
		path := filepath.Clean(rawPath)
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		body, exists, readErr := readCredentialSource(path)
		if readErr != nil {
			_ = os.RemoveAll(snapshotRoot)
			return nil, nil, "", secretInventory{}, readErr
		}
		snapshotPath := filepath.Join(snapshotRoot, fmt.Sprintf("source-%04d", len(sources)))
		if exists {
			if err := os.WriteFile(snapshotPath, body, 0o600); err != nil {
				_ = os.RemoveAll(snapshotRoot)
				return nil, nil, "", secretInventory{}, fmt.Errorf("эфемерный снимок credential source не записан")
			}
			collectCredentialFileValues(values, body)
		}
		var sourceInfo os.FileInfo
		if exists {
			sourceInfo, err = os.Stat(path)
			if err != nil {
				_ = os.RemoveAll(snapshotRoot)
				return nil, nil, "", secretInventory{}, fmt.Errorf("credential source изменился при создании снимка")
			}
		}
		rewrites[path] = snapshotPath
		sources = append(sources, credentialSourceSnapshot{
			sourcePath: path,
			digest:     sha256.Sum256(body),
			exists:     exists,
			info:       sourceInfo,
		})
	}
	return rewriteCredentialFileEnvironment(environment, rewrites), sources, snapshotRoot, compileSecretInventory(values), nil
}

func readCredentialSource(path string) ([]byte, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("credential source недоступен")
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("credential source не является обычным файлом")
	}
	if info.Size() < 0 || info.Size() > maxCredentialFileBytes {
		return nil, false, boundedFileError{Kind: "credential source"}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("credential source не прочитан")
	}
	return body, true, nil
}

func rewriteCredentialFileEnvironment(environment []string, rewrites map[string]string) []string {
	result := append([]string(nil), environment...)
	runtimeNames := runtimeEnvironmentNames(environment)
	for index, item := range result {
		name, value, ok := strings.Cut(item, "=")
		if !ok || !credentialFileEnvironmentName(name, runtimeNames) {
			continue
		}
		paths := strings.Split(value, string(os.PathListSeparator))
		for pathIndex, rawPath := range paths {
			if replacement, exists := rewrites[filepath.Clean(strings.TrimSpace(rawPath))]; exists {
				paths[pathIndex] = replacement
			}
		}
		result[index] = name + "=" + strings.Join(paths, string(os.PathListSeparator))
	}
	return result
}

func credentialFileEnvironmentName(name string, runtimeNames map[string]bool) bool {
	return name == "KUBECONFIG" || name == "MATTERCODEX_GITHUB_TOKEN_FILE" ||
		(strings.HasSuffix(name, "_FILE") && (sensitiveEnvironmentName(strings.TrimSuffix(name, "_FILE")) || runtimeNames[name] || runtimeNames[strings.TrimSuffix(name, "_FILE")]))
}

func (guard *credentialRunGuard) start() {
	if guard == nil || guard.started.Swap(true) {
		return
	}
	if guard.sourcesChanged(true) {
		guard.markRotated()
	}
	go func() {
		defer close(guard.done)
		ticker := time.NewTicker(credentialRotationPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-guard.stop:
				return
			case <-ticker.C:
				if guard.sourcesChanged(false) {
					guard.markRotated()
					return
				}
			}
		}
	}()
}

func (guard *credentialRunGuard) finish(runErr error) error {
	if guard == nil {
		return runErr
	}
	guard.stopOnce.Do(func() { close(guard.stop) })
	if guard.started.Load() {
		<-guard.done
	}
	if guard.sourcesChanged(true) {
		guard.markRotated()
	}
	guard.cancel()
	_ = os.RemoveAll(guard.snapshotRoot)
	if guard.rotated.Load() {
		guard.runner.discardUnsafeRunData()
		return errors.Join(runErr, credentialRotationError{})
	}
	return runErr
}

func (guard *credentialRunGuard) markRotated() {
	if guard.rotated.CompareAndSwap(false, true) {
		guard.runner.safety.markUnsafe()
		guard.cancel()
	}
}

func (guard *credentialRunGuard) sourcesChanged(checkContent bool) bool {
	for _, source := range guard.sources {
		info, err := os.Stat(source.sourcePath)
		if os.IsNotExist(err) {
			if source.exists {
				return true
			}
			continue
		}
		if err != nil || !source.exists || !info.Mode().IsRegular() || source.info == nil ||
			info.Size() != source.info.Size() || !info.ModTime().Equal(source.info.ModTime()) ||
			info.Mode() != source.info.Mode() || !os.SameFile(info, source.info) {
			return true
		}
		if checkContent {
			body, exists, readErr := readCredentialSource(source.sourcePath)
			if readErr != nil || !exists || sha256.Sum256(body) != source.digest {
				return true
			}
		}
	}
	return false
}

func (r *runner) discardUnsafeRunData() {
	if r.rawArtifacts != "" {
		_ = os.RemoveAll(r.rawArtifacts)
	}
	if r.codexHome != "" {
		_ = os.RemoveAll(filepath.Join(r.codexHome, "sessions"))
	}
}
