// Package s3store implements objectstorage.Store over the AWS S3 API while
// preserving versioned receipts and validating digest metadata on reads.
// RU: Пакет s3store реализует objectstorage.Store поверх AWS S3 API,
// сохраняя версионированные квитанции и проверяя метаданные дайджеста при чтении.
package s3store

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// digestMetadataKey names the immutable object digest stored in S3 metadata.
// RU: digestMetadataKey задаёт имя дайджеста объекта в метаданных S3.
const digestMetadataKey = "kodex-sha256"

// Config contains the exact endpoint, bucket, region and static credentials
// used by one S3 adapter; validation forbids credentials embedded in the URL.
// RU: Config содержит точный узел, бакет, регион и учётные данные одного
// адаптера S3; проверка запрещает учётные данные внутри URL.
type Config struct {
	Endpoint, Region, Bucket string
	AccessKeyID, SecretKey   string
	UsePathStyle             bool
}

// api narrows the SDK surface so store behavior can be tested without S3 IO.
// RU: api ограничивает поверхность SDK для проверки поведения без обращения к S3.
type api interface {
	// HeadBucket checks whether the configured bucket is reachable.
	// RU: HeadBucket проверяет доступность настроенного бакета.
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	// PutObject issues one upload attempt for an immutable owner intent.
	// RU: PutObject выполняет одну попытку загрузки для неизменяемого намерения владельца.
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	// GetObject retrieves bytes and exact version metadata for validation.
	// RU: GetObject получает байты и метаданные точной версии для проверки.
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	// HeadObject reads metadata for the version-pinned upload receipt.
	// RU: HeadObject читает метаданные квитанции загрузки с закреплённой версией.
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	// DeleteObject removes one selected key or version as directed by its owner.
	// RU: DeleteObject удаляет выбранный ключ либо версию по команде владельца.
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// Store binds the narrowed S3 client to one bucket while implementing the
// objectstorage boundary with typed receipts and closed errors.
// RU: Store связывает ограниченный клиент S3 с одним бакетом и реализует
// границу objectstorage через типизированные квитанции и закрытые ошибки.
type Store struct {
	client api
	bucket string
}

// New validates adapter configuration and constructs a path-style-capable S3
// client; it does not perform a bucket readiness probe until Check.
// RU: New проверяет параметры адаптера и создаёт S3-клиент с поддержкой
// path-style; доступность бакета отдельно проверяется методом Check.
func New(ctx context.Context, config Config) (*Store, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretKey, "")),
	)
	if err != nil {
		return nil, objectstorage.ErrUnavailable
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(config.Endpoint)
		options.UsePathStyle = config.UsePathStyle
	})
	return &Store{client: client, bucket: config.Bucket}, nil
}

// validateConfig rejects missing credentials and endpoints with userinfo,
// query, fragment or unexpected path before passing the URL to the SDK.
// RU: validateConfig отклоняет отсутствие учётных данных и URL с userinfo,
// query, fragment или лишним путём до передачи адреса в SDK.
func validateConfig(config Config) error {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") ||
		config.Region == "" || config.Bucket == "" || config.AccessKeyID == "" || config.SecretKey == "" {
		return objectstorage.ErrInvalid
	}
	return nil
}

// Check probes the exact configured bucket and returns a closed unavailable
// error when the store or remote bucket cannot be used.
// RU: Check проверяет точный настроенный бакет и возвращает закрытый отказ
// недоступности при ошибке клиента либо удалённого бакета.
func (store *Store) Check(ctx context.Context) error {
	if store == nil || store.client == nil || store.bucket == "" {
		return objectstorage.ErrUnavailable
	}
	if _, err := store.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

// Put makes one non-retried S3 write for the owner's immutable intent, then
// reads back the exact version and compares its size and digest before receipt.
// An unknown write outcome never triggers a second SDK upload attempt.
// RU: Put выполняет одну попытку S3-записи, затем перечитывает точную версию
// и сверяет размер с дайджестом до выдачи квитанции; неизвестный исход не
// запускает повторную загрузку средствами SDK.
func (store *Store) Put(ctx context.Context, input objectstorage.PutInput) (objectstorage.Receipt, error) {
	if !validPut(input) {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	checksum, _ := hex.DecodeString(strings.TrimPrefix(input.Digest, "sha256:"))
	output, err := store.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Key), Body: input.Body,
		ContentLength: aws.Int64(input.SizeBytes), ContentType: aws.String(input.MediaType),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(checksum)),
		Metadata:       map[string]string{digestMetadataKey: input.Digest},
	}, func(options *s3.Options) {
		// Один immutable write не повторяется SDK при неизвестном исходе.
		// Readback и следующие owner attempts имеют отдельный lifecycle.
		options.RetryMaxAttempts = 1
	})
	if err != nil {
		return objectstorage.Receipt{}, objectstorage.ErrUnavailable
	}
	receipt, err := store.Head(ctx, input.Key, aws.ToString(output.VersionId))
	if err != nil {
		return objectstorage.Receipt{}, err
	}
	if receipt.SizeBytes != input.SizeBytes || receipt.Digest != input.Digest {
		return objectstorage.Receipt{}, objectstorage.ErrConflict
	}
	receipt.ETag = strings.Trim(aws.ToString(output.ETag), "\"")
	return receipt, nil
}

// validPut checks the object key, digest, media type, nonnegative size and
// body presence before a write can cross the S3 boundary.
// RU: validPut проверяет ключ, дайджест, тип содержимого, размер и наличие
// тела до перехода операции через границу S3.
func validPut(input objectstorage.PutInput) bool {
	return objectstorage.ValidKey(input.Key) && objectstorage.ValidDigest(input.Digest) &&
		input.MediaType != "" && input.SizeBytes >= 0 && input.Body != nil
}

// Get retrieves a selected key/version, enables checksum response data and
// refuses to expose a body whose digest or size metadata is invalid.
// RU: Get читает выбранный ключ и версию с контрольными данными ответа и
// не выдаёт тело при ошибочных метаданных дайджеста либо размера.
func (store *Store) Get(ctx context.Context, key, versionID string) (objectstorage.Object, error) {
	if !objectstorage.ValidKey(key) {
		return objectstorage.Object{}, objectstorage.ErrInvalid
	}
	input := &s3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	output, err := store.client.GetObject(ctx, input)
	if err != nil {
		return objectstorage.Object{}, mapError(err)
	}
	if output.Body == nil {
		return objectstorage.Object{}, objectstorage.ErrConflict
	}
	digest := output.Metadata[digestMetadataKey]
	if !objectstorage.ValidDigest(digest) || output.ContentLength == nil || *output.ContentLength < 0 {
		_ = output.Body.Close()
		return objectstorage.Object{}, objectstorage.ErrConflict
	}
	return objectstorage.Object{Receipt: objectstorage.Receipt{Key: key, VersionID: aws.ToString(output.VersionId), ETag: strings.Trim(aws.ToString(output.ETag), "\""), Digest: digest, SizeBytes: *output.ContentLength}, Body: output.Body}, nil
}

// Head reads one key/version and returns only a receipt with valid digest
// and length metadata; missing or malformed metadata is a conflict.
// RU: Head читает ключ и версию и возвращает квитанцию лишь при корректных
// метаданных дайджеста и длины; отсутствие или порча означает конфликт.
func (store *Store) Head(ctx context.Context, key, versionID string) (objectstorage.Receipt, error) {
	if !objectstorage.ValidKey(key) {
		return objectstorage.Receipt{}, objectstorage.ErrInvalid
	}
	input := &s3.HeadObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	output, err := store.client.HeadObject(ctx, input)
	if err != nil {
		return objectstorage.Receipt{}, mapError(err)
	}
	digest := output.Metadata[digestMetadataKey]
	if !objectstorage.ValidDigest(digest) || output.ContentLength == nil || *output.ContentLength < 0 {
		return objectstorage.Receipt{}, objectstorage.ErrConflict
	}
	return objectstorage.Receipt{Key: key, VersionID: aws.ToString(output.VersionId), ETag: strings.Trim(aws.ToString(output.ETag), "\""), Digest: digest, SizeBytes: *output.ContentLength}, nil
}

// Delete sends one deletion request for a valid key and optional exact
// version; ownership and authorization must already be established upstream.
// RU: Delete отправляет запрос удаления корректного ключа и необязательной
// точной версии; владелец и полномочия должны быть проверены вызывающим слоем.
func (store *Store) Delete(ctx context.Context, key, versionID string) error {
	if !objectstorage.ValidKey(key) {
		return objectstorage.ErrInvalid
	}
	input := &s3.DeleteObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	if _, err := store.client.DeleteObject(ctx, input); err != nil {
		return mapError(err)
	}
	return nil
}

// mapError converts recognized S3 missing-key and bucket failures into the
// closed objectstorage error vocabulary without leaking provider internals.
// RU: mapError переводит известные ошибки отсутствия ключа и бакета S3 в
// закрытый набор ошибок objectstorage без раскрытия деталей провайдера.
func mapError(err error) error {
	var notFound *types.NoSuchKey
	if errors.As(err, &notFound) {
		return objectstorage.ErrNotFound
	}
	var noBucket *types.NoSuchBucket
	if errors.As(err, &noBucket) {
		return objectstorage.ErrUnavailable
	}
	var responseError interface{ HTTPStatusCode() int }
	if errors.As(err, &responseError) && responseError.HTTPStatusCode() == 404 {
		return objectstorage.ErrNotFound
	}
	return objectstorage.ErrUnavailable
}
