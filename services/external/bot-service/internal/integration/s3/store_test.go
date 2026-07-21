package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
)

func TestStorePutImmutableUsesConditionalChecksumAndExactKey(t *testing.T) {
	t.Parallel()
	client := &fakeS3API{}
	store := &Store{client: client, bucket: "artifacts"}
	key := "projects/1/sessions/2/artifacts/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/versions/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	body := []byte("safe body")
	checksum := "78f8c60b744fc44c8b86f6935bb7f40611e2e8a33e21dca349d8409e1ed0462f"
	if err := store.PutImmutable(context.Background(), key, "text/plain", int64(len(body)), checksum, bytes.NewReader(body)); err != nil {
		t.Fatalf("PutImmutable() error = %v", err)
	}
	if client.put == nil || value(client.put.Key) != key || value(client.put.Bucket) != "artifacts" || value(client.put.IfNoneMatch) != "*" || value(client.put.ChecksumSHA256) == "" || client.put.Metadata["mattercodex-sha256"] != checksum {
		t.Fatalf("PutObjectInput = %#v", client.put)
	}
	read, _ := io.ReadAll(client.put.Body)
	if string(read) != string(body) {
		t.Fatalf("PutObject body = %q", read)
	}
	client.putError = &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "synthetic conflict"}
	if err := store.PutImmutable(context.Background(), key, "text/plain", int64(len(body)), checksum, bytes.NewReader(body)); !errors.Is(err, domainartifact.ErrConflict) {
		t.Fatalf("conditional conflict error = %v", err)
	}
	client.putError = &smithy.GenericAPIError{Code: "ConditionalRequestConflict", Message: "synthetic concurrent conflict"}
	if err := store.PutImmutable(context.Background(), key, "text/plain", int64(len(body)), checksum, bytes.NewReader(body)); !errors.Is(err, domainartifact.ErrConflict) {
		t.Fatalf("concurrent conditional conflict error = %v", err)
	}
	if err := store.PutImmutable(context.Background(), "../escape", "text/plain", 1, checksum, strings.NewReader("x")); err == nil {
		t.Fatal("unsafe object key unexpectedly accepted")
	}
	client.getError = &smithy.GenericAPIError{Code: "NoSuchKey", Message: "synthetic missing object"}
	if _, err := store.Open(context.Background(), key); !errors.Is(err, domainartifact.ErrNotFound) {
		t.Fatalf("missing object error = %v", err)
	}
}

type fakeS3API struct {
	put      *s3.PutObjectInput
	putError error
	getError error
}

func (client *fakeS3API) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	client.put = input
	return &s3.PutObjectOutput{}, client.putError
}

func (client *fakeS3API) GetObject(_ context.Context, _ *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if client.getError != nil {
		return nil, client.getError
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("body"))}, nil
}

func value(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}
