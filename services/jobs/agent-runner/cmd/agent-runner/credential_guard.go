package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	credentialEventSyncTimeout          = 2 * time.Second
	rotatingCredentialPollInterval      = 100 * time.Millisecond
	rotatingCredentialFinalReadAttempts = 20
	rotatingCredentialFinalReadDelay    = 25 * time.Millisecond
)

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

type unsupportedCredentialProviderError struct{}

func (unsupportedCredentialProviderError) Error() string {
	return "kubeconfig содержит неподдерживаемый динамический источник авторизации; запуск отклонён"
}

type rotatingCredentialCaptureError struct{}

func (rotatingCredentialCaptureError) Error() string {
	return "rotating credential source could not be captured safely"
}

type credentialSourceSnapshot struct {
	sourcePath string
	digest     [sha256.Size]byte
	exists     bool
	info       os.FileInfo
}

type credentialEventWatcher interface {
	Add(string) error
	Close() error
	Events() <-chan fsnotify.Event
	Errors() <-chan error
	TriggerSync(string) error
}

type fsnotifyCredentialEventWatcher struct {
	watcher *fsnotify.Watcher
}

func newFSNotifyCredentialEventWatcher() (credentialEventWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &fsnotifyCredentialEventWatcher{watcher: watcher}, nil
}

func (watcher *fsnotifyCredentialEventWatcher) Add(path string) error {
	return watcher.watcher.Add(path)
}

func (watcher *fsnotifyCredentialEventWatcher) Close() error {
	return watcher.watcher.Close()
}

func (watcher *fsnotifyCredentialEventWatcher) Events() <-chan fsnotify.Event {
	return watcher.watcher.Events
}

func (watcher *fsnotifyCredentialEventWatcher) Errors() <-chan error {
	return watcher.watcher.Errors
}

func (watcher *fsnotifyCredentialEventWatcher) TriggerSync(path string) error {
	return os.WriteFile(path, nil, 0o600)
}

type credentialRunGuard struct {
	runner              *runner
	cancel              context.CancelFunc
	snapshotRoot        string
	runtimeHome         string
	syncRuntimeSessions bool
	sources             []credentialSourceSnapshot
	watcher             credentialEventWatcher
	barrierRoot         string
	done                chan struct{}
	started             atomic.Bool
	rotated             atomic.Bool
	closing             atomic.Bool
	closeOnce           sync.Once
	syncIndex           atomic.Uint64

	watchMutex    sync.RWMutex
	watchedDirs   map[string]bool
	relevantPaths map[string]struct{}
	strictDirs    map[string]struct{}
	syncWaiters   map[string]chan struct{}

	rotatingMutex      sync.Mutex
	rotatingPaths      map[string]struct{}
	rotatingValues     map[string]struct{}
	rotatingExactOnly  map[string]struct{}
	rotatingSources    map[string]int64
	rotatingDigests    map[string][sha256.Size]byte
	rotatingCaptureErr error
	rotatingStop       chan struct{}
	rotatingDone       chan struct{}
	rotatingStarted    atomic.Bool
	rotatingCloseOnce  sync.Once
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
	snapshotRoot, err := os.MkdirTemp(r.ephemeralRoot, "credential-snapshot-")
	if err != nil {
		return nil, nil, fmt.Errorf("эфемерный снимок credential sources не создан")
	}
	if err := os.Chmod(snapshotRoot, 0o700); err != nil {
		_ = os.RemoveAll(snapshotRoot)
		return nil, nil, fmt.Errorf("эфемерный снимок credential sources не защищён")
	}
	commandContext, cancel := context.WithCancel(ctx)
	guard, err := r.newCredentialRunGuard(cancel, snapshotRoot)
	if err != nil {
		cancel()
		_ = os.RemoveAll(snapshotRoot)
		return nil, nil, err
	}
	guardedEnvironment, inventory, err := r.snapshotCredentialEnvironment(environment, guard)
	if err != nil {
		guard.abort()
		return nil, nil, err
	}
	merged, err := r.secrets.merge(inventory)
	if err != nil {
		guard.abort()
		return nil, nil, err
	}
	r.secrets = merged
	if err := r.secrets.validateForExecution(); err != nil {
		guard.abort()
		return nil, nil, err
	}
	cmd := r.command(commandContext, name, args...)
	cmd.Env = guardedEnvironment
	return cmd, guard, nil
}

func (r *runner) readCredentialFileWithEventGuard(ctx context.Context, path string) ([]byte, error) {
	if err := r.prepareEphemeralRuntime(); err != nil {
		return nil, err
	}
	snapshotRoot, err := os.MkdirTemp(r.ephemeralRoot, "credential-read-")
	if err != nil {
		return nil, fmt.Errorf("эфемерная область чтения credential source не создана")
	}
	if err := os.Chmod(snapshotRoot, 0o700); err != nil {
		_ = os.RemoveAll(snapshotRoot)
		return nil, fmt.Errorf("эфемерная область чтения credential source не защищена")
	}
	_, cancel := context.WithCancel(ctx)
	guard, err := r.newCredentialRunGuard(cancel, snapshotRoot)
	if err != nil {
		cancel()
		_ = os.RemoveAll(snapshotRoot)
		return nil, err
	}
	builder := &credentialSnapshotBuilder{
		guard:               guard,
		values:              map[string]struct{}{},
		exactOnlyValues:     map[string]struct{}{},
		sources:             map[string]int64{},
		readSources:         map[string]credentialReadResult{},
		snapshots:           map[string]string{},
		rewrites:            map[string]string{},
		kubeconfigSnapshots: map[string]string{},
	}
	result, err := builder.readSource(path)
	if err != nil || !result.exists {
		guard.abort()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("credential source отсутствует")
	}
	collectCredentialFileValues(builder.values, builder.exactOnlyValues, result.body)
	inventory, err := compileSecretInventory(builder.values, builder.sources, builder.exactOnlyValues)
	if err != nil {
		guard.abort()
		return nil, err
	}
	merged, err := r.secrets.merge(inventory)
	if err != nil {
		guard.abort()
		return nil, err
	}
	if err := merged.validateForExecution(); err != nil {
		guard.abort()
		return nil, err
	}
	r.secrets = merged
	if err := guard.start(); err != nil {
		return nil, guard.finish(err)
	}
	if err := guard.finish(nil); err != nil {
		return nil, err
	}
	return append([]byte(nil), result.body...), nil
}

func (r *runner) newCredentialRunGuard(cancel context.CancelFunc, snapshotRoot string) (*credentialRunGuard, error) {
	factory := r.credentialWatcherFactory
	if factory == nil {
		factory = newFSNotifyCredentialEventWatcher
	}
	watcher, err := factory()
	if err != nil {
		return nil, fmt.Errorf("event guard credential sources не создан")
	}
	barrierRoot := filepath.Join(snapshotRoot, "event-barrier")
	if err := os.Mkdir(barrierRoot, 0o700); err != nil {
		_ = watcher.Close()
		return nil, fmt.Errorf("event guard credential sources не подготовлен")
	}
	guard := &credentialRunGuard{
		runner:            r,
		cancel:            cancel,
		snapshotRoot:      snapshotRoot,
		watcher:           watcher,
		barrierRoot:       barrierRoot,
		done:              make(chan struct{}),
		watchedDirs:       map[string]bool{},
		relevantPaths:     map[string]struct{}{},
		strictDirs:        map[string]struct{}{},
		syncWaiters:       map[string]chan struct{}{},
		rotatingPaths:     map[string]struct{}{},
		rotatingValues:    map[string]struct{}{},
		rotatingExactOnly: map[string]struct{}{},
		rotatingSources:   map[string]int64{},
		rotatingDigests:   map[string][sha256.Size]byte{},
		rotatingStop:      make(chan struct{}),
		rotatingDone:      make(chan struct{}),
	}
	if err := guard.addWatchDirectory(barrierRoot, false); err != nil {
		_ = watcher.Close()
		return nil, err
	}
	go guard.consumeEvents()
	return guard, nil
}

func (r *runner) snapshotCredentialEnvironment(environment []string, guard *credentialRunGuard) ([]string, secretInventory, error) {
	builder := &credentialSnapshotBuilder{
		guard:               guard,
		values:              map[string]struct{}{},
		exactOnlyValues:     map[string]struct{}{},
		sources:             map[string]int64{},
		readSources:         map[string]credentialReadResult{},
		snapshots:           map[string]string{},
		rewrites:            map[string]string{},
		kubeconfigSnapshots: map[string]string{},
	}
	collectEnvironmentSecretValues(builder.values, builder.exactOnlyValues, environment)

	kubePaths := kubeconfigFileSources(environment)
	canonicalKubePaths := map[string]struct{}{}
	for _, path := range kubePaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		canonical, err := canonicalCredentialPath(path)
		if err != nil {
			return nil, secretInventory{}, err
		}
		canonicalKubePaths[canonical] = struct{}{}
		if len(canonicalKubePaths) > maxKubeconfigSources {
			return nil, secretInventory{}, credentialCorpusLimitError{Limit: "числа KUBECONFIG sources"}
		}
	}
	for _, path := range kubePaths {
		if _, err := builder.snapshotKubeconfig(path); err != nil {
			return nil, secretInventory{}, err
		}
	}

	paths := credentialFileEnvironmentSources(environment)
	if r.credentialFiles == nil {
		paths = append(defaultCredentialFileSources(), paths...)
	} else {
		paths = append(paths, r.credentialFiles...)
	}
	if runtimeHome := environmentValue(environment, "CODEX_HOME"); runtimeHome != "" {
		paths = append(paths, filepath.Join(runtimeHome, "auth.json"))
	}
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		normalized, err := absoluteCredentialPath(path)
		if err != nil {
			return nil, secretInventory{}, err
		}
		if _, isKubeconfig := builder.kubeconfigSnapshots[normalized]; isKubeconfig {
			continue
		}
		if _, err := builder.snapshotFile(path, false); err != nil {
			return nil, secretInventory{}, err
		}
	}
	rotatingPaths := defaultRotatingCredentialFileSources()
	if r.rotatingCredentialFiles != nil {
		rotatingPaths = r.rotatingCredentialFiles
	}
	for _, path := range rotatingPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := guard.registerRotatingCredentialPath(path); err != nil {
			return nil, secretInventory{}, err
		}
	}

	runtimeHome, syncRuntimeSessions, err := builder.snapshotRuntimeCodexHome(environment)
	if err != nil {
		return nil, secretInventory{}, err
	}
	guard.runtimeHome = runtimeHome
	guard.syncRuntimeSessions = syncRuntimeSessions
	if guard.rotated.Load() || guard.sourcesChanged(true) {
		guard.markRotated()
		return nil, secretInventory{}, credentialRotationError{}
	}
	inventory, err := compileSecretInventory(builder.values, builder.sources, builder.exactOnlyValues)
	if err != nil {
		return nil, secretInventory{}, err
	}
	rotatingInventory, err := guard.rotatingCredentialInventory()
	if err != nil {
		return nil, secretInventory{}, err
	}
	inventory, err = inventory.merge(rotatingInventory)
	if err != nil {
		return nil, secretInventory{}, err
	}
	result := rewriteCredentialFileEnvironment(environment, builder.rewrites)
	if runtimeHome != "" {
		result = replaceEnvironmentValue(result, "CODEX_HOME", runtimeHome)
	}
	return result, inventory, nil
}

type credentialReadResult struct {
	body      []byte
	exists    bool
	canonical string
}

type credentialSnapshotBuilder struct {
	guard               *credentialRunGuard
	values              map[string]struct{}
	exactOnlyValues     map[string]struct{}
	sources             map[string]int64
	readSources         map[string]credentialReadResult
	snapshots           map[string]string
	rewrites            map[string]string
	kubeconfigSnapshots map[string]string
	nextSnapshot        int
}

func (builder *credentialSnapshotBuilder) readSource(rawPath string) (credentialReadResult, error) {
	requested, canonical, err := builder.guard.watchCredentialPath(rawPath)
	if err != nil {
		return credentialReadResult{}, err
	}
	if cached, exists := builder.readSources[canonical]; exists {
		if err := builder.guard.captureSourceBaseline(requested, cached.body, cached.exists); err != nil {
			return credentialReadResult{}, err
		}
		return cached, nil
	}
	info, err := os.Stat(requested)
	if err != nil {
		if os.IsNotExist(err) {
			result := credentialReadResult{canonical: canonical}
			builder.readSources[canonical] = result
			if err := builder.guard.captureSourceBaseline(requested, nil, false); err != nil {
				return credentialReadResult{}, err
			}
			return result, nil
		}
		return credentialReadResult{}, fmt.Errorf("credential source недоступен")
	}
	if !info.Mode().IsRegular() {
		return credentialReadResult{}, fmt.Errorf("credential source не является обычным файлом")
	}
	if info.Size() < 0 || info.Size() > maxCredentialFileBytes {
		return credentialReadResult{}, boundedFileError{Kind: "credential source"}
	}
	if err := addCredentialSourceBudget(builder.sources, canonical, info.Size()); err != nil {
		return credentialReadResult{}, err
	}
	body, err := os.ReadFile(requested)
	if err != nil {
		return credentialReadResult{}, fmt.Errorf("credential source не прочитан")
	}
	result := credentialReadResult{body: body, exists: true, canonical: canonical}
	builder.readSources[canonical] = result
	if err := builder.guard.captureSourceBaseline(requested, body, true); err != nil {
		return credentialReadResult{}, err
	}
	if builder.guard.rotated.Load() {
		return credentialReadResult{}, credentialRotationError{}
	}
	return result, nil
}

func (builder *credentialSnapshotBuilder) snapshotFile(rawPath string, wholeValue bool) (string, error) {
	result, err := builder.readSource(rawPath)
	if err != nil {
		return "", err
	}
	requested, err := absoluteCredentialPath(rawPath)
	if err != nil {
		return "", err
	}
	if wholeValue && result.exists {
		addSecretValue(builder.values, strings.TrimSpace(string(result.body)))
	} else if result.exists {
		collectCredentialFileValues(builder.values, builder.exactOnlyValues, result.body)
	}
	if snapshot, exists := builder.snapshots[result.canonical]; exists {
		builder.rewrites[requested] = snapshot
		return snapshot, nil
	}
	snapshot := builder.nextSnapshotPath()
	if result.exists {
		if err := os.WriteFile(snapshot, result.body, 0o600); err != nil {
			return "", fmt.Errorf("эфемерный снимок credential source не записан")
		}
	}
	builder.snapshots[result.canonical] = snapshot
	builder.rewrites[requested] = snapshot
	return snapshot, nil
}

func (builder *credentialSnapshotBuilder) snapshotKubeconfig(rawPath string) (string, error) {
	requested, err := absoluteCredentialPath(rawPath)
	if err != nil {
		return "", err
	}
	if snapshot, exists := builder.kubeconfigSnapshots[requested]; exists {
		return snapshot, nil
	}
	result, err := builder.readSource(rawPath)
	if err != nil {
		return "", err
	}
	snapshot := builder.nextSnapshotPath()
	if !result.exists {
		builder.kubeconfigSnapshots[requested] = snapshot
		builder.rewrites[requested] = snapshot
		return snapshot, nil
	}
	config, err := clientcmd.Load(result.body)
	if err != nil {
		return "", fmt.Errorf("kubeconfig не прошёл структурную проверку")
	}
	if len(config.Extensions) > 0 || len(config.Preferences.Extensions) > 0 {
		return "", unsupportedCredentialProviderError{}
	}
	authNames := sortedMapKeys(config.AuthInfos)
	for _, name := range authNames {
		authInfo := config.AuthInfos[name]
		if authInfo == nil || authInfo.Exec != nil || authInfo.AuthProvider != nil || len(authInfo.Extensions) > 0 {
			return "", unsupportedCredentialProviderError{}
		}
		for _, value := range []string{authInfo.Token, authInfo.Password, string(authInfo.ClientCertificateData), string(authInfo.ClientKeyData)} {
			addSecretValue(builder.values, value)
		}
		if authInfo.Username != "" && authInfo.Password != "" {
			addSecretValue(builder.values, authInfo.Username+":"+authInfo.Password)
		}
		if authInfo.TokenFile != "" {
			authInfo.TokenFile, err = builder.snapshotKubeconfigReference(rawPath, authInfo.TokenFile, true)
			if err != nil {
				return "", err
			}
		}
		if authInfo.ClientKey != "" {
			authInfo.ClientKey, err = builder.snapshotKubeconfigReference(rawPath, authInfo.ClientKey, true)
			if err != nil {
				return "", err
			}
		}
		if authInfo.ClientCertificate != "" {
			authInfo.ClientCertificate, err = builder.snapshotKubeconfigReference(rawPath, authInfo.ClientCertificate, true)
			if err != nil {
				return "", err
			}
		}
	}
	clusterNames := sortedMapKeys(config.Clusters)
	for _, name := range clusterNames {
		cluster := config.Clusters[name]
		if cluster == nil || len(cluster.Extensions) > 0 {
			return "", unsupportedCredentialProviderError{}
		}
		if cluster.CertificateAuthority != "" {
			cluster.CertificateAuthority, err = builder.snapshotKubeconfigReference(rawPath, cluster.CertificateAuthority, false)
			if err != nil {
				return "", err
			}
		}
		for _, endpoint := range []string{cluster.Server, cluster.ProxyURL} {
			if err := collectURLCredentialValues(builder.values, endpoint); err != nil {
				return "", err
			}
		}
	}
	for _, contextName := range sortedMapKeys(config.Contexts) {
		if context := config.Contexts[contextName]; context == nil || len(context.Extensions) > 0 {
			return "", unsupportedCredentialProviderError{}
		}
	}
	body, err := clientcmd.Write(*config)
	if err != nil {
		return "", fmt.Errorf("kubeconfig snapshot не сериализован")
	}
	if err := os.WriteFile(snapshot, body, 0o600); err != nil {
		return "", fmt.Errorf("kubeconfig snapshot не записан")
	}
	builder.kubeconfigSnapshots[requested] = snapshot
	builder.snapshots[result.canonical] = snapshot
	builder.rewrites[requested] = snapshot
	return snapshot, nil
}

func collectURLCredentialValues(values map[string]struct{}, rawURL string) error {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("kubeconfig URL недопустим")
	}
	if parsed.User == nil {
		return nil
	}
	addSecretValue(values, parsed.User.String())
	if password, ok := parsed.User.Password(); ok {
		addSecretValue(values, password)
		addSecretValue(values, parsed.User.Username()+":"+password)
	}
	return nil
}

func (builder *credentialSnapshotBuilder) snapshotKubeconfigReference(kubeconfigPath string, reference string, wholeValue bool) (string, error) {
	if !filepath.IsAbs(reference) {
		base, err := absoluteCredentialPath(kubeconfigPath)
		if err != nil {
			return "", err
		}
		reference = filepath.Join(filepath.Dir(base), reference)
	}
	return builder.snapshotFile(reference, wholeValue)
}

func (builder *credentialSnapshotBuilder) snapshotRuntimeCodexHome(environment []string) (string, bool, error) {
	sourceHome := strings.TrimSpace(environmentValue(environment, "CODEX_HOME"))
	if sourceHome == "" {
		return "", false, nil
	}
	sourceHome, err := absoluteCredentialPath(sourceHome)
	if err != nil {
		return "", false, err
	}
	runtimeHome := filepath.Join(builder.guard.snapshotRoot, "runtime-codex-home")
	if err := os.Mkdir(runtimeHome, 0o700); err != nil {
		return "", false, fmt.Errorf("runtime CODEX_HOME snapshot не создан")
	}
	configPath := filepath.Join(sourceHome, "config.toml")
	if err := copyBoundedRuntimeFile(configPath, filepath.Join(runtimeHome, "config.toml")); err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	authSource := filepath.Join(sourceHome, "auth.json")
	auth, err := builder.readSource(authSource)
	if err != nil {
		return "", false, err
	}
	authTarget := filepath.Join(runtimeHome, "auth.json")
	if auth.exists {
		collectCredentialFileValues(builder.values, builder.exactOnlyValues, auth.body)
		if err := os.WriteFile(authTarget, auth.body, 0o600); err != nil {
			return "", false, fmt.Errorf("runtime auth snapshot не записан")
		}
	}
	syncSessions := sourceHome == builder.guard.runner.codexHome
	if syncSessions {
		if err := copySessionTree(filepath.Join(sourceHome, "sessions"), filepath.Join(runtimeHome, "sessions")); err != nil {
			return "", false, err
		}
	}
	if err := builder.guard.watchRuntimeAuthCopy(authTarget, auth.body, auth.exists); err != nil {
		return "", false, err
	}
	return runtimeHome, syncSessions, nil
}

func (builder *credentialSnapshotBuilder) nextSnapshotPath() string {
	path := filepath.Join(builder.guard.snapshotRoot, fmt.Sprintf("source-%04d", builder.nextSnapshot))
	builder.nextSnapshot++
	return path
}

func sortedMapKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyBoundedRuntimeFile(source string, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxCredentialFileBytes {
		return boundedFileError{Kind: "runtime config"}
	}
	body, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("runtime config не прочитан")
	}
	return os.WriteFile(target, body, 0o600)
}

func copySessionTree(source string, target string) error {
	info, err := os.Stat(source)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("каталог сессий имеет недопустимый тип")
	}
	fileCount := 0
	entryCount := 0
	totalBytes := int64(0)
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		entryCount++
		if entryCount > maxSessionArchiveEntries {
			return sessionArchiveLimitError{Limit: "количества записей"}
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !fileInfo.Mode().IsRegular() {
			return fmt.Errorf("каталог сессий содержит недопустимый тип файла")
		}
		fileCount++
		totalBytes += fileInfo.Size()
		if fileCount > maxSessionArchiveFiles || fileInfo.Size() < 0 || fileInfo.Size() > maxSessionArchiveFileBytes {
			return sessionArchiveLimitError{Limit: "размера или количества файлов"}
		}
		if totalBytes > maxSessionArchiveTotalBytes {
			return sessionArchiveLimitError{Limit: "общего размера"}
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.CopyN(output, input, fileInfo.Size())
		inputCloseErr := input.Close()
		closeErr := output.Close()
		return errors.Join(copyErr, inputCloseErr, closeErr)
	})
}

func addCredentialSourceBudget(sources map[string]int64, canonical string, size int64) error {
	if previous, exists := sources[canonical]; exists {
		if size > previous {
			sources[canonical] = size
		}
	} else {
		sources[canonical] = size
	}
	return validateCredentialSourceBudget(sources)
}

func validateCredentialSourceBudget(sources map[string]int64) error {
	if len(sources) > maxCredentialSources {
		return credentialCorpusLimitError{Limit: "числа canonical sources"}
	}
	total := int64(0)
	for _, size := range sources {
		total += size
		if total > maxCredentialSourceBytes {
			return credentialCorpusLimitError{Limit: "общего размера sources"}
		}
	}
	return nil
}

func absoluteCredentialPath(rawPath string) (string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return "", fmt.Errorf("credential source path пуст")
	}
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("credential source path не канонизирован")
	}
	return absolute, nil
}

func canonicalCredentialPath(rawPath string) (string, error) {
	absolute, err := absoluteCredentialPath(rawPath)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err == nil {
		return filepath.Clean(canonical), nil
	}
	if os.IsNotExist(err) {
		return absolute, nil
	}
	return "", fmt.Errorf("credential source path не канонизирован")
}

func kubeconfigFileSources(environment []string) []string {
	for _, item := range environment {
		name, value, ok := strings.Cut(item, "=")
		if ok && name == "KUBECONFIG" {
			return strings.Split(value, string(os.PathListSeparator))
		}
	}
	return nil
}

func environmentValue(environment []string, target string) string {
	for index := len(environment) - 1; index >= 0; index-- {
		name, value, ok := strings.Cut(environment[index], "=")
		if ok && name == target {
			return value
		}
	}
	return ""
}

func replaceEnvironmentValue(environment []string, target string, value string) []string {
	result := append([]string(nil), environment...)
	for index := range result {
		name, _, ok := strings.Cut(result[index], "=")
		if ok && name == target {
			result[index] = target + "=" + value
			return result
		}
	}
	return append(result, target+"="+value)
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
			normalized, err := absoluteCredentialPath(rawPath)
			if err != nil {
				continue
			}
			if replacement, exists := rewrites[normalized]; exists {
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

func (guard *credentialRunGuard) registerRotatingCredentialPath(rawPath string) error {
	path, err := absoluteCredentialPath(rawPath)
	if err != nil {
		return err
	}
	body, exists, err := readCredentialSource(path)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	guard.rotatingMutex.Lock()
	guard.rotatingPaths[path] = struct{}{}
	guard.rotatingMutex.Unlock()
	return guard.captureRotatingCredential(path, body)
}

func (guard *credentialRunGuard) captureRotatingCredential(path string, body []byte) error {
	if strings.TrimSpace(string(body)) == "" {
		return rotatingCredentialCaptureError{}
	}
	digest := sha256.Sum256(body)
	guard.rotatingMutex.Lock()
	defer guard.rotatingMutex.Unlock()
	if previous, exists := guard.rotatingDigests[path]; exists && previous == digest {
		return nil
	}
	values := make(map[string]struct{}, len(guard.rotatingValues)+1)
	exactOnly := make(map[string]struct{}, len(guard.rotatingExactOnly))
	sources := make(map[string]int64, len(guard.rotatingSources)+1)
	for value := range guard.rotatingValues {
		values[value] = struct{}{}
	}
	for value := range guard.rotatingExactOnly {
		exactOnly[value] = struct{}{}
	}
	for source, size := range guard.rotatingSources {
		sources[source] = size
	}
	collectCredentialFileValues(values, exactOnly, body)
	if size := int64(len(body)); size > sources[path] {
		sources[path] = size
	}
	if err := validateCredentialSourceBudget(sources); err != nil {
		return err
	}
	inventory, err := compileSecretInventory(values, sources, exactOnly)
	if err != nil {
		return err
	}
	if err := inventory.validateForExecution(); err != nil {
		return err
	}
	guard.rotatingValues = values
	guard.rotatingExactOnly = exactOnly
	guard.rotatingSources = sources
	guard.rotatingDigests[path] = digest
	return nil
}

func (guard *credentialRunGuard) rotatingCredentialInventory() (secretInventory, error) {
	guard.rotatingMutex.Lock()
	defer guard.rotatingMutex.Unlock()
	values := make(map[string]struct{}, len(guard.rotatingValues))
	exactOnly := make(map[string]struct{}, len(guard.rotatingExactOnly))
	sources := make(map[string]int64, len(guard.rotatingSources))
	for value := range guard.rotatingValues {
		values[value] = struct{}{}
	}
	for value := range guard.rotatingExactOnly {
		exactOnly[value] = struct{}{}
	}
	for source, size := range guard.rotatingSources {
		sources[source] = size
	}
	inventory, err := compileSecretInventory(values, sources, exactOnly)
	if err != nil {
		return secretInventory{}, err
	}
	if err := inventory.validateForExecution(); err != nil {
		return secretInventory{}, err
	}
	return inventory, nil
}

func (guard *credentialRunGuard) rotatingCredentialPaths() []string {
	guard.rotatingMutex.Lock()
	defer guard.rotatingMutex.Unlock()
	paths := make([]string, 0, len(guard.rotatingPaths))
	for path := range guard.rotatingPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (guard *credentialRunGuard) captureCurrentRotatingCredentials(final bool) error {
	for _, path := range guard.rotatingCredentialPaths() {
		var lastErr error
		attempts := 1
		if final {
			attempts = rotatingCredentialFinalReadAttempts
		}
		for attempt := 0; attempt < attempts; attempt++ {
			body, exists, err := readCredentialSource(path)
			if err == nil && exists {
				if err := guard.captureRotatingCredential(path, body); err != nil {
					return err
				}
				lastErr = nil
				break
			}
			lastErr = err
			if lastErr == nil {
				lastErr = rotatingCredentialCaptureError{}
			}
			if attempt+1 < attempts {
				time.Sleep(rotatingCredentialFinalReadDelay)
			}
		}
		if final && lastErr != nil {
			return rotatingCredentialCaptureError{}
		}
	}
	return nil
}

func (guard *credentialRunGuard) startRotatingCredentialCapture() {
	if len(guard.rotatingCredentialPaths()) == 0 || guard.rotatingStarted.Swap(true) {
		return
	}
	go func() {
		defer close(guard.rotatingDone)
		ticker := time.NewTicker(rotatingCredentialPollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := guard.captureCurrentRotatingCredentials(false); err != nil {
					guard.markRotatingCredentialCaptureFailed(err)
					return
				}
			case <-guard.rotatingStop:
				return
			}
		}
	}()
}

func (guard *credentialRunGuard) stopRotatingCredentialCapture() {
	if !guard.rotatingStarted.Load() {
		return
	}
	guard.rotatingCloseOnce.Do(func() {
		close(guard.rotatingStop)
		<-guard.rotatingDone
	})
}

func (guard *credentialRunGuard) markRotatingCredentialCaptureFailed(err error) {
	if err == nil {
		err = rotatingCredentialCaptureError{}
	}
	guard.rotatingMutex.Lock()
	if guard.rotatingCaptureErr == nil {
		guard.rotatingCaptureErr = err
	}
	guard.rotatingMutex.Unlock()
	guard.runner.safety.markUnsafe()
	guard.cancel()
	guard.runner.discardUnsafeRunData()
}

func (guard *credentialRunGuard) rotatingCredentialError() error {
	guard.rotatingMutex.Lock()
	defer guard.rotatingMutex.Unlock()
	return guard.rotatingCaptureErr
}

func (guard *credentialRunGuard) watchCredentialPath(rawPath string) (string, string, error) {
	requested, err := absoluteCredentialPath(rawPath)
	if err != nil {
		return "", "", err
	}
	if err := guard.watchExactPath(requested, false); err != nil {
		return "", "", err
	}
	info, lstatErr := os.Lstat(requested)
	if lstatErr != nil && !os.IsNotExist(lstatErr) {
		return "", "", fmt.Errorf("credential source path недоступен для event guard")
	}
	if lstatErr == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := guard.addWatchDirectory(filepath.Dir(requested), true); err != nil {
			return "", "", err
		}
	}
	canonical, err := canonicalCredentialPath(requested)
	if err != nil {
		return "", "", err
	}
	if canonical != requested {
		if err := guard.watchExactPath(canonical, false); err != nil {
			return "", "", err
		}
	}
	return requested, canonical, nil
}

func (guard *credentialRunGuard) watchExactPath(path string, strictParent bool) error {
	parent := filepath.Dir(path)
	guard.watchMutex.Lock()
	guard.relevantPaths[path] = struct{}{}
	guard.relevantPaths[parent] = struct{}{}
	guard.watchMutex.Unlock()
	if err := guard.addWatchDirectory(parent, strictParent); err != nil {
		return err
	}
	grandparent := filepath.Dir(parent)
	if grandparent != parent {
		if err := guard.addWatchDirectory(grandparent, false); err != nil {
			return err
		}
	}
	return nil
}

func (guard *credentialRunGuard) addWatchDirectory(path string, strict bool) error {
	requested := filepath.Clean(path)
	watchPath := requested
	for {
		info, err := os.Stat(watchPath)
		if err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(watchPath)
		if parent == watchPath {
			return fmt.Errorf("parent directory credential source недоступен для event guard")
		}
		watchPath = parent
		strict = true
	}
	guard.watchMutex.Lock()
	defer guard.watchMutex.Unlock()
	if strict || watchPath != requested {
		guard.strictDirs[watchPath] = struct{}{}
	}
	if guard.watchedDirs[watchPath] {
		return nil
	}
	if err := guard.watcher.Add(watchPath); err != nil {
		return fmt.Errorf("parent directory credential source не поставлен под event guard")
	}
	guard.watchedDirs[watchPath] = true
	return nil
}

func (guard *credentialRunGuard) captureSourceBaseline(path string, body []byte, exists bool) error {
	var info os.FileInfo
	if exists {
		current, err := os.Stat(path)
		if err != nil || !current.Mode().IsRegular() || current.Size() != int64(len(body)) {
			guard.markRotated()
			return credentialRotationError{}
		}
		info = current
	} else if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
		guard.markRotated()
		return credentialRotationError{}
	}
	guard.sources = append(guard.sources, credentialSourceSnapshot{
		sourcePath: path,
		digest:     sha256.Sum256(body),
		exists:     exists,
		info:       info,
	})
	return nil
}

func (guard *credentialRunGuard) watchRuntimeAuthCopy(path string, body []byte, exists bool) error {
	requested, _, err := guard.watchCredentialPath(path)
	if err != nil {
		return err
	}
	return guard.captureSourceBaseline(requested, body, exists)
}

func (guard *credentialRunGuard) consumeEvents() {
	defer close(guard.done)
	events := guard.watcher.Events()
	errorsChannel := guard.watcher.Errors()
	for events != nil || errorsChannel != nil {
		select {
		case event, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			guard.handleEvent(event)
		case _, ok := <-errorsChannel:
			if !ok {
				errorsChannel = nil
				continue
			}
			if !guard.closing.Load() {
				guard.markRotated()
			}
		}
	}
}

func (guard *credentialRunGuard) handleEvent(event fsnotify.Event) {
	name := filepath.Clean(event.Name)
	guard.watchMutex.RLock()
	if waiter := guard.syncWaiters[name]; waiter != nil {
		guard.watchMutex.RUnlock()
		select {
		case waiter <- struct{}{}:
		default:
		}
		return
	}
	_, relevant := guard.relevantPaths[name]
	_, strict := guard.strictDirs[filepath.Dir(name)]
	guard.watchMutex.RUnlock()
	if event.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename|fsnotify.Chmod) != 0 && (relevant || strict) {
		guard.markRotated()
	}
}

func (guard *credentialRunGuard) syncEvents() error {
	if guard.rotated.Load() {
		return credentialRotationError{}
	}
	path := filepath.Join(guard.barrierRoot, fmt.Sprintf("sync-%d", guard.syncIndex.Add(1)))
	waiter := make(chan struct{}, 1)
	guard.watchMutex.Lock()
	guard.syncWaiters[path] = waiter
	guard.watchMutex.Unlock()
	defer func() {
		guard.watchMutex.Lock()
		delete(guard.syncWaiters, path)
		guard.watchMutex.Unlock()
	}()
	if err := guard.watcher.TriggerSync(path); err != nil {
		guard.markRotated()
		return credentialRotationError{}
	}
	timer := time.NewTimer(credentialEventSyncTimeout)
	defer timer.Stop()
	select {
	case <-waiter:
	case <-timer.C:
		guard.markRotated()
		return credentialRotationError{}
	case <-guard.done:
		guard.markRotated()
		return credentialRotationError{}
	}
	if guard.rotated.Load() {
		return credentialRotationError{}
	}
	return nil
}

func (guard *credentialRunGuard) start() error {
	if guard == nil || guard.started.Swap(true) {
		return nil
	}
	if err := guard.syncEvents(); err != nil {
		return err
	}
	if guard.sourcesChanged(true) {
		guard.markRotated()
		return credentialRotationError{}
	}
	guard.startRotatingCredentialCapture()
	return nil
}

func (guard *credentialRunGuard) finish(runErr error) error {
	if guard == nil {
		return runErr
	}
	guard.stopRotatingCredentialCapture()
	if err := guard.captureCurrentRotatingCredentials(true); err != nil {
		guard.markRotatingCredentialCaptureFailed(err)
	}
	rotatingInventory, err := guard.rotatingCredentialInventory()
	if err != nil {
		guard.markRotatingCredentialCaptureFailed(err)
	} else {
		merged, mergeErr := guard.runner.secrets.merge(rotatingInventory)
		if mergeErr != nil {
			guard.markRotatingCredentialCaptureFailed(mergeErr)
		} else {
			guard.runner.secrets = merged
		}
	}
	if guard.started.Load() {
		if err := guard.syncEvents(); err != nil {
			runErr = errors.Join(runErr, err)
		}
	}
	if guard.sourcesChanged(true) {
		guard.markRotated()
	}
	guard.closeWatcher()
	guard.cancel()
	if captureErr := guard.rotatingCredentialError(); captureErr != nil {
		guard.runner.discardUnsafeRunData()
		_ = os.RemoveAll(guard.snapshotRoot)
		return errors.Join(runErr, captureErr)
	}
	if guard.rotated.Load() {
		guard.runner.discardUnsafeRunData()
		_ = os.RemoveAll(guard.snapshotRoot)
		return errors.Join(runErr, credentialRotationError{})
	}
	if guard.runtimeHome != "" && guard.syncRuntimeSessions {
		sourceSessions := filepath.Join(guard.runtimeHome, "sessions")
		if _, err := os.Stat(sourceSessions); err == nil {
			if err := replaceDirectoryAtomically(sourceSessions, filepath.Join(guard.runner.codexHome, "sessions")); err != nil {
				runErr = errors.Join(runErr, err)
			}
		} else if !os.IsNotExist(err) {
			runErr = errors.Join(runErr, err)
		}
	}
	_ = os.RemoveAll(guard.snapshotRoot)
	return runErr
}

func (guard *credentialRunGuard) abort() {
	if guard == nil {
		return
	}
	guard.stopRotatingCredentialCapture()
	guard.closeWatcher()
	guard.cancel()
	_ = os.RemoveAll(guard.snapshotRoot)
}

func (guard *credentialRunGuard) closeWatcher() {
	guard.closeOnce.Do(func() {
		guard.closing.Store(true)
		_ = guard.watcher.Close()
		<-guard.done
	})
}

func (guard *credentialRunGuard) markRotated() {
	if guard.rotated.CompareAndSwap(false, true) {
		guard.runner.safety.markUnsafe()
		guard.cancel()
		guard.runner.discardUnsafeRunData()
		_ = os.RemoveAll(guard.snapshotRoot)
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

func (r *runner) discardUnsafeRunData() {
	if r.rawArtifacts != "" {
		_ = os.RemoveAll(r.rawArtifacts)
	}
	if r.codexHome != "" {
		_ = os.RemoveAll(filepath.Join(r.codexHome, "sessions"))
	}
}
