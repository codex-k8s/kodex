package platform

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

type catalogCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	Ref     string `json:"r"`
}

func catalogScope(current scope, kind string, filter query.Filter) string {
	filter.Page = query.Page{}
	raw, _ := json.Marshal(struct {
		Organization, Actor, Kind, AuthorityProject string
		Filter                                      query.Filter
	}{current.organizationID, current.actorID, kind, current.authorityProjectID, filter})
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func decodeCatalogCursor(current scope, kind string, filter query.Filter) (string, error) {
	if filter.Page.Token == "" {
		return "", nil
	}
	if len(filter.Page.Token) > 512 {
		return "", errs.ErrInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(filter.Page.Token)
	if err != nil {
		return "", errs.ErrInvalid
	}
	var cursor catalogCursor
	if json.Unmarshal(raw, &cursor) != nil || cursor.Version != 1 || cursor.Scope != catalogScope(current, kind, filter) || cursor.Ref == "" || len(cursor.Ref) > 96 || strings.ContainsAny(cursor.Ref, "\x00\n\r") {
		return "", errs.ErrInvalid
	}
	return cursor.Ref, nil
}

func encodeCatalogCursor(current scope, kind string, filter query.Filter, ref string) string {
	raw, _ := json.Marshal(catalogCursor{Version: 1, Scope: catalogScope(current, kind, filter), Ref: ref})
	return base64.RawURLEncoding.EncodeToString(raw)
}
