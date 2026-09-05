package httptransport

import (
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	sb "github.com/codex-k8s/kodex/libs/go/secretbrokerapi/gen/secretbroker/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type secretDraftRecorder struct {
	grpc.ClientConnInterface
	calls         []string
	requests      []proto.Message
	retainedValue []byte
	replay        bool
	failMethod    string
	failure       error
	mutateReceipt func(*cp.RuntimeSecretDraftOperationReceipt)
	mutateDraft   func(*sb.RuntimeSecretDraftMetadata)
	notReady      bool
}

func draftFixture() *cp.RuntimeSecretDraft {
	now := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	return &cp.RuntimeSecretDraft{Ref: "sdft_fixture01", ProjectRef: "prj_fixture01", SecretRef: "sec_fixture01", SecretVersion: 4, Version: 3, Generation: 1, Name: "Fixture", Description: "", ValueType: cp.RuntimeSecretValueType_RUNTIME_SECRET_VALUE_TYPE_STRING, State: cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT, CreatedAt: timestamppb.New(now), UpdatedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
}

func (c *secretDraftRecorder) Invoke(_ context.Context, method string, request, response any, _ ...grpc.CallOption) error {
	c.calls = append(c.calls, method)
	c.requests = append(c.requests, proto.Clone(request.(proto.Message)))
	if strings.HasSuffix(method, c.failMethod) && c.failMethod != "" {
		return c.failure
	}
	draft := draftFixture()
	target := cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT
	if strings.Contains(method, "Validate") {
		target = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_VALID
	}
	if strings.Contains(method, "Publish") {
		target = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED
		draft.SecretVersion = 7
	}
	if strings.Contains(method, "Discard") {
		target = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED
	}
	secret := &cp.RuntimeSecret{Ref: draft.SecretRef, ProjectRef: draft.ProjectRef, Version: 4, Name: draft.Name, Description: draft.Description, ValueType: draft.ValueType, State: "ACTIVE", CurrentRevision: 2, CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt}
	if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED {
		secret.Version = 8
	}
	if strings.Contains(method, "/Prepare") {
		op := &cp.RuntimeSecretDraftOperationReceipt{OperationRef: "sdop_fixture01", OperationGrant: "fixture-server-grant", State: cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_PREPARED, Draft: draft, ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
		if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT {
			draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PREPARING
		}
		if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED {
			draft.State = cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHING
		}
		if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED {
			draft.State = target
		}
		if c.replay {
			op.OperationGrant = ""
			op.State = cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED
			draft.State = target
			draft.Version++
			if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED {
				draft.PublishedRevision = 2
				draft.SecretVersion = secret.Version
				op.TerminalSecret = secret
			}
		}
		if c.mutateReceipt != nil {
			c.mutateReceipt(op)
		}
		switch out := response.(type) {
		case *cp.PrepareSaveRuntimeSecretDraftResponse:
			out.Operation = op
		case *cp.PrepareValidateRuntimeSecretDraftResponse:
			out.Operation = op
		case *cp.PreparePublishRuntimeSecretDraftResponse:
			out.Operation = op
		case *cp.PrepareDiscardRuntimeSecretDraftResponse:
			out.Operation = op
		}
		return nil
	}
	if out, ok := response.(*cp.GetRuntimeSecretDraftResponse); ok {
		out.Draft = draft
		if c.mutateReceipt != nil {
			op := &cp.RuntimeSecretDraftOperationReceipt{Draft: draft}
			c.mutateReceipt(op)
			out.Draft = op.Draft
		}
		return nil
	}
	if out, ok := response.(*sb.CheckSecretDraftReadinessResponse); ok {
		out.Ready = !c.notReady
		return nil
	}
	meta := &sb.RuntimeSecretDraftMetadata{Ref: draft.Ref, Version: draft.Version + 1, Generation: draft.Generation, ProjectRef: draft.ProjectRef, SecretRef: draft.SecretRef, SecretVersion: draft.SecretVersion, Name: draft.Name, Description: draft.Description, ValueType: sb.RuntimeSecretValueType(draft.ValueType), State: sb.RuntimeSecretDraftState(target), CreatedAt: draft.CreatedAt, UpdatedAt: draft.UpdatedAt, ExpiresAt: draft.ExpiresAt}
	if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DISCARDED {
		meta.Version = draft.Version
	}
	if target == cp.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_PUBLISHED {
		meta.PublishedRevision = 2
		meta.SecretVersion = secret.Version
	}
	if c.mutateDraft != nil {
		c.mutateDraft(meta)
	}
	switch out := response.(type) {
	case *sb.SaveSecretDraftResponse:
		out.Draft = meta
		c.retainedValue = request.(*sb.SaveSecretDraftRequest).Value
	case *sb.ValidateSecretDraftResponse:
		out.Draft = meta
	case *sb.DiscardSecretDraftResponse:
		out.Draft = meta
	case *sb.PublishSecretDraftResponse:
		out.Draft = meta
		out.Secret = &sb.RuntimeSecretMetadata{SecretRef: secret.Ref, ProjectRef: secret.ProjectRef, Version: secret.Version, Name: secret.Name, ValueType: sb.RuntimeSecretValueType(secret.ValueType), Status: sb.RuntimeSecretStatus_RUNTIME_SECRET_STATUS_ACTIVE, Revision: uint64(secret.CurrentRevision), CreatedAt: secret.CreatedAt, UpdatedAt: secret.UpdatedAt}
	}
	return nil
}

func secretDraftHandler(c *secretDraftRecorder) http.Handler {
	return generated.Handler(&Server{control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(c), Command: cp.NewPlatformCommandServiceClient(c)}, secretDrafts: sb.NewSecretBrokerServiceClient(c)})
}

func TestSecretDraftRoutesKeepOwnerPinsAndWriteOnlyValue(t *testing.T) {
	for _, test := range []struct {
		path, body, rpc, target string
		code                    int
	}{
		{"/projects/prj_fixture01/runtime-secret-drafts", `{"name":"Fixture","description":"","valueType":"STRING","value":"fixture-only-value"}`, "SaveSecretDraft", "DRAFT", 201},
		{"/runtime-secrets/sec_fixture01/drafts", `{"valueType":"STRING","value":"fixture-only-value"}`, "SaveSecretDraft", "DRAFT", 201},
		{"/runtime-secret-drafts/sdft_fixture01/validate", "", "ValidateSecretDraft", "VALID", 200},
		{"/runtime-secret-drafts/sdft_fixture01/publish", `{"expectedSecretVersion":7,"impactPlanRef":"sdip_fixture01","selectedItemRefs":["sdit_fixture01"]}`, "PublishSecretDraft", "PUBLISHED", 200},
		{"/runtime-secret-drafts/sdft_fixture01/discard", "", "DiscardSecretDraft", "DISCARDED", 200},
	} {
		for _, replay := range []bool{false, true} {
			t.Run(test.path+map[bool]string{false: "/effect", true: "/replay"}[replay], func(t *testing.T) {
				c := &secretDraftRecorder{replay: replay}
				w := httptest.NewRecorder()
				secretDraftHandler(c).ServeHTTP(w, managedTestRequest(http.MethodPost, "/api/v1"+test.path, test.body))
				if w.Code != test.code || !strings.Contains(w.Body.String(), `"state":"`+test.target+`"`) {
					t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
				}
				if replay && len(c.calls) != 1 || !replay && (len(c.calls) != 3 || !strings.HasSuffix(c.calls[1], "/CheckSecretDraftReadiness") || !strings.HasSuffix(c.calls[2], "/"+test.rpc)) {
					t.Fatalf("wrong effect chain %v", c.calls)
				}
				for _, forbidden := range []string{"fixture-only-value", "fixture-server-grant", "operationGrant", "contentSha256", "ciphertext", "encryptionKey"} {
					if strings.Contains(w.Body.String(), forbidden) {
						t.Fatal("private data escaped")
					}
				}
				if w.Header().Get("ETag") == "" || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing safe readback headers")
				}
				if input, ok := c.requests[0].(*cp.PrepareSaveRuntimeSecretDraftRequest); ok {
					if input.ExpectedContentSha256 != runtimeSecretSHA256(sha256.Sum256([]byte("fixture-only-value"))) || input.Mutation.IdempotencyKey != "managed-fixture-01" {
						t.Fatal("value commitment or idempotency lost")
					}
					if strings.HasPrefix(test.path, "/projects/") {
						if input.ProjectRef != "prj_fixture01" || input.Mutation.ExpectedVersion != nil || input.SecretRef != "" {
							t.Fatal("create acquired caller ownership/version")
						}
					} else if input.SecretRef != "sec_fixture01" || input.ProjectRef != "" || input.Mutation.GetExpectedVersion() != 3 {
						t.Fatal("existing Secret OCC lost")
					}
				}
				if input, ok := c.requests[0].(*cp.PreparePublishRuntimeSecretDraftRequest); ok {
					if input.ExpectedSecretVersion != 7 || input.Mutation.GetExpectedVersion() != 3 || input.ImpactPlanRef != "sdip_fixture01" || len(input.SelectedItemRefs) != 1 || input.SelectedItemRefs[0] != "sdit_fixture01" {
						t.Fatal("draft and Secret OCC mixed")
					}
				}
				for _, b := range c.retainedValue {
					if b != 0 {
						t.Fatal("plaintext buffer was not erased")
					}
				}
			})
		}
	}
}

func TestSecretDraftRejectsInvalidReceiptsAndBrokerReadback(t *testing.T) {
	for name, mutate := range map[string]func(*cp.RuntimeSecretDraftOperationReceipt){
		"missing draft":            func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft = nil },
		"wrong resource":           func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.Ref = "sdft_other01" },
		"unknown state":            func(o *cp.RuntimeSecretDraftOperationReceipt) { o.State = 99 },
		"missing grant":            func(o *cp.RuntimeSecretDraftOperationReceipt) { o.OperationGrant = "" },
		"unknown type":             func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.ValueType = 99 },
		"invalid expiry":           func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.ExpiresAt = nil },
		"unsafe version":           func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.Version = maximumSafeJSONInteger + 1 },
		"inconsistent publication": func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.PublishedRevision = 1 },
		"terminal with grant": func(o *cp.RuntimeSecretDraftOperationReceipt) {
			o.State = cp.RuntimeSecretOperationState_RUNTIME_SECRET_OPERATION_STATE_COMPLETED
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := &secretDraftRecorder{mutateReceipt: mutate}
			w := httptest.NewRecorder()
			secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", ""))
			if w.Code != 502 || len(c.calls) != 1 {
				t.Fatalf("invalid receipt reached broker: %d %v", w.Code, c.calls)
			}
		})
	}
	for name, mutate := range map[string]func(*sb.RuntimeSecretDraftMetadata){
		"wrong ref":        func(d *sb.RuntimeSecretDraftMetadata) { d.Ref = "sdft_other01" },
		"wrong owner":      func(d *sb.RuntimeSecretDraftMetadata) { d.ProjectRef = "prj_other01" },
		"wrong secret":     func(d *sb.RuntimeSecretDraftMetadata) { d.SecretRef = "sec_other01" },
		"wrong generation": func(d *sb.RuntimeSecretDraftMetadata) { d.Generation++ },
		"stale version":    func(d *sb.RuntimeSecretDraftMetadata) { d.Version = 3 },
		"unexpected state": func(d *sb.RuntimeSecretDraftMetadata) {
			d.State = sb.RuntimeSecretDraftState_RUNTIME_SECRET_DRAFT_STATE_DRAFT
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := &secretDraftRecorder{mutateDraft: mutate}
			w := httptest.NewRecorder()
			secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", ""))
			if w.Code != 502 {
				t.Fatalf("bad broker readback accepted: %d", w.Code)
			}
		})
	}
}

func TestSecretDraftPropagatesAuthorityAndUnknownOutcomeWithoutSuccess(t *testing.T) {
	for _, code := range []codes.Code{codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.Aborted, codes.FailedPrecondition, codes.Unavailable, codes.DeadlineExceeded} {
		for _, method := range []string{"PrepareValidateRuntimeSecretDraft", "CheckSecretDraftReadiness", "ValidateSecretDraft"} {
			t.Run(code.String()+method, func(t *testing.T) {
				c := &secretDraftRecorder{failMethod: method, failure: status.Error(code, "private upstream detail")}
				w := httptest.NewRecorder()
				secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", ""))
				if w.Code < 400 || strings.Contains(w.Body.String(), "private upstream") {
					t.Fatalf("error became success or leaked: %d", w.Code)
				}
				if method == "CheckSecretDraftReadiness" && len(c.calls) != 2 {
					t.Fatal("readiness failure reached effect")
				}
			})
		}
	}
	c := &secretDraftRecorder{notReady: true}
	w := httptest.NewRecorder()
	secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", ""))
	if w.Code != 503 || len(c.calls) != 2 {
		t.Fatal("false readiness reached effect")
	}
	for _, body := range []string{`{"expectedSecretVersion":0}`, `{"expectedSecretVersion":-1}`, `{"expectedSecretVersion":2,"value":"forbidden"}`} {
		c := &secretDraftRecorder{}
		w := httptest.NewRecorder()
		secretDraftHandler(c).ServeHTTP(w, managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/publish", body))
		if w.Code != 400 || len(c.calls) != 0 {
			t.Fatal("invalid publication reached owner")
		}
	}
	for _, header := range []string{"If-Match", "Idempotency-Key"} {
		c := &secretDraftRecorder{}
		w := httptest.NewRecorder()
		r := managedTestRequest("POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate", "")
		r.Header.Del(header)
		secretDraftHandler(c).ServeHTTP(w, r)
		if w.Code < 400 || len(c.calls) != 0 {
			t.Fatal("missing mutation fence reached owner")
		}
	}
}

func TestSecretDraftGetIsAuthoritativeAndPinned(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		c := &secretDraftRecorder{}
		if mismatch {
			c.mutateReceipt = func(o *cp.RuntimeSecretDraftOperationReceipt) { o.Draft.Ref = "sdft_other01" }
		}
		w := httptest.NewRecorder()
		secretDraftHandler(c).ServeHTTP(w, managedTestRequest("GET", "/api/v1/runtime-secret-drafts/sdft_fixture01", ""))
		if mismatch && w.Code != 502 || !mismatch && w.Code != 200 || len(c.calls) != 1 {
			t.Fatal("wrong authoritative read")
		}
	}
}
