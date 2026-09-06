package callback

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/filetransfer"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// ServeCatalogFile не переносит caller headers или URL authority upstream.
// Controller уже завершил private spool/owner verification до ответа 200.
func (client *Client) ServeCatalogFile(writer http.ResponseWriter, request *http.Request, input model.Input) {
	expected, err := filetransfer.CatalogRequest(input, request)
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), filetransfer.TotalTimeout)
	defer cancel()
	control := http.NewResponseController(writer)
	deadline, _ := ctx.Deadline()
	if err := control.SetWriteDeadline(deadline); err != nil {
		http.Error(writer, "runtime file response deadline is unavailable", http.StatusServiceUnavailable)
		return
	}
	defer control.SetWriteDeadline(time.Time{})
	endpoint := *client.base
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery = request.URL.Path, "", request.URL.RawQuery
	outbound, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		http.Error(writer, "runtime file request is invalid", http.StatusBadGateway)
		return
	}
	outbound.Header.Set("Authorization", "Bearer "+client.token)
	bindExecutionHeaders(outbound, input, "artifact")
	outbound.Header.Set("Accept", "application/octet-stream")
	response, err := client.files.Do(outbound)
	if err != nil {
		http.Error(writer, "runtime file download is unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		http.Error(writer, "runtime file download was rejected", http.StatusBadGateway)
		return
	}
	if response.ContentLength < 0 || response.ContentLength > runtimecontract.MaximumArtifactTransferBytes || response.Header.Get("X-Kodex-Artifact-Digest") != expected || response.Header.Get("Content-Type") == "" {
		http.Error(writer, "runtime file response is invalid", http.StatusBadGateway)
		return
	}
	writer.Header().Set("Content-Length", strconv.FormatInt(response.ContentLength, 10))
	writer.Header().Set("Content-Type", response.Header.Get("Content-Type"))
	writer.Header().Set("X-Kodex-Artifact-Digest", expected)
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(writer, hash), io.LimitReader(response.Body, response.ContentLength+1))
	if err != nil || written != response.ContentLength || "sha256:"+hex.EncodeToString(hash.Sum(nil)) != expected {
		panic(http.ErrAbortHandler)
	}
}
