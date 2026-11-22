package s3store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// rangeBody records positive response cleanup while allowing read and close failure injection.
// rangeBody фиксирует response cleanup и позволяет проверить read/close failure.
type rangeBody struct {
	io.Reader
	closed   bool
	closeErr error
}

// Close records the actual owned body close before exposing a configured private failure.
// Close фиксирует actual owned body close до возврата настроенного private failure.
func (body *rangeBody) Close() error { body.closed = true; return body.closeErr }

// rangeAPI narrows the existing S3 fixture to one observed range GET with configurable reply.
// rangeAPI ограничивает existing S3 fixture одним observed range GET с configurable reply.
type rangeAPI struct {
	api
	reply   *s3.GetObjectOutput
	err     error
	calls   int
	input   *s3.GetObjectInput
	options s3.Options
}

// GetObject captures exact version/ETag/range and the no-retry option without returning raw errors.
// GetObject фиксирует exact version/ETag/range и no-retry option без вывода raw errors.
func (client *rangeAPI) GetObject(_ context.Context, input *s3.GetObjectInput, options ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	client.calls++
	client.input = input
	for _, option := range options {
		option(&client.options)
	}
	return client.reply, client.err
}

// rangeDigest computes a deterministic full or slice fixture commitment without logging content.
// rangeDigest вычисляет deterministic full либо slice fixture commitment без вывода content.
func rangeDigest(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// rangeFixture binds a ten-byte object and its three-byte middle slice to exact metadata.
// rangeFixture связывает ten-byte object и middle slice из3bytes с exact metadata.
func rangeFixture() (objectstorage.RangeRead, *s3.GetObjectOutput, *rangeBody) {
	input := objectstorage.RangeRead{Object: objectstorage.Receipt{Key: "organizations/org/source/private", VersionID: "version-1", ETag: "etag-1", Digest: rangeDigest("0123456789"), SizeBytes: 10}, Offset: 3, Length: 3, Digest: rangeDigest("345")}
	body := &rangeBody{Reader: strings.NewReader("345")}
	reply := &s3.GetObjectOutput{Body: body, ContentLength: aws.Int64(3), ContentRange: aws.String("bytes 3-5/10"), VersionId: aws.String("version-1"), ETag: aws.String("\"etag-1\""), Metadata: map[string]string{digestMetadataKey: input.Object.Digest}}
	return input, reply, body
}

// TestReadRangePinsExactSourceAndClosesBody proves one existing SDK request and owned validated bytes.
// TestReadRangePinsExactSourceAndClosesBody доказывает один existing SDK request и owned validated bytes.
func TestReadRangePinsExactSourceAndClosesBody(t *testing.T) {
	input, reply, body := rangeFixture()
	client := &rangeAPI{reply: reply}
	store := &Store{client: client, bucket: "fixture"}
	got, err := store.ReadRange(context.Background(), input)
	if err != nil || !bytes.Equal(got, []byte("345")) || !body.closed || client.calls != 1 {
		t.Fatal("range validation or cleanup failed", err)
	}
	clear(got)
	if aws.ToString(client.input.Range) != "bytes=3-5" || aws.ToString(client.input.VersionId) != input.Object.VersionID || aws.ToString(client.input.IfMatch) != "\"etag-1\"" || client.options.RetryMaxAttempts != 1 {
		t.Fatal("range request lost immutable pins or attempt bound")
	}
}

// TestReadRangeRejectsMalformedResponse closes all mismatch, short/extra/hash and cleanup paths.
// TestReadRangeRejectsMalformedResponse закрывает mismatch, short/extra/hash и cleanup paths.
func TestReadRangeRejectsMalformedResponse(t *testing.T) {
	cases := map[string]func(*s3.GetObjectOutput, *rangeBody){
		"ignored-range":  func(r *s3.GetObjectOutput, _ *rangeBody) { r.ContentRange = nil },
		"range-start":    func(r *s3.GetObjectOutput, _ *rangeBody) { r.ContentRange = aws.String("bytes 2-4/10") },
		"total":          func(r *s3.GetObjectOutput, _ *rangeBody) { r.ContentRange = aws.String("bytes 3-5/11") },
		"length":         func(r *s3.GetObjectOutput, _ *rangeBody) { r.ContentLength = aws.Int64(4) },
		"missing-length": func(r *s3.GetObjectOutput, _ *rangeBody) { r.ContentLength = nil },
		"version":        func(r *s3.GetObjectOutput, _ *rangeBody) { r.VersionId = aws.String("version-2") },
		"etag":           func(r *s3.GetObjectOutput, _ *rangeBody) { r.ETag = aws.String("\"etag-2\"") },
		"digest":         func(r *s3.GetObjectOutput, _ *rangeBody) { r.Metadata[digestMetadataKey] = rangeDigest("wrong") },
		"short":          func(_ *s3.GetObjectOutput, b *rangeBody) { b.Reader = strings.NewReader("34") },
		"extra":          func(_ *s3.GetObjectOutput, b *rangeBody) { b.Reader = strings.NewReader("3456") },
		"corrupt":        func(_ *s3.GetObjectOutput, b *rangeBody) { b.Reader = strings.NewReader("bad") },
		"close":          func(_ *s3.GetObjectOutput, b *rangeBody) { b.closeErr = errors.New("synthetic close failure") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input, reply, body := rangeFixture()
			mutate(reply, body)
			client := &rangeAPI{reply: reply}
			got, err := (&Store{client: client, bucket: "fixture"}).ReadRange(context.Background(), input)
			expected := objectstorage.ErrConflict
			if name == "close" {
				expected = objectstorage.ErrUnavailable
			}
			if !errors.Is(err, expected) || got != nil || !body.closed || client.calls != 1 {
				t.Fatal("invalid range returned bytes or skipped cleanup", err)
			}
		})
	}
}

// TestReadRangeRejectsInvalidOrExpiredRequests proves failure before any SDK request or response allocation.
// TestReadRangeRejectsInvalidOrExpiredRequests доказывает failure до SDK request либо response allocation.
func TestReadRangeRejectsInvalidOrExpiredRequests(t *testing.T) {
	input, reply, _ := rangeFixture()
	client := &rangeAPI{reply: reply}
	store := &Store{client: client, bucket: "fixture"}
	for _, mutate := range []func(*objectstorage.RangeRead){
		func(r *objectstorage.RangeRead) { r.Object.VersionID = "" },
		func(r *objectstorage.RangeRead) { r.Object.VersionID = "null" }, func(r *objectstorage.RangeRead) { r.Object.ETag = "bad\r\nvalue" },
		func(r *objectstorage.RangeRead) { r.Offset = -1 }, func(r *objectstorage.RangeRead) { r.Offset = 9223372036854775807 },
		func(r *objectstorage.RangeRead) { r.Length = 0 }, func(r *objectstorage.RangeRead) { r.Length = 32769 },
		func(r *objectstorage.RangeRead) { r.Digest = "bad" },
	} {
		invalid := input
		mutate(&invalid)
		if got, err := store.ReadRange(context.Background(), invalid); !errors.Is(err, objectstorage.ErrInvalid) || got != nil {
			t.Fatal("invalid range admitted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := store.ReadRange(ctx, input); !errors.Is(err, objectstorage.ErrUnavailable) || got != nil {
		t.Fatal("expired request admitted")
	}
	if client.calls != 0 {
		t.Fatal("invalid source reached SDK")
	}
}
