package main

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultHTTPTimeout    = 2 * time.Minute
	maxArchiveBytes       = 512 * 1024 * 1024
	maxExtractedFiles     = 32768
	maxExtractedFileBytes = 128 * 1024 * 1024
	maxManifestFiles      = 128
	maxManifestBytes      = 8 * 1024 * 1024
)

type report struct {
	SourceSnapshotRef     string            `json:"source_snapshot_ref"`
	SourceSnapshotDigest  string            `json:"source_snapshot_digest"`
	BuildContextDigest    string            `json:"build_context_digest"`
	ManifestBundleDigests map[string]string `json:"manifest_bundle_digests,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, safeError(err))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "github-archive" {
		return errors.New("unsupported materializer command")
	}
	fs := flag.NewFlagSet("github-archive", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var provider string
	var owner string
	var repo string
	var sourceRef string
	var commit string
	var output string
	var resultPath string
	var serviceKeys multiValue
	fs.StringVar(&provider, "provider", "", "provider slug")
	fs.StringVar(&owner, "owner", "", "repository owner")
	fs.StringVar(&repo, "repo", "", "repository name")
	fs.StringVar(&sourceRef, "source-ref", "", "source ref")
	fs.StringVar(&commit, "commit", "", "commit sha")
	fs.StringVar(&output, "output", "", "output directory")
	fs.StringVar(&resultPath, "result", "", "result file")
	fs.Var(&serviceKeys, "service-key", "affected service key")
	if err := fs.Parse(args[1:]); err != nil {
		return errors.New("invalid materializer arguments")
	}
	input, err := normalizeInput(provider, owner, repo, sourceRef, commit, output, resultPath, serviceKeys)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(input.output); err != nil {
		return errors.New("prepare output directory failed")
	}
	if err := os.MkdirAll(input.output, 0o750); err != nil {
		return errors.New("create output directory failed")
	}
	archivePath := filepath.Join(os.TempDir(), "kodex-github-archive.zip")
	if err := downloadArchive(ctx, input, archivePath); err != nil {
		return err
	}
	if err := extractArchive(archivePath, input.output); err != nil {
		return err
	}
	digest, err := digestDirectory(input.output)
	if err != nil {
		return err
	}
	manifestBundleDigests, err := digestManifestBundles(input.output, input.serviceKeys)
	if err != nil {
		return err
	}
	sourceRefValue := fmt.Sprintf("github://github.com/%s/%s#%s", input.owner, input.repo, input.commit)
	sourceDigest := digestString(strings.Join([]string{input.provider, input.owner, input.repo, input.sourceRef, input.commit}, "\x00"))
	result := report{
		SourceSnapshotRef:     sourceRefValue,
		SourceSnapshotDigest:  sourceDigest,
		BuildContextDigest:    digest,
		ManifestBundleDigests: manifestBundleDigests,
	}
	if err := writeReport(input.resultPath, result); err != nil {
		return err
	}
	encoded, _ := json.Marshal(result)
	fmt.Println(string(encoded))
	return nil
}

type materializerInput struct {
	provider    string
	owner       string
	repo        string
	sourceRef   string
	commit      string
	output      string
	resultPath  string
	serviceKeys []string
}

func normalizeInput(provider string, owner string, repo string, sourceRef string, commit string, output string, resultPath string, serviceKeys []string) (materializerInput, error) {
	input := materializerInput{
		provider:   strings.ToLower(strings.TrimSpace(provider)),
		owner:      strings.TrimSpace(owner),
		repo:       strings.TrimSpace(repo),
		sourceRef:  strings.TrimSpace(sourceRef),
		commit:     strings.ToLower(strings.TrimSpace(commit)),
		output:     filepath.Clean(strings.TrimSpace(output)),
		resultPath: filepath.Clean(strings.TrimSpace(resultPath)),
	}
	normalizedKeys, err := normalizeServiceKeys(serviceKeys)
	if err != nil {
		return materializerInput{}, err
	}
	input.serviceKeys = normalizedKeys
	if input.provider != "github" ||
		!safePathToken(input.owner, 128) ||
		!safePathToken(input.repo, 128) ||
		!safeRef(input.sourceRef) ||
		!validCommit(input.commit) ||
		input.output == "." || input.output == string(filepath.Separator) ||
		!strings.HasPrefix(input.output, string(filepath.Separator)) ||
		input.resultPath == "." || !strings.HasPrefix(input.resultPath, input.output+string(filepath.Separator)) {
		return materializerInput{}, errors.New("invalid materializer input")
	}
	return input, nil
}

type multiValue []string

func (m *multiValue) String() string {
	return strings.Join(*m, ",")
}

func (m *multiValue) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return errors.New("empty service key")
	}
	*m = append(*m, trimmed)
	return nil
}

func normalizeServiceKeys(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.TrimSpace(value)
		if key == "" {
			continue
		}
		if !safePathToken(key, 128) {
			return nil, errors.New("invalid service key")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	sort.Strings(result)
	return result, nil
}

func downloadArchive(ctx context.Context, input materializerInput, destination string) error {
	ctx, cancel := context.WithTimeout(ctx, defaultHTTPTimeout)
	defer cancel()
	archiveURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/zipball/%s", url.PathEscape(input.owner), url.PathEscape(input.repo), url.PathEscape(input.commit))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return errors.New("create archive request failed")
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "kodex-runtime-materializer")
	if token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return errors.New("download archive failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.New("download archive returned non-success status")
	}
	file, err := os.Create(destination)
	if err != nil {
		return errors.New("create archive file failed")
	}
	defer file.Close()
	if _, err := io.Copy(file, io.LimitReader(response.Body, maxArchiveBytes+1)); err != nil {
		return errors.New("store archive failed")
	}
	info, err := file.Stat()
	if err != nil {
		return errors.New("stat archive failed")
	}
	if info.Size() == 0 || info.Size() > maxArchiveBytes {
		return errors.New("archive size is invalid")
	}
	return nil
}

func extractArchive(archivePath string, output string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return errors.New("open archive failed")
	}
	defer reader.Close()
	if len(reader.File) == 0 || len(reader.File) > maxExtractedFiles {
		return errors.New("archive file count is invalid")
	}
	root := ""
	for _, file := range reader.File {
		name := strings.Trim(file.Name, "/")
		if name == "" {
			continue
		}
		part := strings.Split(name, "/")[0]
		if root == "" {
			root = part
		} else if root != part {
			return errors.New("archive root is invalid")
		}
	}
	if root == "" {
		return errors.New("archive root is missing")
	}
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxExtractedFileBytes {
			return errors.New("archive file size is invalid")
		}
		relative := strings.TrimPrefix(strings.Trim(file.Name, "/"), root+"/")
		cleaned, err := safeRelativePath(relative)
		if err != nil {
			return err
		}
		target := filepath.Join(output, filepath.FromSlash(cleaned))
		if !strings.HasPrefix(target, output+string(filepath.Separator)) {
			return errors.New("archive path escapes output")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return errors.New("create archive directory failed")
		}
		source, err := file.Open()
		if err != nil {
			return errors.New("open archive file failed")
		}
		err = writeExtractedFile(target, source)
		closeErr := source.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return errors.New("close archive file failed")
		}
	}
	return nil
}

func writeExtractedFile(target string, source io.Reader) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return errors.New("create extracted file failed")
	}
	defer file.Close()
	if _, err := io.Copy(file, io.LimitReader(source, maxExtractedFileBytes+1)); err != nil {
		return errors.New("write extracted file failed")
	}
	return nil
}

func digestDirectory(root string) (string, error) {
	files := []string{}
	if err := filepath.WalkDir(root, func(pathValue string, entry os.DirEntry, err error) error {
		if err != nil {
			return errors.New("walk materialized context failed")
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, pathValue)
		if err != nil {
			return errors.New("walk materialized context failed")
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	}); err != nil {
		return "", err
	}
	if len(files) == 0 || len(files) > maxExtractedFiles {
		return "", errors.New("materialized context file count is invalid")
	}
	sort.Strings(files)
	treeHash := sha256.New()
	for _, relative := range files {
		fileHash, size, err := digestFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(treeHash, "%s\x00%d\x00%s\n", relative, size, fileHash)
	}
	return "sha256:" + hex.EncodeToString(treeHash.Sum(nil)), nil
}

func digestFile(pathValue string) (string, int64, error) {
	file, err := os.Open(pathValue)
	if err != nil {
		return "", 0, errors.New("open materialized context file failed")
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(file, maxExtractedFileBytes+1))
	if err != nil {
		return "", 0, errors.New("digest materialized context file failed")
	}
	if size > maxExtractedFileBytes {
		return "", 0, errors.New("materialized context file size is invalid")
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), size, nil
}

type manifestFile struct {
	Relative string
	Raw      []byte
}

func digestManifestBundles(root string, serviceKeys []string) (map[string]string, error) {
	if len(serviceKeys) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(serviceKeys))
	for _, serviceKey := range serviceKeys {
		digest, ok, err := digestManifestBundle(filepath.Join(root, "deploy", "base", serviceKey))
		if err != nil {
			return nil, err
		}
		if ok {
			result[serviceKey] = digest
		}
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func digestManifestBundle(root string) (string, bool, error) {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil || !info.IsDir() {
		return "", false, errors.New("deploy manifest bundle cannot be read")
	}
	files := []manifestFile{}
	totalBytes := int64(0)
	if err := filepath.WalkDir(root, func(pathValue string, entry os.DirEntry, err error) error {
		if err != nil {
			return errors.New("deploy manifest bundle cannot be read")
		}
		if entry.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		if len(files) >= maxManifestFiles {
			return errors.New("deploy manifest bundle has too many files")
		}
		info, err := entry.Info()
		if err != nil {
			return errors.New("deploy manifest file cannot be read")
		}
		totalBytes += info.Size()
		if totalBytes > maxManifestBytes {
			return errors.New("deploy manifest bundle is too large")
		}
		raw, err := os.ReadFile(pathValue)
		if err != nil {
			return errors.New("deploy manifest file cannot be read")
		}
		relative, err := filepath.Rel(root, pathValue)
		if err != nil {
			return errors.New("deploy manifest file cannot be read")
		}
		files = append(files, manifestFile{Relative: filepath.ToSlash(relative), Raw: raw})
		return nil
	}); err != nil {
		return "", false, err
	}
	if len(files) == 0 {
		return "", false, nil
	}
	return digestManifestFiles(files), true, nil
}

func digestManifestFiles(files []manifestFile) string {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Relative < files[j].Relative
	})
	digest := sha256.New()
	for _, file := range files {
		writeManifestDigestEntry(digest, file)
	}
	return "sha256:" + hex.EncodeToString(digest.Sum(nil))
}

func writeManifestDigestEntry(writer io.Writer, file manifestFile) {
	fmt.Fprintf(writer, "%s\x00%d\x00", file.Relative, len(file.Raw))
	_, _ = writer.Write(file.Raw)
	_, _ = writer.Write([]byte("\n"))
}

func writeReport(pathValue string, result report) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return errors.New("encode materializer report failed")
	}
	if err := os.MkdirAll(filepath.Dir(pathValue), 0o750); err != nil {
		return errors.New("create report directory failed")
	}
	return os.WriteFile(pathValue, raw, 0o640)
}

func safeRelativePath(value string) (string, error) {
	cleaned := path.Clean(strings.TrimSpace(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") || strings.ContainsAny(cleaned, "\\\x00\r\n\t") {
		return "", errors.New("archive path is invalid")
	}
	return cleaned, nil
}

func safePathToken(value string, max int) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > max || strings.ContainsAny(trimmed, " \t\r\n\\{}") || strings.Contains(trimmed, "..") {
		return false
	}
	for _, char := range trimmed {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			continue
		}
		return false
	}
	return true
}

func safeRef(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= 256 && !strings.ContainsAny(trimmed, " \t\r\n\\{}")
}

func validCommit(value string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if len(trimmed) != 40 && len(trimmed) != 64 {
		return false
	}
	_, err := hex.DecodeString(trimmed)
	return err == nil
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.ToLower(strings.TrimSpace(err.Error()))
	if text == "" || strings.Contains(text, "token") || strings.Contains(text, "authorization") || strings.Contains(text, "secret") {
		return "runtime materializer failed"
	}
	return err.Error()
}
