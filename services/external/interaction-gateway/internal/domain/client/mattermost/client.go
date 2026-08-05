// Package mattermost задаёт порт проверенного Mattermost transport.
package mattermost

import (
	"context"
	"errors"

	"github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/types/entity"
)

var ErrAmbiguousEffect = errors.New("Mattermost effect outcome is ambiguous")

type RawEvent struct {
	ProviderEventID string
	Kind            string
	Revision        uint64
	Cursor          int64
	Verified        bool
	TeamID          string
	ChannelID       string
	PostID          string
	RootPostID      string
	UserID          string
	Text            string
	FileIDs         []string
	DeleteAt        int64
}

type Published struct {
	PostID        string
	ChannelID     string
	RootPostID    string
	ReceiptSHA256 string
}

type Client interface {
	Check(context.Context) error
	ResolveInbound(context.Context, RawEvent) (entity.Boundary, RawEvent, error)
	ResolveDelivery(string, string) (entity.Boundary, error)
	ResolveRoomDelivery(string, string, string) (entity.Boundary, error)
	DownloadFile(context.Context, string, string) ([]byte, string, string, error)
	Publish(context.Context, entity.Delivery, []string) (Published, error)
	OpenDecisionDialog(context.Context, string, string, string, string, string) error
	ReconcileLifecycle(context.Context, map[string]string, func(context.Context, RawEvent) error) error
	CatchUp(context.Context, map[string]int64, map[string]string, func(context.Context, RawEvent) error) error
	Listen(context.Context, func(context.Context, RawEvent) error) error
	ChannelBoundaries() []entity.Boundary
	ReadinessBoundary() (entity.Boundary, error)
	AuthenticateArtifactDownload(context.Context, string, entity.DownloadGrant) error
}
