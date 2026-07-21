package s3

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	domainartifact "github.com/codex-k8s/matter-codex/services/external/bot-service/internal/domain/artifact"
)

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
}

type apiClient interface {
	PutObject(ctx context.Context, input *awss3.PutObjectInput, options ...func(*awss3.Options)) (*awss3.PutObjectOutput, error)
	GetObject(ctx context.Context, input *awss3.GetObjectInput, options ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
}

type Store struct {
	client apiClient
	bucket string
}

var _ domainartifact.ObjectStore = (*Store)(nil)

func New(ctx context.Context, cfg Config) (*Store, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("artifact S3 endpoint is invalid")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "us-east-1"
	}
	if strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("artifact S3 bucket and credentials are required")
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load artifact S3 client configuration: %w", err)
	}
	client := awss3.NewFromConfig(loaded, func(options *awss3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = cfg.UsePathStyle
	})
	return &Store{client: client, bucket: strings.TrimSpace(cfg.Bucket)}, nil
}

func (store *Store) PutImmutable(ctx context.Context, key string, mediaType string, size int64, sha256 string, body io.Reader) error {
	if !validObjectKey(key) || size < 0 || size > domainartifact.DefaultMaxObjectBytes || body == nil {
		return fmt.Errorf("artifact object input is invalid")
	}
	checksum, err := hex.DecodeString(sha256)
	if err != nil || len(checksum) != 32 {
		return fmt.Errorf("artifact object checksum is invalid")
	}
	_, err = store.client.PutObject(ctx, &awss3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(key), Body: body,
		ContentLength: aws.Int64(size), ContentType: aws.String(mediaType),
		ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(checksum)),
		IfNoneMatch:    aws.String("*"),
		Metadata:       map[string]string{"mattercodex-sha256": sha256},
	})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "PreconditionFailed" || apiError.ErrorCode() == "ConditionalRequestConflict") {
			return domainartifact.ErrConflict
		}
		return fmt.Errorf("put immutable artifact object: %w", err)
	}
	return nil
}

func (store *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if !validObjectKey(key) {
		return nil, fmt.Errorf("artifact object key is invalid")
	}
	output, err := store.client.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(store.bucket), Key: aws.String(key)})
	if err != nil {
		var apiError smithy.APIError
		if errors.As(err, &apiError) && (apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound") {
			return nil, domainartifact.ErrNotFound
		}
		return nil, fmt.Errorf("get artifact object: %w", err)
	}
	if output == nil || output.Body == nil {
		return nil, fmt.Errorf("get artifact object: response body is missing")
	}
	return output.Body, nil
}

func validObjectKey(key string) bool {
	parts := strings.Split(strings.TrimSpace(key), "/")
	if len(parts) != 8 || parts[0] != "projects" || parts[2] != "sessions" || parts[4] != "artifacts" || parts[6] != "versions" {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\\x00\r\n") {
			return false
		}
	}
	return true
}
