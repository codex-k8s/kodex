package grpc

import (
	"encoding/json"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func castRuntimeFileCatalog(value any) (*cp.RuntimeFileCatalog, bool) {
	if value == nil {
		return nil, true
	}
	raw, err := json.Marshal(value)
	var catalog runtimecontract.RuntimeFileCatalog
	if err != nil || json.Unmarshal(raw, &catalog) != nil || catalog.Validate() != nil {
		return nil, false
	}
	return runtimeFileCatalogProto(catalog), true
}

func runtimeFileCatalogProto(catalog runtimecontract.RuntimeFileCatalog) *cp.RuntimeFileCatalog {
	result := &cp.RuntimeFileCatalog{Ref: catalog.Ref, Digest: catalog.Digest, Total: catalog.Total}
	for _, purpose := range catalog.Purposes {
		result.Purposes = append(result.Purposes, cp.RuntimeFilePurpose(cp.RuntimeFilePurpose_value["RUNTIME_FILE_PURPOSE_"+purpose]))
	}
	return result
}
