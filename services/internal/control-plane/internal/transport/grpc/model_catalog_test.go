package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"strings"
	"testing"
)

func TestModelCatalogCasterPinsEmptyAndNonemptyPages(t *testing.T) {
	for _, models := range [][]entity.ModelCapability{nil, {{ID: "model", Available: true}}} {
		v := entity.ModelCatalog{Models: models, Total: int64(len(models)), NextPageToken: "next", Revision: "mcat_" + strings.Repeat("a", 64), Digest: strings.Repeat("a", 64)}
		result := castModelCatalog(v)
		if result.CatalogRevision != v.Revision || result.CatalogDigest != v.Digest || result.Total != v.Total || result.Page.NextPageToken != "next" || len(result.Models) != len(models) {
			t.Fatal("catalog pin lost in transport")
		}
	}
}
