package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// TestWriteReadRPCResultPreservesExistingProjection compares the extracted helper with the previous error-first branch for public whole/nested/page data and invalid upstream shapes. Nil/partial replies paired with errors must never be inspected; every gRPC code retains the existing status, headers and body, and successful input protobufs remain unchanged.
// TestWriteReadRPCResultPreservesExistingProjection сравнивает вынесенный helper с прежней error-first ветвью для полного, вложенного, страничного и неверного ответа. Nil и частичные ответы при ошибке не должны разбираться; все gRPC-коды сохраняют прежние статус, заголовки и тело, успешные исходные protobuf не меняются.
func TestWriteReadRPCResultPreservesExistingProjection(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		message        proto.Message
		err            error
		field, pageKey string
	}{
		{name: "whole", message: &controlplanev1.PermissionDefinition{Key: "agent.launch", Risk: controlplanev1.PermissionRisk_PERMISSION_RISK_WRITE}},
		{name: "nested", message: &controlplanev1.GetAgentResponse{Agent: &controlplanev1.Agent{Ref: "agt_fixture01", Name: "TYPE_literal", Version: 7}}, field: "agent"},
		{name: "empty terminal page", message: &controlplanev1.ListSchedulesResponse{}, pageKey: "schedules"},
		{name: "localized page metadata", message: &controlplanev1.ListIntegrationDefinitionsResponse{
			Definitions: []*controlplanev1.IntegrationDefinition{{Key: "example", Version: 3, Name: "i18n:CATALOG_LABEL", ConnectionCount: 7}},
			CoreReady:   true, NextActions: []controlplanev1.NextAction{controlplanev1.NextAction_NEXT_ACTION_CREATE_CONNECTION},
		}, pageKey: "definitions"},
		{name: "invalid public shape", message: &controlplanev1.AgentAvatar{}},
		{name: "unsafe integer", message: &controlplanev1.AssistantPlan{Version: 1 << 53}},
		{name: "nil reply on error", err: status.Error(codes.Unavailable, "upstream detail must not escape")},
		{name: "typed nil reply on error", message: (*controlplanev1.GetProjectResponse)(nil), err: status.Error(codes.PermissionDenied, "private detail")},
		{name: "partial reply on error", message: &controlplanev1.GetProjectResponse{Project: &controlplanev1.Project{Name: "private partial result"}}, err: status.Error(codes.NotFound, "private detail"), field: "project"},
		{name: "ordinary error", err: errors.New("private diagnostic")},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
			expected := &localizingRecorder{ResponseRecorder: httptest.NewRecorder()}
			before := proto.Clone(test.message)
			if test.err != nil {
				writeRPCProblem(expected, test.err)
			} else {
				writeMessage(expected, http.StatusOK, test.message, test.field, test.pageKey)
			}
			writeReadRPCResult(actual, test.message, test.err, test.field, test.pageKey)
			assertReadRPCProjection(t, actual.ResponseRecorder, expected.ResponseRecorder)
			if !proto.Equal(before, test.message) {
				t.Fatal("input protobuf changed")
			}
		})
	}
	for code := codes.OK; code <= codes.Unauthenticated; code++ {
		t.Run(code.String(), func(t *testing.T) {
			err := status.Error(code, "private diagnostic")
			if err == nil {
				return
			}
			actual, expected := httptest.NewRecorder(), httptest.NewRecorder()
			writeRPCProblem(expected, err)
			writeReadRPCResult(actual, nil, err, "ignored", "ignored")
			assertReadRPCProjection(t, actual, expected)
		})
	}
}

// assertReadRPCProjection compares every response header, status and JSON field except the intentionally fresh correlation UUID, whose canonical form and independence are checked separately. It does not drop unknown fields or normalize domain data to make unequal responses pass.
// assertReadRPCProjection сравнивает все заголовки, статус и JSON-поля, кроме намеренно нового correlation UUID, чьи каноничность и независимость проверяются отдельно. Неизвестные поля не удаляются, доменные данные не нормализуются ради совпадения ответов.
func assertReadRPCProjection(t *testing.T, actual, expected *httptest.ResponseRecorder) {
	t.Helper()
	if actual.Code != expected.Code || !reflect.DeepEqual(actual.Header(), expected.Header()) {
		t.Fatal("read RPC status or headers changed")
	}
	var actualBody, expectedBody map[string]any
	if err := json.Unmarshal(actual.Body.Bytes(), &actualBody); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected.Body.Bytes(), &expectedBody); err != nil {
		t.Fatal(err)
	}
	if actual.Header().Get("Content-Type") == "application/problem+json" {
		actualID, aok := actualBody["correlationId"].(string)
		expectedID, eok := expectedBody["correlationId"].(string)
		a, aerr := uuid.Parse(actualID)
		e, eerr := uuid.Parse(expectedID)
		if !aok || !eok || aerr != nil || eerr != nil || a.String() != actualID || e.String() != expectedID || a == uuid.Nil || e == uuid.Nil || actualID == expectedID {
			t.Fatal("problem correlation IDs are not fresh canonical UUIDs")
		}
		delete(actualBody, "correlationId")
		delete(expectedBody, "correlationId")
	}
	if !reflect.DeepEqual(actualBody, expectedBody) {
		t.Fatal("read RPC public JSON changed")
	}
}

// TestArtifactCatalogFiltersPreserveSelection checks absent/explicit enum distinctions, ordered source groups, duplicates, bounds and single/group conflict. Successful filter-only requests remain independent and carry no project/run/page/query; invalid inputs expose no partial request. The helper does not authorize a scope or mark a file clean.
// TestArtifactCatalogFiltersPreserveSelection проверяет различие отсутствующих и явных enum, порядок групп, дубликаты, пределы и конфликт одиночного и группового источников. Успешные запросы независимы и содержат только фильтры без project/run/page/query; неверный вход не выдаёт частичный запрос. Helper не авторизует область и не помечает файл чистым.
func TestArtifactCatalogFiltersPreserveSelection(t *testing.T) {
	t.Parallel()
	text := func(value string) *string { return &value }
	for _, test := range []struct {
		name                          string
		lifecycle, kind, scan, source *string
		sources                       []string
		want                          *controlplanev1.ListArtifactsRequest
	}{
		{name: "absent", want: &controlplanev1.ListArtifactsRequest{}},
		{name: "explicit unspecified fields", lifecycle: text("UNSPECIFIED"), kind: text("UNSPECIFIED"), scan: text("UNSPECIFIED"), want: &controlplanev1.ListArtifactsRequest{}},
		{name: "known lifecycle", lifecycle: text("DELETED"), want: &controlplanev1.ListArtifactsRequest{LifecycleState: controlplanev1.ArtifactLifecycleState_ARTIFACT_LIFECYCLE_STATE_DELETED}},
		{name: "known type and scan", kind: text("DOCUMENT"), scan: text("CLEAN"), want: &controlplanev1.ListArtifactsRequest{Type: controlplanev1.ArtifactType_ARTIFACT_TYPE_DOCUMENT, ScanState: controlplanev1.ArtifactScanState_ARTIFACT_SCAN_STATE_CLEAN}},
		{name: "known source", source: text("AGENT_RESULT"), want: &controlplanev1.ListArtifactsRequest{SourceKind: controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT}},
		{name: "ordered group", sources: []string{"AGENT_RESULT", "INTERACTION_ATTACHMENT"}, want: &controlplanev1.ListArtifactsRequest{SourceKinds: []controlplanev1.ArtifactSource{controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT, controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTERACTION_ATTACHMENT}}},
		{name: "empty group", sources: []string{}, want: &controlplanev1.ListArtifactsRequest{}},
		{name: "maximum unique group", sources: []string{"CONTROL_CENTER", "AGENT_RESULT", "INTEGRATION_RESULT", "KNOWLEDGE_SOURCE", "INTERACTION_ATTACHMENT"}, want: &controlplanev1.ListArtifactsRequest{SourceKinds: []controlplanev1.ArtifactSource{controlplanev1.ArtifactSource_ARTIFACT_SOURCE_CONTROL_CENTER, controlplanev1.ArtifactSource_ARTIFACT_SOURCE_AGENT_RESULT, controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTEGRATION_RESULT, controlplanev1.ArtifactSource_ARTIFACT_SOURCE_KNOWLEDGE_SOURCE, controlplanev1.ArtifactSource_ARTIFACT_SOURCE_INTERACTION_ATTACHMENT}}},
		{name: "empty lifecycle", lifecycle: text("")},
		{name: "unknown type", kind: text("UNKNOWN")},
		{name: "unknown scan", scan: text("UNKNOWN")},
		{name: "unknown source", source: text("UNKNOWN")},
		{name: "explicit unspecified source", source: text("UNSPECIFIED")},
		{name: "unknown group source", sources: []string{"AGENT_RESULT", "UNKNOWN"}},
		{name: "duplicate group", sources: []string{"AGENT_RESULT", "AGENT_RESULT"}},
		{name: "single and group", source: text("AGENT_RESULT"), sources: []string{"INTERACTION_ATTACHMENT"}},
		{name: "oversized group", sources: []string{"AGENT_RESULT", "AGENT_RESULT", "AGENT_RESULT", "AGENT_RESULT", "AGENT_RESULT", "AGENT_RESULT"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, ok := artifactCatalogFilters(test.lifecycle, test.kind, test.scan, test.source, &test.sources)
			if ok != (test.want != nil) || !proto.Equal(actual, test.want) {
				t.Fatal("artifact filter selection changed")
			}
			if !ok {
				if actual != nil {
					t.Fatal("partial invalid request returned")
				}
				return
			}
			if actual.ProjectRef != "" || actual.RunRef != "" || actual.Page != nil || actual.Query != "" {
				t.Fatal("filter helper assigned caller scope")
			}
			again, valid := artifactCatalogFilters(test.lifecycle, test.kind, test.scan, test.source, &test.sources)
			if !valid || again == actual || !proto.Equal(again, actual) {
				t.Fatal("request allocation is not independent")
			}
			if len(actual.SourceKinds) > 0 {
				actual.SourceKinds[0] = controlplanev1.ArtifactSource_ARTIFACT_SOURCE_UNSPECIFIED
				if !proto.Equal(again, test.want) {
					t.Fatal("source group backing slice was shared")
				}
			}
		})
	}
	if request, ok := artifactCatalogFilters((*string)(nil), (*string)(nil), (*string)(nil), (*string)(nil), (*[]string)(nil)); !ok || !proto.Equal(request, &controlplanev1.ListArtifactsRequest{}) {
		t.Fatal("absent group changed")
	}
}
