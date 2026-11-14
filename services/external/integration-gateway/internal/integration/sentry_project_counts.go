package integration

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// sentryProjectCounts requests only native project_total_received for the last 24 completed UTC hours, requiring every hourly bucket and exact integer counts; missing/unsupported/ambiguous data never becomes a zero or an ad-hoc organization query.
// sentryProjectCounts запрашивает только native project_total_received за последние 24 завершённых UTC часа, требуя каждый hourly bucket и exact integer counts; отсутствующие/неподдержанные/неоднозначные данные не становятся нулём либо ad-hoc organization query.
func (adapter *Adapter) sentryProjectCounts(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, now time.Time) (map[string]any, error) {
	end := now.UTC().Truncate(time.Hour)
	start := end.Add(-24 * time.Hour)
	query := url.Values{"stat": {"received"}, "resolution": {"1h"}, "since": {strconv.FormatInt(start.Unix(), 10)}, "until": {strconv.FormatInt(end.Unix()-1, 10)}}
	path := "/api/0/projects/" + configuration["organization_slug"] + "/" + configuration["project_slug"] + "/stats/"
	response, err := adapter.sentryJSON(ctx, request, capability, configuration, http.MethodGet, path, query, nil)
	if err != nil {
		var safe *SafeError
		if errors.As(err, &safe) && (safe.ProviderHTTPStatus == http.StatusNotFound || safe.ProviderHTTPStatus == http.StatusNotImplemented) {
			return nil, &SafeError{Code: providerCapabilityUnsupportedCode, ProviderHTTPStatus: safe.ProviderHTTPStatus}
		}
		return nil, err
	}
	var buckets [][2]int64
	if decodeTypedProviderJSON(response.Body, &buckets) != nil || len(buckets) != 24 {
		return nil, &SafeError{Code: providerResponseInvalidCode}
	}
	var total int64
	for index, bucket := range buckets {
		if bucket[0] != start.Unix()+int64(index)*3600 || bucket[1] < 0 || bucket[1] > 9007199254740991-total {
			return nil, &SafeError{Code: providerResponseInvalidCode}
		}
		total += bucket[1]
	}
	return map[string]any{"state": "VALIDATED", "stat": "received", "model": "project_total_received", "start": start.Format(time.RFC3339), "end_exclusive": end.Format(time.RFC3339), "interval_seconds": 3600, "buckets": buckets, "total": total}, nil
}
