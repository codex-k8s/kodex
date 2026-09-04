package runner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryOperationLockReleaseRecoversAfterTemporaryFailure(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := retryOperationLockRelease(context.Background(), time.Nanosecond, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("temporary S3 failure")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("operation lock release = %v after %d attempts", err, attempts)
	}
}

func TestRetryOperationLockReleaseStopsOnContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	attempts := 0
	err := retryOperationLockRelease(ctx, time.Nanosecond, func(context.Context) error {
		attempts++
		return errors.New("persistent S3 failure")
	})
	if err == nil || err.Error() != "persistent S3 failure" || attempts != 1 {
		t.Fatalf("operation lock release = %v after %d attempts", err, attempts)
	}
}

func TestRetryOperationLockReleaseStopsWhenAttemptCancelsContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	releaseErr := errors.New("persistent S3 failure")
	attempts := 0
	err := retryOperationLockRelease(ctx, time.Nanosecond, func(context.Context) error {
		attempts++
		cancel()
		return releaseErr
	})
	if !errors.Is(err, releaseErr) || attempts != 1 {
		t.Fatalf("operation lock release = %v after %d attempts", err, attempts)
	}
}
