package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	githubapi "github.com/google/go-github/v82/github"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

const (
	defaultAdoptionScanTreeEntries = 1000
	defaultAdoptionScanMarkerPaths = 64
)

func (a *Adapter) executeScanRepositoryForAdoption(ctx context.Context, client *githubapi.Client, command *providerclient.ScanRepositoryForAdoptionCommand) (providerclient.WriteResult, error) {
	if command == nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	owner, repoName, err := a.repositoryRefFromTarget(ctx, client, command.RepositoryTarget)
	if err != nil {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, err)
	}
	repository, _, err := client.Repositories.Get(ctx, owner, repoName)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	defaultBranch := strings.TrimSpace(repository.GetDefaultBranch())
	if defaultBranch == "" {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, nil)
	}
	requestedRef := strings.TrimSpace(command.Options.RequestedRef)
	scannedRef := requestedRef
	if scannedRef == "" {
		scannedRef = defaultBranch
	}
	if !allowedGitHubAdoptionScanRef(scannedRef, defaultBranch, command.Options.AllowedRefPrefixes) {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindUnsupported, 0, nil)
	}
	ref, _, err := client.Git.GetRef(ctx, owner, repoName, gitRefForScan(scannedRef))
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	headSHA := strings.TrimSpace(ref.GetObject().GetSHA())
	if headSHA == "" {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, nil)
	}
	commit, _, err := client.Git.GetCommit(ctx, owner, repoName, headSHA)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	treeSHA := strings.TrimSpace(commit.GetTree().GetSHA())
	if treeSHA == "" {
		return providerclient.WriteResult{}, providerError(providerclient.ErrorKindPermanent, 0, nil)
	}
	tree, _, err := client.Git.GetTree(ctx, owner, repoName, treeSHA, true)
	if err != nil {
		return providerclient.WriteResult{}, classifyGitHubError(err)
	}
	snapshot := githubAdoptionScanSnapshot(repository, command, requestedRef, scannedRef, headSHA, tree)
	return providerclient.WriteResult{
		ResultRef:              snapshot.RepositoryURL,
		ProviderObjectID:       snapshot.ProviderRepositoryID,
		ProviderVersion:        headSHA,
		Target:                 &snapshot.RepositoryTarget,
		RepositoryAdoptionScan: &snapshot,
		BaseBranch:             defaultBranch,
	}, nil
}

func githubAdoptionScanSnapshot(repository *githubapi.Repository, command *providerclient.ScanRepositoryForAdoptionCommand, requestedRef string, scannedRef string, headSHA string, tree *githubapi.Tree) providerclient.RepositoryAdoptionScan {
	fullName := strings.TrimSpace(repository.GetFullName())
	if fullName == "" {
		fullName = strings.TrimSpace(command.RepositoryTarget.RepositoryFullName)
	}
	providerRepositoryID := ""
	if repository.GetID() > 0 {
		providerRepositoryID = strconv.FormatInt(repository.GetID(), 10)
	}
	target := providerclient.Target{
		ProviderSlug:         enum.ProviderSlugGitHub,
		RepositoryFullName:   fullName,
		ProviderRepositoryID: providerRepositoryID,
		WebURL:               repository.GetHTMLURL(),
	}
	markers, fileCount, visibleFileCount, treeTruncated, warnings := githubAdoptionScanMarkers(tree, command.Options)
	status := enum.RepositoryAdoptionScanStatusCompleted
	if treeTruncated {
		status = enum.RepositoryAdoptionScanStatusLimited
	} else if !hasAdoptionMarkerKind(markers, enum.RepositoryAdoptionMarkerServiceDescriptor) {
		status = enum.RepositoryAdoptionScanStatusNeedsReview
	}
	snapshot := providerclient.RepositoryAdoptionScan{
		RepositoryTarget:     target,
		RepositoryFullName:   fullName,
		ProviderRepositoryID: providerRepositoryID,
		RepositoryURL:        repository.GetHTMLURL(),
		DefaultBranch:        strings.TrimSpace(repository.GetDefaultBranch()),
		RequestedRef:         requestedRef,
		ScannedRef:           scannedRef,
		HeadSHA:              headSHA,
		Status:               status,
		Markers:              markers,
		FileCount:            fileCount,
		VisibleFileCount:     visibleFileCount,
		TreeTruncated:        treeTruncated,
		Warnings:             warnings,
		ObservedAt:           time.Now().UTC(),
	}
	snapshot.SnapshotDigest = githubAdoptionScanDigest(snapshot)
	return snapshot
}

func githubAdoptionScanMarkers(tree *githubapi.Tree, options providerclient.RepositoryAdoptionScanOptions) ([]providerclient.RepositoryAdoptionScanMarker, int64, int64, bool, []string) {
	maxTreeEntries := options.MaxTreeEntries
	if maxTreeEntries <= 0 {
		maxTreeEntries = defaultAdoptionScanTreeEntries
	}
	maxMarkerPaths := options.MaxMarkerPaths
	if maxMarkerPaths <= 0 {
		maxMarkerPaths = defaultAdoptionScanMarkerPaths
	}
	var entries []*githubapi.TreeEntry
	if tree != nil {
		entries = append([]*githubapi.TreeEntry(nil), tree.Entries...)
	}
	sort.SliceStable(entries, func(left int, right int) bool {
		return strings.TrimSpace(entries[left].GetPath()) < strings.TrimSpace(entries[right].GetPath())
	})
	fileCount := int64(0)
	visibleFileCount := int64(0)
	hints := markerPathHints(options.MarkerPathHints)
	markers := make([]providerclient.RepositoryAdoptionScanMarker, 0)
	treeTruncated := tree != nil && tree.GetTruncated()
	treeTruncated = treeTruncated || len(entries) > maxTreeEntries
	markersTruncated := false
	for index, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.GetType()), "blob") {
			fileCount++
		}
		if index >= maxTreeEntries {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(entry.GetType()), "blob") {
			continue
		}
		visibleFileCount++
		path := strings.TrimSpace(entry.GetPath())
		kind, ok := adoptionMarkerKind(path, hints)
		if !ok {
			continue
		}
		if len(markers) >= maxMarkerPaths {
			treeTruncated = true
			markersTruncated = true
			continue
		}
		markers = append(markers, providerclient.RepositoryAdoptionScanMarker{
			Path:         path,
			Kind:         kind,
			ObjectDigest: strings.TrimSpace(entry.GetSHA()),
			SizeBytes:    int64(entry.GetSize()),
		})
	}
	warnings := adoptionScanWarnings(markers, treeTruncated, markersTruncated)
	return markers, fileCount, visibleFileCount, treeTruncated, warnings
}

func markerPathHints(values []string) map[string]enum.RepositoryAdoptionMarkerKind {
	hints := make(map[string]enum.RepositoryAdoptionMarkerKind, len(values))
	for _, value := range values {
		if path := strings.TrimSpace(value); path != "" {
			hints[path] = enum.RepositoryAdoptionMarkerOther
		}
	}
	return hints
}

func adoptionMarkerKind(path string, hints map[string]enum.RepositoryAdoptionMarkerKind) (enum.RepositoryAdoptionMarkerKind, bool) {
	if kind, ok := defaultAdoptionMarkerKind(path); ok {
		return kind, true
	}
	if kind, ok := hints[path]; ok {
		return kind, true
	}
	switch {
	case strings.HasPrefix(path, "docs/"):
		return enum.RepositoryAdoptionMarkerDocs, true
	case strings.HasPrefix(path, ".github/workflows/"):
		return enum.RepositoryAdoptionMarkerWorkflow, true
	case strings.HasPrefix(path, "deploy/"), strings.HasPrefix(path, "k8s/"), strings.HasPrefix(path, "charts/"):
		return enum.RepositoryAdoptionMarkerDeploy, true
	default:
		return "", false
	}
}

func defaultAdoptionMarkerKind(path string) (enum.RepositoryAdoptionMarkerKind, bool) {
	switch path {
	case "services.yaml":
		return enum.RepositoryAdoptionMarkerServiceDescriptor, true
	case ".gitmodules":
		return enum.RepositoryAdoptionMarkerGitmodules, true
	case "README", "README.md":
		return enum.RepositoryAdoptionMarkerReadme, true
	case "AGENTS.md":
		return enum.RepositoryAdoptionMarkerAgents, true
	case "go.mod":
		return enum.RepositoryAdoptionMarkerModule, true
	case "package.json", "pyproject.toml":
		return enum.RepositoryAdoptionMarkerPackage, true
	case "Dockerfile":
		return enum.RepositoryAdoptionMarkerDeploy, true
	default:
		return "", false
	}
}

func adoptionScanWarnings(markers []providerclient.RepositoryAdoptionScanMarker, truncated bool, markersTruncated bool) []string {
	warnings := make([]string, 0, 3)
	if truncated {
		warnings = append(warnings, "tree_truncated")
	}
	if markersTruncated {
		warnings = append(warnings, "marker_paths_truncated")
	}
	if len(markers) == 0 {
		warnings = append(warnings, "adoption_markers_missing")
	} else if !hasAdoptionMarkerKind(markers, enum.RepositoryAdoptionMarkerServiceDescriptor) {
		warnings = append(warnings, "services_descriptor_missing")
	}
	return warnings
}

func hasAdoptionMarkerKind(markers []providerclient.RepositoryAdoptionScanMarker, kind enum.RepositoryAdoptionMarkerKind) bool {
	for _, marker := range markers {
		if marker.Kind == kind {
			return true
		}
	}
	return false
}

func gitRefForScan(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "refs/") {
		return strings.TrimPrefix(ref, "refs/")
	}
	if strings.HasPrefix(ref, "heads/") || strings.HasPrefix(ref, "tags/") {
		return ref
	}
	return gitBranchRef(ref)
}

func allowedGitHubAdoptionScanRef(ref string, defaultBranch string, prefixes []string) bool {
	ref = strings.TrimSpace(ref)
	defaultBranch = strings.TrimSpace(defaultBranch)
	if ref == "" {
		return false
	}
	if ref == defaultBranch || ref == "heads/"+defaultBranch || ref == "refs/heads/"+defaultBranch {
		return true
	}
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" && strings.HasPrefix(ref, prefix) {
			return true
		}
		if prefix != "" && strings.HasPrefix("refs/heads/"+ref, prefix) {
			return true
		}
	}
	return false
}

func githubAdoptionScanDigest(snapshot providerclient.RepositoryAdoptionScan) string {
	payload, err := json.Marshal(struct {
		RepositoryFullName   string                                        `json:"repository_full_name"`
		ProviderRepositoryID string                                        `json:"provider_repository_id,omitempty"`
		DefaultBranch        string                                        `json:"default_branch"`
		RequestedRef         string                                        `json:"requested_ref,omitempty"`
		ScannedRef           string                                        `json:"scanned_ref"`
		HeadSHA              string                                        `json:"head_sha"`
		Status               enum.RepositoryAdoptionScanStatus             `json:"status"`
		Markers              []providerclient.RepositoryAdoptionScanMarker `json:"markers,omitempty"`
		FileCount            int64                                         `json:"file_count"`
		VisibleFileCount     int64                                         `json:"visible_file_count"`
		TreeTruncated        bool                                          `json:"tree_truncated,omitempty"`
		Warnings             []string                                      `json:"warnings,omitempty"`
	}{
		RepositoryFullName:   snapshot.RepositoryFullName,
		ProviderRepositoryID: snapshot.ProviderRepositoryID,
		DefaultBranch:        snapshot.DefaultBranch,
		RequestedRef:         snapshot.RequestedRef,
		ScannedRef:           snapshot.ScannedRef,
		HeadSHA:              snapshot.HeadSHA,
		Status:               snapshot.Status,
		Markers:              snapshot.Markers,
		FileCount:            snapshot.FileCount,
		VisibleFileCount:     snapshot.VisibleFileCount,
		TreeTruncated:        snapshot.TreeTruncated,
		Warnings:             snapshot.Warnings,
	})
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}
