package integration

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/google/uuid"
)

// grafanaSilenceMatcher retains the API's explicit matcher semantics; write callers select only one literal equality matcher, never regex or arbitrary configuration.
// grafanaSilenceMatcher сохраняет явную matcher semantics API; write caller выбирает только один literal equality matcher без regex либо arbitrary configuration.
type grafanaSilenceMatcher struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	IsRegex *bool  `json:"isRegex"`
	IsEqual *bool  `json:"isEqual,omitempty"`
}

// grafanaSilence projects suppression state without creator identity, user email, access metadata, alert bodies or routing configuration.
// grafanaSilence выдаёт suppression state без creator identity, user email, access metadata, alert bodies либо routing configuration.
type grafanaSilence struct {
	ID       string                  `json:"id"`
	Matchers []grafanaSilenceMatcher `json:"matchers"`
	StartsAt string                  `json:"startsAt"`
	EndsAt   string                  `json:"endsAt"`
	Comment  string                  `json:"comment"`
	Status   struct {
		State string `json:"state"`
	} `json:"status"`
}

// grafanaSilenceID accepts only a canonical lowercase UUID, excluding alternate UUID forms that could change provider path identity.
// grafanaSilenceID допускает только canonical lowercase UUID, исключая альтернативные формы, меняющие provider path identity.
func grafanaSilenceID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

// grafanaSilenceValid bounds current suppression data; provider permission checks and owner organization grants remain independently required.
// grafanaSilenceValid ограничивает current suppression data; provider permission checks и owner organization grants остаются отдельно обязательными.
func grafanaSilenceValid(item grafanaSilence, expected string) bool {
	if !grafanaSilenceID(item.ID) || expected != "" && item.ID != expected || len(item.Matchers) < 1 || len(item.Matchers) > 20 || len(item.Comment) > 1000 || len(item.StartsAt) > 40 || len(item.EndsAt) > 40 {
		return false
	}
	start, err := time.Parse(time.RFC3339Nano, item.StartsAt)
	if err != nil {
		return false
	}
	end, err := time.Parse(time.RFC3339Nano, item.EndsAt)
	if err != nil || end.Before(start) {
		return false
	}
	if item.Status.State != "active" && item.Status.State != "pending" && item.Status.State != "expired" {
		return false
	}
	for _, matcher := range item.Matchers {
		if !observabilityLabelName(matcher.Name) || len(matcher.Value) > 256 || matcher.IsRegex == nil {
			return false
		}
	}
	return true
}

// grafanaSilenceRead resolves the exact UUID under the current authenticated organization; no caller-supplied datasource or Alertmanager URL is accepted.
// grafanaSilenceRead разрешает exact UUID внутри current authenticated organization без caller-supplied datasource либо Alertmanager URL.
func (adapter *Adapter) grafanaSilenceRead(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, id string) (grafanaSilence, error) {
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, "/api/alertmanager/grafana/api/v2/silence/"+id, nil, nil, false)
	if err != nil {
		return grafanaSilence{}, err
	}
	var item grafanaSilence
	if grafanaDecode(response.Body, &item) != nil || !grafanaSilenceValid(item, id) {
		return grafanaSilence{}, &SafeError{Code: providerResponseInvalidCode}
	}
	return item, nil
}

// grafanaSilencePermissions requires the provider's general-silence instance action and read access; fine-grained rule-only silence permissions are not misrepresented as authority to suppress arbitrary matchers.
// grafanaSilencePermissions требует provider general-silence instance action и read access; fine-grained rule-only silence permissions не выдаются за authority для arbitrary matchers.
func grafanaSilencePermissions(permissions map[string][]string, operation string) bool {
	return len(permissions[grafanaWriteAction(operation)]) > 0 && (len(permissions["alert.instances:read"]) > 0 || len(permissions["alert.silences:read"]) > 0)
}

// grafanaSilences uses only Grafana's organization-bound Alertmanager API, one literal equality matcher per effect and explicit EACH admission; DELETE records expiration requested rather than claiming physical removal.
// grafanaSilences использует только organization-bound Alertmanager API Grafana, один literal equality matcher на effect и явный EACH admission; DELETE фиксирует expiration requested без заявления физического удаления.
func (adapter *Adapter) grafanaSilences(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, permissions map[string][]string) (any, string, error) {
	const collection = "/api/alertmanager/grafana/api/v2/silences"
	reference := "grafana-silence:" + configuration["organization_id"]
	name, _ := request.Input["matcher_name"].(string)
	value, _ := request.Input["matcher_value"].(string)
	if request.Operation == "grafana.silence.list" {
		if !observabilityLabelName(name) || value == "" || len(value) > 256 {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		query := url.Values{"filter": {name + "=" + strconv.Quote(value)}}
		response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodGet, collection, query, nil, false)
		if err != nil {
			return nil, "", err
		}
		var items []grafanaSilence
		if grafanaDecode(response.Body, &items) != nil || items == nil || len(items) > 50 {
			return nil, "", &SafeError{Code: providerResponseInvalidCode}
		}
		seen := map[string]bool{}
		for _, item := range items {
			if !grafanaSilenceValid(item, "") || seen[item.ID] {
				return nil, "", &SafeError{Code: providerResponseInvalidCode}
			}
			seen[item.ID] = true
		}
		return map[string]any{"items": items, "limit_reached": len(items) == 50}, reference, nil
	}
	id, _ := request.Input["silence_id"].(string)
	if request.Operation != "grafana.silence.create" {
		if !grafanaSilenceID(id) {
			return nil, "", &SafeError{Code: providerRequestRejectedCode}
		}
		item, err := adapter.grafanaSilenceRead(ctx, request, capability, configuration, id)
		if err != nil {
			return nil, "", err
		}
		if request.Operation == "grafana.silence.read" {
			return item, reference + ":" + id, nil
		}
	}
	if !grafanaSilencePermissions(permissions, request.Operation) {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	if request.Operation == "grafana.silence.delete" {
		response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodDelete, "/api/alertmanager/grafana/api/v2/silence/"+id, nil, nil, false)
		if err != nil {
			return nil, "", err
		}
		var receipt struct {
			Message string `json:"message"`
		}
		if grafanaDecode(response.Body, &receipt) != nil || receipt.Message != "silence deleted" {
			return nil, "", &UnknownOutcomeError{stage: "response_validation"}
		}
		return map[string]string{"id": id, "status": "expiration_requested"}, reference + ":" + id, nil
	}
	if request.Operation != "grafana.silence.create" && request.Operation != "grafana.silence.update" {
		return nil, "", &SafeError{Code: providerCapabilityUnsupportedCode}
	}
	start, err := observabilityTime(request.Input, "start")
	if err != nil {
		return nil, "", err
	}
	end, err := observabilityTime(request.Input, "end")
	if err != nil {
		return nil, "", err
	}
	comment, _ := request.Input["comment"].(string)
	if !observabilityLabelName(name) || value == "" || len(value) > 256 || comment == "" || len(comment) > 1000 || !end.After(start) || end.Sub(start) > 24*time.Hour || start.Nanosecond()%1000000 != 0 || end.Nanosecond()%1000000 != 0 {
		return nil, "", &SafeError{Code: providerRequestRejectedCode}
	}
	body := map[string]any{"matchers": []map[string]any{{"name": name, "value": value, "isRegex": false, "isEqual": true}}, "startsAt": start.Format(time.RFC3339Nano), "endsAt": end.Format(time.RFC3339Nano), "createdBy": "Kodex", "comment": comment}
	if id != "" {
		body["id"] = id
	}
	response, err := adapter.grafanaJSON(ctx, request, capability, configuration, http.MethodPost, collection, nil, body, false)
	if err != nil {
		return nil, "", err
	}
	var receipt struct {
		SilenceID string `json:"silenceID"`
	}
	if response.StatusCode != http.StatusAccepted || grafanaDecode(response.Body, &receipt) != nil || !grafanaSilenceID(receipt.SilenceID) {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	confirmed, err := adapter.grafanaSilenceRead(ctx, request, capability, configuration, receipt.SilenceID)
	if err != nil || len(confirmed.Matchers) != 1 || confirmed.Matchers[0].Name != name || confirmed.Matchers[0].Value != value || *confirmed.Matchers[0].IsRegex || confirmed.Matchers[0].IsEqual != nil && !*confirmed.Matchers[0].IsEqual || confirmed.Comment != comment {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	confirmedStart, errStart := time.Parse(time.RFC3339Nano, confirmed.StartsAt)
	confirmedEnd, errEnd := time.Parse(time.RFC3339Nano, confirmed.EndsAt)
	if errStart != nil || errEnd != nil || !confirmedStart.Equal(start) || !confirmedEnd.Equal(end) {
		return nil, "", &UnknownOutcomeError{stage: "response_validation"}
	}
	return confirmed, reference + ":" + receipt.SilenceID, nil
}
