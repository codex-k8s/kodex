package http

import (
	"context"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v5"

	controlplanev1 "github.com/codex-k8s/codex-k8s/proto/gen/go/codexk8s/controlplane/v1"
	"github.com/codex-k8s/codex-k8s/services/external/api-gateway/internal/transport/http/casters"
	"github.com/codex-k8s/codex-k8s/services/external/api-gateway/internal/transport/http/models"
)

const (
	staffListRealtimeDefaultPage     = 1
	staffListRealtimeDefaultPageSize = 20
)

type listRealtimeKind string

const (
	listRealtimeRuns               listRealtimeKind = "runs"
	listRealtimeRuntimeDeployTasks listRealtimeKind = "runtime_deploy_tasks"
)

type paginatedRealtimeSnapshot[T any] struct {
	Items      []T
	Pagination models.Pagination
}

type listRealtimeMessage[T any] struct {
	Type       models.ListRealtimeMessageType `json:"type"`
	Items      []T                            `json:"items,omitempty"`
	Pagination *models.Pagination             `json:"pagination,omitempty"`
	Message    *string                        `json:"message,omitempty"`
	SentAt     string                         `json:"sent_at"`
}

type paginatedRealtimeResponse[ProtoItem any] interface {
	GetItems() []ProtoItem
	GetPage() int32
	GetPageSize() int32
	GetTotalCount() int32
}

type runsListRealtimeSnapshot = paginatedRealtimeSnapshot[models.Run]
type runtimeDeployTasksRealtimeSnapshot = paginatedRealtimeSnapshot[models.RuntimeDeployTaskListItem]

// RunsRealtime opens an authenticated websocket stream for the paginated runs table.
func (h *staffHandler) RunsRealtime(c *echo.Context) error {
	return h.streamListRealtime(c, listRealtimeRuns)
}

// RuntimeDeployTasksRealtime opens an authenticated websocket stream for the paginated runtime deploy tasks table.
func (h *staffHandler) RuntimeDeployTasksRealtime(c *echo.Context) error {
	return h.streamListRealtime(c, listRealtimeRuntimeDeployTasks)
}

func (h *staffHandler) streamListRealtime(c *echo.Context, kind listRealtimeKind) error {
	switch kind {
	case listRealtimeRuns:
		return streamResolvedRealtime(
			h,
			c,
			resolveRunListPage(staffListRealtimeDefaultPage, staffListRealtimeDefaultPageSize),
			(*staffHandler).fetchRunsRealtimeSnapshot,
			func(snapshot runsListRealtimeSnapshot) any { return newListRealtimeSnapshotMessage(snapshot) },
			func(err error) any { return newListRealtimeErrorMessage[models.Run](err) },
		)
	case listRealtimeRuntimeDeployTasks:
		return streamResolvedRealtime(
			h,
			c,
			resolveRuntimeDeployListFilters(staffListRealtimeDefaultPage, staffListRealtimeDefaultPageSize),
			(*staffHandler).fetchRuntimeDeployTasksRealtimeSnapshot,
			func(snapshot runtimeDeployTasksRealtimeSnapshot) any { return newListRealtimeSnapshotMessage(snapshot) },
			func(err error) any { return newListRealtimeErrorMessage[models.RuntimeDeployTaskListItem](err) },
		)
	default:
		return nil
	}
}

func streamResolvedRealtime[Arg any, Snapshot any](
	h *staffHandler,
	c *echo.Context,
	resolve func(c *echo.Context) (Arg, error),
	fetch func(*staffHandler, context.Context, *controlplanev1.Principal, Arg) (Snapshot, error),
	buildSnapshotMessage func(snapshot Snapshot) any,
	buildErrorMessage func(err error) any,
) error {
	return withPrincipalAndResolved(c, resolve, func(principal *controlplanev1.Principal, arg Arg) error {
		return streamRealtimeSnapshots(
			c,
			func(ctx context.Context) (Snapshot, error) {
				return fetch(h, ctx, principal, arg)
			},
			buildSnapshotMessage,
			buildErrorMessage,
		)
	})
}

func streamRealtimeSnapshots[Snapshot any](
	c *echo.Context,
	fetch func(ctx context.Context) (Snapshot, error),
	buildSnapshotMessage func(snapshot Snapshot) any,
	buildErrorMessage func(err error) any,
) error {
	conn, err := staffRealtimeUpgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return nil
	}
	defer conn.Close()

	conn.SetReadLimit(64 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(realtimePongTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(realtimePongTimeout))
	})

	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			if _, _, readErr := conn.ReadMessage(); readErr != nil {
				return
			}
		}
	}()

	fetchCtx, cancelFetch := context.WithTimeout(c.Request().Context(), realtimeFetchTimeout)
	initialSnapshot, err := fetch(fetchCtx)
	cancelFetch()
	if err != nil {
		_ = writeRealtimeJSONMessage(conn, buildErrorMessage(err))
		sendRealtimeClose(conn)
		return nil
	}

	if err := writeRealtimeJSONMessage(conn, buildSnapshotMessage(initialSnapshot)); err != nil {
		return nil
	}

	fingerprint := marshalRealtimeFingerprint(initialSnapshot)

	updateTicker := time.NewTicker(realtimeUpdateInterval)
	defer updateTicker.Stop()
	pingTicker := time.NewTicker(realtimePingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-c.Request().Context().Done():
			sendRealtimeClose(conn)
			return nil
		case <-readerDone:
			return nil
		case <-pingTicker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout))
			if pingErr := conn.WriteMessage(websocket.PingMessage, nil); pingErr != nil {
				return nil
			}
		case <-updateTicker.C:
			nextFetchCtx, nextCancel := context.WithTimeout(c.Request().Context(), realtimeFetchTimeout)
			nextSnapshot, fetchErr := fetch(nextFetchCtx)
			nextCancel()
			if fetchErr != nil {
				_ = writeRealtimeJSONMessage(conn, buildErrorMessage(fetchErr))
				continue
			}

			nextFingerprint := marshalRealtimeFingerprint(nextSnapshot)
			if nextFingerprint == fingerprint {
				continue
			}
			if writeErr := writeRealtimeJSONMessage(conn, buildSnapshotMessage(nextSnapshot)); writeErr != nil {
				return nil
			}
			fingerprint = nextFingerprint
		}
	}
}

func (h *staffHandler) fetchRunsRealtimeSnapshot(ctx context.Context, principal *controlplanev1.Principal, arg runListPageArg) (runsListRealtimeSnapshot, error) {
	return fetchPaginatedRealtimeSnapshot(ctx, principal, arg, h.listRunsCall, casters.Runs)
}

func (h *staffHandler) fetchRuntimeDeployTasksRealtimeSnapshot(ctx context.Context, principal *controlplanev1.Principal, arg runtimeDeployListArg) (runtimeDeployTasksRealtimeSnapshot, error) {
	return fetchPaginatedRealtimeSnapshot(ctx, principal, arg, h.listRuntimeDeployTasksCall, casters.RuntimeDeployTaskListItems)
}

func newListRealtimeSnapshotMessage[T any](snapshot paginatedRealtimeSnapshot[T]) listRealtimeMessage[T] {
	return listRealtimeMessage[T]{
		Type:       models.ListRealtimeMessageTypeSnapshot,
		Items:      snapshot.Items,
		Pagination: &snapshot.Pagination,
		SentAt:     realtimeSentAt(),
	}
}

func newListRealtimeErrorMessage[T any](err error) listRealtimeMessage[T] {
	return listRealtimeMessage[T]{
		Type:    models.ListRealtimeMessageTypeError,
		Message: realtimeErrorMessagePtr(err),
		SentAt:  realtimeSentAt(),
	}
}

func realtimeErrorMessagePtr(err error) *string {
	text := realtimeErrorText(err)
	return &text
}

func realtimeSentAt() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newPaginatedRealtimeSnapshot[T any](items []T, page int32, pageSize int32, totalCount int32) paginatedRealtimeSnapshot[T] {
	return paginatedRealtimeSnapshot[T]{
		Items: items,
		Pagination: models.Pagination{
			Page:       int(page),
			PageSize:   int(pageSize),
			TotalCount: int(totalCount),
		},
	}
}

func fetchPaginatedRealtimeSnapshot[Arg any, ProtoItem any, Item any, Resp paginatedRealtimeResponse[ProtoItem]](
	ctx context.Context,
	principal *controlplanev1.Principal,
	arg Arg,
	call func(context.Context, *controlplanev1.Principal, Arg) (Resp, error),
	cast func([]ProtoItem) []Item,
) (paginatedRealtimeSnapshot[Item], error) {
	resp, err := call(ctx, principal, arg)
	if err != nil {
		return paginatedRealtimeSnapshot[Item]{}, err
	}
	return newPaginatedRealtimeSnapshot(cast(resp.GetItems()), resp.GetPage(), resp.GetPageSize(), resp.GetTotalCount()), nil
}

func realtimeErrorText(err error) string {
	text := ""
	if err != nil {
		text = err.Error()
	}
	if text == "" {
		return "realtime stream fetch failed"
	}
	return text
}
