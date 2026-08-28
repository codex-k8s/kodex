package s3store

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

type fakeAPI struct {
	putInput  *s3.PutObjectInput
	headInput *s3.HeadObjectInput
	getInput  *s3.GetObjectInput
	deleted   *s3.DeleteObjectInput
	head      s3.HeadObjectOutput
	get       s3.GetObjectOutput
}

func (*fakeAPI) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

func (api *fakeAPI) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	api.putInput = input
	return &s3.PutObjectOutput{ETag: aws.String("\"etag\""), VersionId: aws.String("version-1")}, nil
}

func (api *fakeAPI) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	api.getInput = input
	return &api.get, nil
}

func (api *fakeAPI) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	api.headInput = input
	return &api.head, nil
}

func (api *fakeAPI) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	api.deleted = input
	return &s3.DeleteObjectOutput{}, nil
}

func TestPutPerformsMetadataReadback(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("a", 64)
	api := &fakeAPI{head: s3.HeadObjectOutput{
		ContentLength: aws.Int64(3), VersionId: aws.String("version-1"),
		Metadata: map[string]string{digestMetadataKey: digest},
	}}
	store := &Store{client: api, bucket: "kodex"}

	receipt, err := store.Put(context.Background(), objectstorage.PutInput{
		Key: "organizations/org/artifacts/art/1", MediaType: "text/plain",
		Digest: digest, SizeBytes: 3, Body: bytes.NewReader([]byte("abc")),
	})
	if err != nil {
		t.Fatalf("put object: %v", err)
	}
	if receipt.VersionID != "version-1" || receipt.ETag != "etag" || receipt.Digest != digest {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
	if aws.ToString(api.putInput.Key) != receipt.Key || aws.ToString(api.putInput.ChecksumSHA256) == "" {
		t.Fatalf("put input is incomplete: %#v", api.putInput)
	}
	if aws.ToString(api.headInput.VersionId) != "version-1" {
		t.Fatalf("head version = %q", aws.ToString(api.headInput.VersionId))
	}
}

func TestPutRejectsReadbackMismatch(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("b", 64)
	api := &fakeAPI{head: s3.HeadObjectOutput{
		ContentLength: aws.Int64(4), Metadata: map[string]string{digestMetadataKey: digest},
	}}
	store := &Store{client: api, bucket: "kodex"}

	_, err := store.Put(context.Background(), objectstorage.PutInput{
		Key: "organizations/org/artifacts/art/1", MediaType: "text/plain",
		Digest: digest, SizeBytes: 3, Body: bytes.NewReader([]byte("abc")),
	})
	if err != objectstorage.ErrConflict {
		t.Fatalf("error = %v, want conflict", err)
	}
}

func TestGetPreservesExactVersionAndMetadata(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("c", 64)
	api := &fakeAPI{get: s3.GetObjectOutput{
		Body: io.NopCloser(strings.NewReader("body")), ContentLength: aws.Int64(4),
		VersionId: aws.String("version-2"), ETag: aws.String("\"etag-2\""),
		Metadata: map[string]string{digestMetadataKey: digest},
	}}
	store := &Store{client: api, bucket: "kodex"}

	object, err := store.Get(context.Background(), "sessions/session/archive.jsonl", "version-2")
	if err != nil {
		t.Fatalf("get object: %v", err)
	}
	defer object.Body.Close()
	if object.VersionID != "version-2" || object.Digest != digest || object.SizeBytes != 4 {
		t.Fatalf("unexpected object: %#v", object.Receipt)
	}
	if aws.ToString(api.getInput.VersionId) != "version-2" {
		t.Fatalf("get version = %q", aws.ToString(api.getInput.VersionId))
	}
}

func TestConfigRejectsEndpointWithEmbeddedCredentials(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), Config{
		Endpoint: "https://user:secret@s3.example.test", Region: "ru-1", Bucket: "kodex",
		AccessKeyID: "key", SecretKey: "secret", UsePathStyle: true,
	})
	if err != objectstorage.ErrInvalid {
		t.Fatalf("error = %v, want invalid", err)
	}
}
