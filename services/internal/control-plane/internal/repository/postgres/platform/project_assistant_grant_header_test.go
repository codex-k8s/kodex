package platform

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Test-only tracer считает query identity, не сохраняет SQL/args/результаты.
type projectGrantProjectionCounter struct{ fullReads atomic.Int64 }

func (counter *projectGrantProjectionCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef ||
		data.SQL == queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef {
		counter.fullReads.Add(1)
	}
	return ctx
}

func (*projectGrantProjectionCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestProjectAssistantGrantConnectionHeaderClosed(t *testing.T) {
	valid := projectAssistantGrantConnectionHeader{ref: "int_owned", name: "Owned connection", version: 1, definitionKey: "github", definitionVersion: "6.0.0", definitionDigest: strings.Repeat("a", 64)}
	if !validProjectAssistantGrantConnectionHeader(valid, valid.ref) {
		t.Fatal("exact header rejected")
	}
	for _, field := range []string{"ref", "name", "version", "key", "revision", "digest"} {
		t.Run(field, func(t *testing.T) {
			value := valid
			switch field {
			case "ref":
				value.ref = "int_foreign"
			case "name":
				value.name = ""
			case "version":
				value.version = 0
			case "key":
				value.definitionKey = ""
			case "revision":
				value.definitionVersion = ""
			case "digest":
				value.definitionDigest = "PRIVATE_SENTINEL"
			}
			if validProjectAssistantGrantConnectionHeader(value, valid.ref) {
				t.Fatal("invalid header accepted")
			}
		})
	}
	if validProjectAssistantGrantConnectionHeader(valid, "") {
		t.Fatal("unbound header accepted")
	}
}
