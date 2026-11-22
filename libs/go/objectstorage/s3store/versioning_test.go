package s3store

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/codex-k8s/kodex/libs/go/objectstorage"
)

// versioningFixture reports only the exact bucket's read-only version configuration.
// versioningFixture сообщает только read-only version configuration exact bucket.
type versioningFixture struct {
	fakeAPI
	response *s3.GetBucketVersioningOutput
	err      error
	bucket   string
	reads    int
}

// GetBucketVersioning records the read without enabling versioning or writing a probe object.
// GetBucketVersioning учитывает read без включения versioning либо записи probe object.
func (api *versioningFixture) GetBucketVersioning(_ context.Context, input *s3.GetBucketVersioningInput, _ ...func(*s3.Options)) (*s3.GetBucketVersioningOutput, error) {
	api.reads++
	api.bucket = aws.ToString(input.Bucket)
	return api.response, api.err
}

// TestCaptureRequiresEnabledVersioning separates ordinary reachability from durable capture.
// TestCaptureRequiresEnabledVersioning отделяет ordinary reachability от durable capture.
func TestCaptureRequiresEnabledVersioning(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      types.BucketVersioningStatus
		nilResponse bool
		err         error
		ready       bool
	}{
		{name: "enabled", status: types.BucketVersioningStatusEnabled, ready: true},
		{name: "suspended", status: types.BucketVersioningStatusSuspended},
		{name: "never-enabled"}, {name: "unknown", status: "future"}, {name: "absent-response", nilResponse: true},
		{name: "configuration-read-denied", err: errors.New("synthetic denial")},
	} {
		t.Run(test.name, func(t *testing.T) {
			api := &versioningFixture{response: &s3.GetBucketVersioningOutput{Status: test.status}, err: test.err}
			if test.nilResponse {
				api.response = nil
			}
			store := &Store{client: api, bucket: "exact-capture-bucket"}
			if err := store.Check(t.Context()); err != nil {
				t.Fatal("ordinary reachability changed", err)
			}
			err := store.CheckVersioning(t.Context())
			if (err == nil) != test.ready || err != nil && !errors.Is(err, objectstorage.ErrUnavailable) || api.reads != 1 || api.bucket != "exact-capture-bucket" || api.putInput != nil || api.deleted != nil {
				t.Fatal("versioning proof was fabricated or mutated", err)
			}
		})
	}
	if (&Store{client: &fakeAPI{}, bucket: "ordinary"}).CheckVersioning(t.Context()) == nil {
		t.Fatal("missing configuration port treated as versioned")
	}
}
