// Package s3store реализует objectstorage.Store поверх AWS S3 API.
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

const digestMetadataKey = "kodex-sha256"

type Config struct {
	Endpoint, Region, Bucket string
	AccessKeyID, SecretKey   string
	UsePathStyle             bool
}

type api interface {
	HeadBucket(context.Context, *s3.HeadBucketInput, ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type Store struct {
	client api
	bucket string
}

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

func validateConfig(config Config) error {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.Host == "" ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") ||
		config.Region == "" || config.Bucket == "" || config.AccessKeyID == "" || config.SecretKey == "" {
		return objectstorage.ErrInvalid
	}
	return nil
}

func (store *Store) Check(ctx context.Context) error {
	if store == nil || store.client == nil || store.bucket == "" {
		return objectstorage.ErrUnavailable
	}
	if _, err := store.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		return objectstorage.ErrUnavailable
	}
	return nil
}

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

func validPut(input objectstorage.PutInput) bool {
	return objectstorage.ValidKey(input.Key) && objectstorage.ValidDigest(input.Digest) &&
		input.MediaType != "" && input.SizeBytes >= 0 && input.Body != nil
}

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
