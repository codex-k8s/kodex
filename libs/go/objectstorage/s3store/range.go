package s3store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// maximumRangeDuration bounds one response body read while preserving any earlier caller deadline.
// maximumRangeDuration ограничивает response body read с сохранением более раннего caller deadline.
const maximumRangeDuration = 5 * time.Second

// ReadRange reads one closed slice from an exact immutable version through the existing S3 client.
// Ignored/malformed ranges, drifted source pins, truncated/extra bytes, corrupt data and expired calls
// fail without returning bytes; one SDK attempt is used and the response body is always closed.
// ReadRange читает closed slice exact immutable version через existing S3 client.
// Ignored/malformed ranges, drift source pins, truncated/extra bytes, corruption и expiry
// закрываются без bytes; SDK делает одну attempt, response body закрывается на всех путях.
func (store *Store) ReadRange(ctx context.Context, input objectstorage.RangeRead) ([]byte, error) {
	if input.Validate() != nil || ctx == nil {
		return nil, objectstorage.ErrInvalid
	}
	if store == nil || store.client == nil || store.bucket == "" || ctx.Err() != nil {
		return nil, objectstorage.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, maximumRangeDuration)
	defer cancel()
	end := input.Offset + input.Length - 1
	output, err := store.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(input.Object.Key),
		VersionId: aws.String(input.Object.VersionID), IfMatch: aws.String("\"" + input.Object.ETag + "\""),
		Range: aws.String(fmt.Sprintf("bytes=%d-%d", input.Offset, end)),
	}, func(options *s3.Options) { options.RetryMaxAttempts = 1 })
	if err != nil {
		if output != nil && output.Body != nil {
			_ = output.Body.Close()
		}
		return nil, mapError(err)
	}
	if output == nil || output.Body == nil {
		return nil, objectstorage.ErrConflict
	}
	if aws.ToString(output.ContentRange) != fmt.Sprintf("bytes %d-%d/%d", input.Offset, end, input.Object.SizeBytes) ||
		output.ContentLength == nil || *output.ContentLength != input.Length ||
		aws.ToString(output.VersionId) != input.Object.VersionID || aws.ToString(output.ETag) != "\""+input.Object.ETag+"\"" ||
		output.Metadata[digestMetadataKey] != input.Object.Digest {
		_ = output.Body.Close()
		return nil, objectstorage.ErrConflict
	}
	payload, readErr := io.ReadAll(io.LimitReader(output.Body, input.Length+1))
	closeErr := output.Body.Close()
	sum := sha256.Sum256(payload)
	if readErr != nil || closeErr != nil || ctx.Err() != nil {
		clear(payload)
		return nil, objectstorage.ErrUnavailable
	}
	if int64(len(payload)) != input.Length || hex.EncodeToString(sum[:]) != strings.TrimPrefix(input.Digest, "sha256:") {
		clear(payload)
		return nil, objectstorage.ErrConflict
	}
	return payload, nil
}
