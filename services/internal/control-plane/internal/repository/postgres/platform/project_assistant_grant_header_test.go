package platform

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
)

// projectGrantProjectionCounter atomically counts only full connection/grant query identities
// without storing SQL arguments or results.
// projectGrantProjectionCounter атомарно считает только идентичности полных запросов соединений и
// разрешений без сохранения аргументов SQL либо результатов.
type projectGrantProjectionCounter struct{ fullReads atomic.Int64 }

// TraceQueryStart increments the full-read counter for either known connection/grant query and
// returns context unchanged.
// TraceQueryStart увеличивает счётчик полного чтения для одного из двух известных запросов
// соединения и разрешения и возвращает неизменный контекст.
func (counter *projectGrantProjectionCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL == queryQueriesAttachconnectionSelectIntegrationGrantsOrganizationIdConnectionIdRef ||
		data.SQL == queryQueriesGetintegrationconnectionSelectIntegrationConnectionsOrganizationIdRef {
		counter.fullReads.Add(1)
	}
	return ctx
}

// TraceQueryEnd intentionally records no query outcome because only query-start identity is
// counted.
// TraceQueryEnd намеренно не сохраняет результат запроса, поскольку учитывается только
// идентичность его начала.
func (*projectGrantProjectionCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

// TestProjectAssistantGrantConnectionHeaderClosed accepts an exact nonempty connection header and
// rejects foreign identity, missing metadata, invalid version/digest or an unbound reference.
// TestProjectAssistantGrantConnectionHeaderClosed принимает точный непустой заголовок соединения и
// отклоняет чужую идентичность, отсутствующие метаданные, неверную версию или дайджест и
// несвязанный ref.
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
