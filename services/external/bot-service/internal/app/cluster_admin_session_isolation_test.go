package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestIsolateClusterAdminSessionWithMissingTokenDeletesPodBeforeCommit(t *testing.T) {
	events := []string{}
	err := isolateClusterAdminSessionWithMissingToken(
		context.Background(),
		42,
		"cluster-admin-session",
		func(ctx context.Context, sessionID int64, isolate func(context.Context) error) error {
			events = append(events, "database-block-staged")
			if err := isolate(ctx); err != nil {
				return err
			}
			events = append(events, "database-block-committed")
			return nil
		},
		func(context.Context, string) error {
			events = append(events, "pod-deleted")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("isolateClusterAdminSessionWithMissingToken() error = %v", err)
	}
	want := []string{"database-block-staged", "pod-deleted", "database-block-committed"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestIsolateClusterAdminSessionWithMissingTokenFailsClosedWhenPodDeletionFails(t *testing.T) {
	deleteErr := errors.New("delete failed")
	committed := false
	err := isolateClusterAdminSessionWithMissingToken(
		context.Background(),
		42,
		"cluster-admin-session",
		func(ctx context.Context, sessionID int64, isolate func(context.Context) error) error {
			if err := isolate(ctx); err != nil {
				return err
			}
			committed = true
			return nil
		},
		func(context.Context, string) error {
			return deleteErr
		},
	)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("isolateClusterAdminSessionWithMissingToken() error = %v, want %v", err, deleteErr)
	}
	if committed {
		t.Fatal("database block committed after pod deletion failure")
	}
}

func TestIsolateClusterAdminSessionWithMissingTokenRejectsMissingSessionKey(t *testing.T) {
	err := isolateClusterAdminSessionWithMissingToken(
		context.Background(),
		42,
		" ",
		func(context.Context, int64, func(context.Context) error) error {
			t.Fatal("block must not be called")
			return nil
		},
		func(context.Context, string) error {
			t.Fatal("cleanup must not be called")
			return nil
		},
	)
	if err == nil {
		t.Fatal("isolateClusterAdminSessionWithMissingToken() error = nil")
	}
}
