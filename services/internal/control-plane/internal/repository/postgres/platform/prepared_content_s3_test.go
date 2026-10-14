package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/s3store"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// Real SDK HTTP signing доказывает то, что in-memory Store не проверяет:
// bounded reader обязан сохранить возможность вычисления signed payload hash.
func TestPreparedContentSignedS3Reader(t *testing.T) {
	const body = "new immutable note"
	sum := sha256.Sum256([]byte(body))
	digest := fmt.Sprintf("sha256:%x", sum[:])
	var puts, heads atomic.Int64
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts.Add(1)
			got, err := io.ReadAll(io.LimitReader(r.Body, 1024))
			if err != nil || string(got) != body || r.Header.Get("X-Amz-Content-Sha256") != fmt.Sprintf("%x", sum[:]) {
				t.Error("signed exact body was not preserved")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.Header().Set("ETag", `"exact-etag"`)
			w.Header().Set("x-amz-version-id", "exact-version")
			return
		}
		if r.Method == http.MethodHead {
			heads.Add(1)
			if r.URL.Query().Get("versionId") != "exact-version" {
				t.Error("head did not pin put version")
			}
			w.Header().Set("ETag", `"exact-etag"`)
			w.Header().Set("x-amz-version-id", "exact-version")
			w.Header().Set("x-amz-meta-kodex-sha256", digest)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer endpoint.Close()
	store, err := s3store.New(t.Context(), s3store.Config{Endpoint: endpoint.URL, Region: "us-east-1", Bucket: "synthetic", AccessKeyID: "synthetic-key", SecretKey: "synthetic-secret", UsePathStyle: true})
	if err != nil {
		t.Fatal("synthetic S3 setup failed")
	}
	for _, name := range []string{"old-seekable-control", "prepared-bounded"} {
		t.Run(name, func(t *testing.T) {
			puts.Store(0)
			heads.Store(0)
			var reader io.Reader = bytes.NewReader([]byte(body))
			if name == "prepared-bounded" {
				reader, err = preparedContentSeekableBody(preparedContentRequest{Body: io.LimitReader(reader, int64(len(body))+1), Digest: digest, SizeBytes: int64(len(body))})
				if err != nil {
					t.Fatal("valid bounded body rejected")
				}
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			receipt, err := store.Put(ctx, objectstorage.PutInput{Key: "synthetic/exact-note", MediaType: "text/plain", Digest: digest, SizeBytes: int64(len(body)), Body: reader})
			if err != nil || puts.Load() != 1 || heads.Load() != 1 || !exactPreparedReceipt(receipt, "synthetic/exact-note", digest, int64(len(body))) {
				t.Fatalf("prepared signed S3 write failed: success=%t put_count=%d head_count=%d exact_receipt=%t", err == nil, puts.Load(), heads.Load(), exactPreparedReceipt(receipt, "synthetic/exact-note", digest, int64(len(body))))
			}
			if head, err := store.Head(t.Context(), receipt.Key, receipt.VersionID); err != nil || head != receipt {
				t.Fatal("second exact head did not match put receipt")
			}
		})
	}
}

type preparedBodyReadFailure struct{}

func (preparedBodyReadFailure) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE_READER_ERROR")
}

func TestPreparedContentSeekableBodyRejectsInvalidEnvelope(t *testing.T) {
	sum := sha256.Sum256([]byte("abc"))
	digest := fmt.Sprintf("sha256:%x", sum[:])
	for _, test := range []struct {
		name   string
		body   io.Reader
		size   int64
		digest string
		want   error
	}{
		{"short", strings.NewReader("ab"), 3, digest, errs.ErrInvalid},
		{"long", strings.NewReader("abcd"), 3, digest, errs.ErrInvalid},
		{"digest", strings.NewReader("xyz"), 3, digest, errs.ErrInvalid},
		{"negative", strings.NewReader("abc"), -1, digest, errs.ErrInvalid},
		{"over-limit", strings.NewReader("abc"), 1<<20 + 1, digest, errs.ErrInvalid},
		{"nil", nil, 3, digest, errs.ErrInvalid},
		{"read-error", preparedBodyReadFailure{}, 3, digest, errs.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := preparedContentSeekableBody(preparedContentRequest{Body: test.body, SizeBytes: test.size, Digest: test.digest})
			if !errors.Is(err, test.want) || body != nil {
				t.Fatal("invalid body envelope accepted or reader error leaked")
			}
		})
	}
}

func TestPreparedContentSeekableBodyBoundedAndReplayable(t *testing.T) {
	for _, size := range []int{0, 1 << 20} {
		body := strings.Repeat("a", size)
		digest := sha256.Sum256([]byte(body))
		reader, err := preparedContentSeekableBody(preparedContentRequest{Body: strings.NewReader(body), SizeBytes: int64(size), Digest: fmt.Sprintf("sha256:%x", digest[:])})
		if err != nil {
			t.Fatal("boundary body rejected")
		}
		for range 2 {
			if _, err := reader.Seek(0, io.SeekStart); err != nil {
				t.Fatal("verified body is not seekable")
			}
			got, err := io.ReadAll(reader)
			if err != nil || string(got) != body {
				t.Fatal("verified body changed on SDK rewind")
			}
		}
	}
}
