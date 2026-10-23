// Package filetransfer validates narrow relative descriptors and configures bounded file HTTP clients.
// Пакет filetransfer проверяет узкие относительные дескрипторы и настраивает ограниченные файловые HTTP-клиенты.
package filetransfer

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

const (
	// HeaderTimeout allows one artifact-transfer budget plus fifteen seconds to receive headers, separate from ordinary callback command deadlines.
	// HeaderTimeout выделяет бюджет передачи артефакта плюс пятнадцать секунд на получение заголовков отдельно от таймаутов обычных команд обратного вызова.
	HeaderTimeout = runtimecontract.MaximumArtifactTransferDuration + 15*time.Second
	// TotalTimeout bounds the complete file HTTP exchange to two artifact-transfer budgets plus thirty seconds, including header and body time.
	// TotalTimeout ограничивает полный файловый HTTP-обмен двумя бюджетами передачи артефакта плюс тридцать секунд с учётом заголовков и тела.
	TotalTimeout = 2*runtimecontract.MaximumArtifactTransferDuration + 30*time.Second
)

// reference recognizes the typed lowercase-prefix/opaque-suffix descriptor locator syntax; it proves neither existence nor access.
// reference распознаёт синтаксис ссылки дескриптора с типизированным префиксом нижнего регистра и непрозрачным суффиксом; существование и доступ не доказываются.
var reference = regexp.MustCompile(`^[a-z][a-z0-9]*_[A-Za-z0-9_-]{8,80}$`)

// digest accepts only a sha256-prefixed lowercase 64-hex digest in the descriptor, not a proof that response bytes match it.
// digest принимает в дескрипторе только дайджест с префиксом sha256 и 64 шестнадцатеричными символами нижнего регистра, не доказывая соответствие байтов ответа.
var digest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// errDescriptor provides fixed rejection text for invalid local descriptor syntax or turn/catalog binding without copying rejected request data.
// errDescriptor задаёт фиксированный текст отказа для неверного синтаксиса локального дескриптора либо привязки хода и каталога без копирования отклонённых данных запроса.
var errDescriptor = errors.New("runtime file descriptor is invalid")

// CatalogRequest checks a relative GET descriptor against the immutable turn/lease and valid file catalog, with exactly four single-valued query selectors.
// Requires canonical positive revision, listed purpose and typed entry/artifact refs, then returns the expected sha256 digest.
// Current access and exact file revision belong to the controller/owner; this does not fetch bytes or verify their content.
// CatalogRequest проверяет относительный GET-дескриптор относительно неизменяемых хода, аренды и корректного каталога файлов с ровно четырьмя однозначными параметрами запроса.
// Требует каноническую положительную ревизию, перечисленную цель, типизированные ссылки элемента и артефакта, затем возвращает ожидаемый дайджест sha256.
// Актуальный доступ и точная ревизия файла принадлежат контроллеру и владельцу; байты не загружаются, их содержимое не проверяется.
func CatalogRequest(input runtimecontract.RunnerInput, request *http.Request) (string, error) {
	u := request.URL
	if request.Method != http.MethodGet || request.ContentLength > 0 || len(request.TransferEncoding) > 0 || u == nil || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || u.RawPath != "" || len(u.RawQuery) > 1024 ||
		input.Mode != runtimecontract.RunnerModeTurn || input.ProjectRef == "" || input.LeaseFence == "" || input.LeaseGeneration < 1 || input.FileCatalog == nil || input.FileCatalog.Validate() != nil {
		return "", errDescriptor
	}
	prefix := "/v1/executions/" + input.LeaseRef + "/artifacts/"
	if !reference.MatchString(input.LeaseRef) || !strings.HasPrefix(u.Path, prefix) {
		return "", errDescriptor
	}
	artifact := strings.TrimPrefix(u.Path, prefix)
	if !strings.HasPrefix(artifact, "art_") || !reference.MatchString(artifact) {
		return "", errDescriptor
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 4 {
		return "", errDescriptor
	}
	for _, key := range []string{"purpose", "entry_ref", "revision", "digest"} {
		if len(q[key]) != 1 || q.Get(key) == "" {
			return "", errDescriptor
		}
	}
	revision, err := strconv.ParseInt(q.Get("revision"), 10, 64)
	if err != nil || revision < 1 || revision > 9007199254740991 || strconv.FormatInt(revision, 10) != q.Get("revision") || !slices.Contains(input.FileCatalog.Purposes, q.Get("purpose")) || !strings.HasPrefix(q.Get("entry_ref"), "vfe_") || !reference.MatchString(q.Get("entry_ref")) || !digest.MatchString(q.Get("digest")) {
		return "", errDescriptor
	}
	return q.Get("digest"), nil
}

// NewClient clones the supplied non-nil HTTP transport, applies file-specific header/total budgets and a 16-KiB response-header cap, and refuses redirects.
// TLS/auth/proxy behavior comes from that transport; no weaker fallback or new authority is introduced.
// NewClient копирует переданный непустой HTTP-транспорт, задаёт файловые бюджеты заголовков и полного обмена, предел заголовков ответа 16 КиБ и запрещает перенаправления.
// Поведение TLS, авторизации и прокси наследуется от транспорта; ослабленного обходного пути или новых полномочий нет.
func NewClient(transport *http.Transport) *http.Client {
	fileTransport := transport.Clone()
	fileTransport.ResponseHeaderTimeout = HeaderTimeout
	fileTransport.MaxResponseHeaderBytes = 16 << 10
	return &http.Client{Transport: fileTransport, Timeout: TotalTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
