// Package publisher publishes a bounded, verified OCI archive to one HTTPS registry.
// RU: Package publisher публикует ограниченный проверенный OCI archive в один HTTPS registry.
package publisher

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/regclient/regclient"
	"github.com/regclient/regclient/config"
	"github.com/regclient/regclient/scheme/reg"
	"github.com/regclient/regclient/types/errs"
	"github.com/regclient/regclient/types/ref"
)

var (
	// ErrInvalid rejects malformed files, credentials, destinations or archive bindings before publication.
	// RU: ErrInvalid закрывает malformed файлы, credentials, destination и archive binding до публикации.
	ErrInvalid = errors.New("publisher input rejected")
	// ErrConflict prevents replacing an existing tag with a different immutable digest.
	// RU: ErrConflict запрещает замену существующего тега другим immutable digest.
	ErrConflict = errors.New("publisher destination digest conflicts")
	// ErrUnauthorized distinguishes rejected credentials without exposing the upstream response.
	// RU: ErrUnauthorized различает отклонённые credentials, не раскрывая ответ внешней системы.
	ErrUnauthorized = errors.New("publisher credential rejected")
	// ErrUnavailable preserves an uncertain result for later digest reconciliation, without retrying the whole import.
	// RU: ErrUnavailable сохраняет неопределённый результат для сверки digest без повтора всей публикации.
	ErrUnavailable = errors.New("publisher unavailable; reconcile destination digest")
	// digestPattern admits only the supported SHA-256 content identity.
	// RU: digestPattern допускает только поддержанную SHA-256 identity содержимого.
	digestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

// MaxArchiveBytes bounds local OCI input independently of transport chunk size.
// RU: MaxArchiveBytes ограничивает локальный OCI input независимо от размера сетевого chunk.
const MaxArchiveBytes int64 = 8 << 30

// Input selects one archive, explicit auth file, HTTPS destination and expected immutable digest.
// RU: Input выбирает один archive, явный auth file, HTTPS destination и ожидаемый immutable digest.
type Input struct {
	Archive, DockerConfigFile, Destination, ExpectedDigest, CAFile string
	ChunkBytes                                                     int64
}

// Result confirms the exact destination identity and whether a previous successful publish was reconciled.
// RU: Result подтверждает точную identity destination и повторное чтение ранее опубликованного результата.
type Result struct {
	Digest     string `json:"digest"`
	Reused     bool   `json:"reused"`
	Reconciled bool   `json:"reconciled,omitempty"`
}

// scopedTransport prevents authentication or redirects from moving credentials to another authority.
// RU: scopedTransport запрещает перенос credentials через authentication/redirect на другую authority.
type scopedTransport struct {
	base http.RoundTripper
	host string
}

// RoundTrip permits only the selected HTTPS authority and never reads ambient proxy configuration.
// RU: RoundTrip допускает только выбранную HTTPS authority и не читает ambient proxy configuration.
func (t scopedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.User != nil || request.URL.Host != t.host {
		return nil, ErrInvalid
	}
	return t.base.RoundTrip(request)
}

// openOwned reads a regular single-link file without following symlinks; secret inputs require owner-only modes.
// RU: openOwned читает regular single-link файл без symlinks; secret input требует owner-only modes.
func openOwned(path string, secret bool) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, ErrInvalid
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ErrInvalid
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !info.Mode().IsRegular() || !ok || owner.Nlink != 1 || owner.Uid != uint32(os.Geteuid()) || secret && (info.Mode().Perm() != 0o600 && info.Mode().Perm() != 0o400) {
		file.Close()
		return nil, ErrInvalid
	}
	return file, nil
}

// validateArchive verifies every blob's bytes and the unique OCI root descriptor before any network effect.
// RU: validateArchive проверяет bytes каждого blob и единственный OCI root descriptor до сетевых эффектов.
func validateArchive(ctx context.Context, file *os.File, expected string) error {
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > MaxArchiveBytes {
		return ErrInvalid
	}
	reader := tar.NewReader(file)
	seen := map[string]bool{}
	blobs := map[string]bool{}
	var root string
	layout := false
	count := 0
	var expanded int64
	for {
		if ctx.Err() != nil {
			return ErrUnavailable
		}
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return ErrInvalid
		}
		count++
		expanded += header.Size
		if expanded < 0 || expanded > MaxArchiveBytes {
			return ErrInvalid
		}
		if count > 10000 || header.Size < 0 || header.Size > MaxArchiveBytes || seen[header.Name] {
			return ErrInvalid
		}
		seen[header.Name] = true
		if header.Typeflag == tar.TypeDir {
			if header.Name != "blobs/" && header.Name != "blobs/sha256/" {
				return ErrInvalid
			}
			continue
		}
		if header.Typeflag != tar.TypeReg {
			return ErrInvalid
		}
		switch header.Name {
		case "oci-layout":
			if header.Size > 4096 {
				return ErrInvalid
			}
			raw, err := io.ReadAll(reader)
			if err != nil {
				return ErrInvalid
			}
			var value struct {
				Version string `json:"imageLayoutVersion"`
			}
			if json.Unmarshal(raw, &value) != nil || value.Version != "1.0.0" {
				return ErrInvalid
			}
			layout = true
		case "index.json":
			if header.Size > 1<<20 {
				return ErrInvalid
			}
			raw, err := io.ReadAll(reader)
			if err != nil {
				return ErrInvalid
			}
			var value struct {
				Version   int `json:"schemaVersion"`
				Manifests []struct {
					Digest string   `json:"digest"`
					URLs   []string `json:"urls"`
				} `json:"manifests"`
			}
			if json.Unmarshal(raw, &value) != nil || value.Version != 2 || len(value.Manifests) != 1 || len(value.Manifests[0].URLs) != 0 {
				return ErrInvalid
			}
			root = value.Manifests[0].Digest
		default:
			name := strings.TrimPrefix(header.Name, "blobs/sha256/")
			if header.Name == name || !digestPattern.MatchString("sha256:"+name) {
				return ErrInvalid
			}
			hash := sha256.New()
			size, err := io.Copy(hash, reader)
			if err != nil || size != header.Size || hex.EncodeToString(hash.Sum(nil)) != name {
				return ErrInvalid
			}
			blobs["sha256:"+name] = true
		}
	}
	if !layout || root != expected || !blobs[root] {
		return ErrInvalid
	}
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return ErrInvalid
	}
	return nil
}

// credentials loads only the selected inline Docker login; helper commands and implicit account fallback are rejected.
// RU: credentials загружает только выбранный inline Docker login; helper commands и неявный account fallback запрещены.
func credentials(path, host string) (string, string, error) {
	file, err := openOwned(path, true)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || len(raw) > 1<<20 {
		return "", "", ErrInvalid
	}
	var value struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
		Store   string            `json:"credsStore"`
		Helpers map[string]string `json:"credHelpers"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Store != "" || len(value.Helpers) != 0 {
		return "", "", ErrInvalid
	}
	login, ok := value.Auths[host]
	if !ok {
		return "", "", ErrInvalid
	}
	decoded, err := base64.StdEncoding.DecodeString(login.Auth)
	if err != nil || len(decoded) > 65536 {
		return "", "", ErrInvalid
	}
	user, password, ok := strings.Cut(string(decoded), ":")
	if !ok || user == "" || password == "" || strings.ContainsAny(string(decoded), "\r\n\x00") {
		return "", "", ErrInvalid
	}
	return user, password, nil
}

// classify returns closed public errors without upstream URLs, upload state or credential contents.
// RU: classify возвращает закрытые ошибки без upstream URL, upload state и credential contents.
func classify(err error) error {
	if errors.Is(err, errs.ErrHTTPUnauthorized) {
		return ErrUnauthorized
	}
	return ErrUnavailable
}

// Publish validates local inputs, reconciles the target, uploads bounded chunks once, and verifies the final manifest digest.
// RU: Publish валидирует inputs, сверяет destination, однократно публикует bounded chunks и проверяет итоговый manifest digest.
func Publish(ctx context.Context, input Input) (Result, error) {
	if !digestPattern.MatchString(input.ExpectedDigest) || input.ChunkBytes < 64<<10 || input.ChunkBytes > 16<<20 {
		return Result{}, ErrInvalid
	}
	target, err := ref.New(input.Destination)
	if err != nil || target.Scheme != "reg" || target.Registry == "" || target.Repository == "" || target.Tag == "" || target.Digest != "" {
		return Result{}, ErrInvalid
	}
	file, err := openOwned(input.Archive, false)
	if err != nil {
		return Result{}, err
	}
	defer file.Close()
	if err = validateArchive(ctx, file, input.ExpectedDigest); err != nil {
		return Result{}, err
	}
	user, password, err := credentials(input.DockerConfigFile, target.Registry)
	if err != nil {
		return Result{}, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		return Result{}, ErrInvalid
	}
	if input.CAFile != "" {
		ca, err := os.Open(input.CAFile)
		if err != nil {
			return Result{}, ErrInvalid
		}
		raw, err := io.ReadAll(io.LimitReader(ca, (1<<20)+1))
		ca.Close()
		if err != nil || len(raw) > 1<<20 || !pool.AppendCertsFromPEM(raw) {
			return Result{}, ErrInvalid
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: scopedTransport{base: transport, host: target.Registry}, Timeout: time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrInvalid }}
	client := regclient.New(regclient.WithConfigHost(config.Host{Name: target.Registry, TLS: config.TLSEnabled, User: user, Pass: password, BlobChunk: input.ChunkBytes, BlobMax: input.ChunkBytes, ReqConcurrent: 1}), regclient.WithRegOpts(reg.WithHTTPClient(httpClient)), regclient.WithRetryLimit(1), regclient.WithRetryDelay(100*time.Millisecond, time.Second), regclient.WithSlog(slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer client.Close(ctx, target)
	before, err := client.ManifestHead(ctx, target)
	if err == nil {
		if before.GetDescriptor().Digest.String() != input.ExpectedDigest {
			return Result{}, ErrConflict
		}
		return Result{Digest: input.ExpectedDigest, Reused: true}, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return Result{}, classify(err)
	}
	imported := client.ImageImport(ctx, target, file)
	after, readErr := client.ManifestHead(ctx, target)
	if readErr == nil && after.GetDescriptor().Digest.String() == input.ExpectedDigest {
		return Result{Digest: input.ExpectedDigest, Reconciled: imported != nil}, nil
	}
	if imported != nil {
		return Result{}, classify(imported)
	}
	if readErr != nil {
		return Result{}, classify(readErr)
	}
	return Result{}, ErrConflict
}
