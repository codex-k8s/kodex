package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func testOwnerGateList(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef string) {
	t.Helper()
	filter := query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 1}}
	first, total, next, err := service.ListOwnerGates(ctx, owner, filter)
	if err != nil || total != 3 || len(first) != 1 || next == "" {
		t.Fatalf("gate first page and total: total=%d items=%d err=%v", total, len(first), err)
	}
	seen := map[string]bool{first[0].Ref: true}
	for next != "" {
		filter.Page.Token = next
		items, count, token, err := service.ListOwnerGates(ctx, owner, filter)
		if err != nil || count != 3 || len(items) != 1 || seen[items[0].Ref] {
			t.Fatalf("gate continuation: total=%d items=%d err=%v", count, len(items), err)
		}
		seen[items[0].Ref] = true
		next = token
	}
	if len(seen) != 3 {
		t.Fatalf("gate continuation lost items: %d", len(seen))
	}
	filter.State = "APPROVED"
	if _, _, _, err := service.ListOwnerGates(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("gate cursor changed state: %v", err)
	}
	filter.Page.Token = ""
	_, total, _, err = service.ListOwnerGates(ctx, owner, filter)
	if err != nil || total != 2 {
		t.Fatalf("gate exact state count: total=%d err=%v", total, err)
	}
	filter.State, filter.Query = "", strings.ToUpper(first[0].Title)
	items, total, _, err := service.ListOwnerGates(ctx, owner, filter)
	if err != nil || total == 0 || len(items) != 1 {
		t.Fatalf("gate case insensitive query: total=%d err=%v", total, err)
	}
	filter.Query = "%"
	items, total, next, err = service.ListOwnerGates(ctx, owner, filter)
	if err != nil || total != 0 || len(items) != 0 || next != "" {
		t.Fatalf("gate literal wildcard and empty total: total=%d err=%v", total, err)
	}
	filter.State = "UNKNOWN"
	if _, _, _, err := service.ListOwnerGates(ctx, owner, filter); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("gate unknown state: %v", err)
	}
}
