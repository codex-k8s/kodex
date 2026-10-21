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

// fakeAPI records S3 requests and returns controlled metadata so adapter
// tests can inspect exact keys, versions and upload readback without network IO.
// RU: fakeAPI запоминает запросы S3 и возвращает заданные метаданные для
// проверки ключей, версий и квитанций без обращения к сети.
type fakeAPI struct {
	putInput  *s3.PutObjectInput
	headInput *s3.HeadObjectInput
	getInput  *s3.GetObjectInput
	deleted   *s3.DeleteObjectInput
	head      s3.HeadObjectOutput
	get       s3.GetObjectOutput
}

// HeadBucket reports a reachable synthetic bucket to readiness tests.
// RU: HeadBucket сообщает тестам готовности о доступном синтетическом бакете.
func (*fakeAPI) HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error) {
	return &s3.HeadBucketOutput{}, nil
}

// PutObject records the exact upload request and returns a pinned test version.
// RU: PutObject запоминает точный запрос загрузки и возвращает тестовую версию.
func (api *fakeAPI) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	api.putInput = input
	return &s3.PutObjectOutput{ETag: aws.String("\"etag\""), VersionId: aws.String("version-1")}, nil
}

// GetObject records the requested version and supplies controlled object data.
// RU: GetObject запоминает запрошенную версию и выдаёт подготовленные данные объекта.
func (api *fakeAPI) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	api.getInput = input
	return &api.get, nil
}

// HeadObject records the readback version and supplies controlled metadata.
// RU: HeadObject запоминает версию обратной проверки и выдаёт заданные метаданные.
func (api *fakeAPI) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	api.headInput = input
	return &api.head, nil
}

// DeleteObject records the targeted key/version without mutating remote state.
// RU: DeleteObject запоминает выбранный ключ и версию без изменения удалённого состояния.
func (api *fakeAPI) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	api.deleted = input
	return &s3.DeleteObjectOutput{}, nil
}

// TestPutPerformsMetadataReadback requires a version-pinned Head response
// matching the uploaded digest and size before a receipt is returned.
// RU: TestPutPerformsMetadataReadback требует сверки точной версии, дайджеста
// и размера обратным Head-запросом до выдачи квитанции.
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

// TestPutRejectsReadbackMismatch rejects a successful upload whose stored
// size differs from the caller's immutable input.
// RU: TestPutRejectsReadbackMismatch отклоняет успешную загрузку, если
// сохранённый размер не совпал с неизменяемым входом вызывающего.
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

// TestGetPreservesExactVersionAndMetadata checks that version selection and
// digest metadata survive a read without switching to a newer object.
// RU: TestGetPreservesExactVersionAndMetadata проверяет сохранение выбранной
// версии и дайджеста при чтении без перехода к более новой версии.
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

// TestConfigRejectsEndpointWithEmbeddedCredentials ensures URL userinfo
// cannot smuggle a second credential source into the S3 adapter.
// RU: TestConfigRejectsEndpointWithEmbeddedCredentials запрещает userinfo
// внутри URL как второй скрытый источник учётных данных для адаптера S3.
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
