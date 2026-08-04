// Package materialize создаёт workspace только из exact artifact readback.
package materialize

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
	"golang.org/x/sys/unix"
)

const maximumResponseHeaderBytes = 16 << 10

func Run(ctx context.Context, input model.Input) error {
	client, err := exactClient(input.InteractionGateway.TLS)
	if err != nil {
		return err
	}
	token, err := readCredential(input.CredentialFiles.MaterializationToken)
	if err != nil {
		return err
	}
	root, err := openRoot(input.WorkspaceRoot)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	for _, item := range input.Materializations {
		if err := materializeOne(ctx, client, token, root, input, item); err != nil {
			return err
		}
	}
	return nil
}

func exactClient(binding model.TLSBinding) (*http.Client, error) {
	caRaw, err := os.ReadFile(binding.CAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read materialization CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse materialization CA")
	}
	certificate, err := tls.LoadX509KeyPair(binding.CertificateFile, binding.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load materialization client identity")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13,
		ServerName: binding.ServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}},
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		ResponseHeaderTimeout: 10 * time.Second, TLSHandshakeTimeout: 5 * time.Second}
	return &http.Client{Transport: transport, Timeout: 2 * time.Minute}, nil
}

func materializeOne(ctx context.Context, client *http.Client, token string, root int,
	input model.Input, item model.Materialization) error {
	base, err := url.Parse(strings.TrimRight(input.InteractionGateway.URL, "/"))
	if err != nil {
		return errors.New("materialization endpoint is invalid")
	}
	base.Path = "/internal/v1/runtime-materializations/" + url.PathEscape(input.ExecutionID) + "/" + url.PathEscape(item.ArtifactID)
	query := base.Query()
	query.Set("version", strconv.FormatUint(item.ArtifactVersion, 10))
	query.Set("sha256", item.SHA256)
	base.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String(), nil)
	if err != nil {
		return errors.New("create materialization request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/octet-stream")
	response, err := client.Do(request)
	if err != nil {
		return errors.New("materialization readback is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.ContentLength != item.SizeBytes ||
		response.Header.Get("X-MatterCodex-Artifact-SHA256") != item.SHA256 ||
		response.Header.Get("X-MatterCodex-Artifact-Version") != strconv.FormatUint(item.ArtifactVersion, 10) ||
		headerBytes(response.Header) > maximumResponseHeaderBytes {
		return errors.New("materialization readback lineage is invalid")
	}
	parent, baseName, err := openParent(root, item.RelativePath)
	if err != nil {
		return err
	}
	defer unix.Close(parent)
	if exactExisting(parent, baseName, item) {
		hash := sha256.New()
		written, copyErr := io.Copy(hash, io.LimitReader(response.Body, item.SizeBytes+1))
		if copyErr != nil || written != item.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
			return errors.New("materialization readback content mismatch")
		}
		return nil
	}
	temporary := "." + baseName + ".mattercodex-" + item.SHA256[:16]
	if err := removeSafeStaging(parent, temporary, item.SizeBytes); err != nil {
		return err
	}
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errors.New("create materialization staging file")
	}
	file := os.NewFile(uintptr(fd), temporary)
	if file == nil {
		_ = unix.Close(fd)
		return errors.New("wrap materialization staging file")
	}
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = unix.Unlinkat(parent, temporary, 0)
		}
	}()
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, item.SizeBytes+1))
	if copyErr != nil || written != item.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != item.SHA256 {
		return errors.New("materialization content digest mismatch")
	}
	if err := file.Sync(); err != nil {
		return errors.New("sync materialization staging file")
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size != item.SizeBytes {
		return errors.New("materialization staging file is unsafe")
	}
	if err := unix.Renameat(parent, temporary, parent, baseName); err != nil {
		return errors.New("commit materialization file")
	}
	committed = true
	if err := unix.Fsync(parent); err != nil {
		return errors.New("sync materialization directory")
	}
	return nil
}

func removeSafeStaging(parent int, name string, maximumSize int64) error {
	var stat unix.Stat_t
	err := unix.Fstatat(parent, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, syscall.ENOENT) {
		return nil
	}
	if err != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size < 0 ||
		stat.Size > maximumSize {
		return errors.New("materialization staging file is unsafe")
	}
	if err := unix.Unlinkat(parent, name, 0); err != nil {
		return errors.New("remove stale materialization staging file")
	}
	return nil
}

func openRoot(path string) (int, error) {
	if path != "/workspace" || filepath.Clean(path) != path {
		return -1, errors.New("workspace root is invalid")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, errors.New("open workspace root")
	}
	return fd, nil
}

func openParent(root int, relative string) (int, string, error) {
	clean := filepath.Clean(relative)
	if clean != relative || filepath.IsAbs(clean) || clean == "." || clean == ".." ||
		strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return -1, "", errors.New("materialization path is invalid")
	}
	parts := strings.Split(clean, string(os.PathSeparator))
	current, err := unix.Dup(root)
	if err != nil {
		return -1, "", errors.New("duplicate workspace descriptor")
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			unix.Close(current)
			return -1, "", errors.New("materialization directory is invalid")
		}
		if err := unix.Mkdirat(current, part, 0o700); err != nil && !errors.Is(err, syscall.EEXIST) {
			unix.Close(current)
			return -1, "", errors.New("create materialization directory")
		}
		next, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(current)
		if err != nil {
			return -1, "", errors.New("open materialization directory")
		}
		var stat unix.Stat_t
		if unix.Fstat(next, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Nlink < 1 || stat.Mode&0o022 != 0 {
			unix.Close(next)
			return -1, "", errors.New("materialization directory is unsafe")
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}

func exactExisting(parent int, name string, item model.Materialization) bool {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return false
	}
	defer file.Close()
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Nlink != 1 || stat.Size != item.SizeBytes {
		return false
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, item.SizeBytes+1))
	return err == nil && written == item.SizeBytes && hex.EncodeToString(hash.Sum(nil)) == item.SHA256
}

func readCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 16<<10 {
		return "", errors.New("read materialization credential")
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || len(value) > 16<<10 {
		return "", errors.New("materialization credential is invalid")
	}
	return value, nil
}

func headerBytes(header http.Header) int {
	total := 0
	for key, values := range header {
		total += len(key)
		for _, value := range values {
			total += len(value)
		}
	}
	return total
}

func ReceiptDigest(input model.Input) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", input.ExecutionID, input.ExecutionVersion, input.RuntimeRevisionSHA256)))
	return hex.EncodeToString(digest[:])
}
