// Package connect parses a bounded, bodyless HTTP CONNECT request.
// Package connect разбирает ограниченный HTTP CONNECT без тела.
package connect

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

// maximumCredentialBytes bounds the decoded runtime credential before policy evaluation.
// maximumCredentialBytes ограничивает размер декодированного credential до проверки политики.
const maximumCredentialBytes = 24 << 10

// Reason enumerates closed, non-secret rejection categories for CONNECT parsing.
// Reason перечисляет закрытые категории отказа разбора CONNECT без секретных данных.
type Reason string

const (
	// ReasonMalformed identifies invalid HTTP request framing.
	// ReasonMalformed обозначает неверное обрамление HTTP-запроса.
	ReasonMalformed Reason = "malformed"
	// ReasonMethod identifies a method outside CONNECT or the exact readiness request.
	// ReasonMethod обозначает метод вне CONNECT или точного запроса готовности.
	ReasonMethod Reason = "method"
	// ReasonAuthority identifies a malformed or mismatched destination and Host header.
	// ReasonAuthority обозначает неверный адресат или несовпадение с заголовком Host.
	ReasonAuthority Reason = "authority"
	// ReasonBody identifies forbidden request-body framing or buffered body bytes.
	// ReasonBody обозначает запрещённое обрамление тела или уже прочитанные байты тела.
	ReasonBody Reason = "body"
	// ReasonCredentials identifies an invalid or unexpected credential header.
	// ReasonCredentials обозначает неверный или неожиданный заголовок credential.
	ReasonCredentials Reason = "credentials"
	// ReasonAuthenticationRequired identifies a CONNECT request without its required proxy credential.
	// ReasonAuthenticationRequired обозначает CONNECT без обязательного proxy credential.
	ReasonAuthenticationRequired Reason = "authentication_required"
	// ReasonOversized identifies a request exceeding its byte or header-count limits.
	// ReasonOversized обозначает превышение лимита байтов или числа заголовков.
	ReasonOversized Reason = "oversized"
	// ReasonPolicy identifies a parsed destination denied by the active policy.
	// ReasonPolicy обозначает разобранный адресат, отклонённый активной политикой.
	ReasonPolicy Reason = "policy"
)

// Error carries only a closed rejection reason, never untrusted request values.
// Error содержит только закрытую причину отказа, без недоверенных значений запроса.
type Error struct{ Reason Reason }

// Error returns a stable diagnostic string built only from the closed reason.
// Error возвращает устойчивую диагностику только из закрытой причины.
func (err *Error) Error() string { return "CONNECT request rejected: " + string(err.Reason) }

// Kind distinguishes the only request forms accepted by this listener.
// Kind различает единственные формы запросов, допускаемые этим listener.
type Kind uint8

const (
	// KindConnect denotes a validated tunnel request.
	// KindConnect обозначает проверенный запрос туннеля.
	KindConnect Kind = iota + 1
	// KindReadiness denotes the exact compatibility readiness request.
	// KindReadiness обозначает точный совместимый запрос готовности.
	KindReadiness
)

// Target holds the normalized, exact CONNECT destination after authority checks.
// Target содержит нормализованный точный адресат CONNECT после проверки authority.
type Target struct {
	Hostname string
	Port     int
}

// Request holds a validated CONNECT or compatibility readiness request.
// Request содержит проверенный CONNECT либо совместимый запрос готовности.
type Request struct {
	Kind   Kind
	Target Target
}

// Parse reads a bounded request, checks authority against Host, and retains the reader for ClientHello.
// Parse ограниченно читает запрос, сверяет authority с Host и сохраняет reader для ClientHello.
func Parse(connection net.Conn, maximumBytes int, timeout time.Duration, allows func(string, int) bool) (Request, *bufio.Reader, error) {
	return parse(connection, maximumBytes, timeout, allows, nil)
}

// ParseAuthenticated accepts only the runtime proxy's Basic credential and passes it to policy.
// ParseAuthenticated принимает только Basic credential служебного proxy и передаёт его политике.
// The credential is not retained in the returned Request.
// Credential не сохраняется в возвращаемом Request.
func ParseAuthenticated(connection net.Conn, maximumBytes int, timeout time.Duration, allows func(string, int, string) bool) (Request, *bufio.Reader, error) {
	return parse(connection, maximumBytes, timeout, nil, allows)
}

// parse enforces framing, headers, credentials, and active policy before returning a tunnel target.
// parse проверяет обрамление, заголовки, credential и активную политику до возврата адресата.
func parse(connection net.Conn, maximumBytes int, timeout time.Duration, allows func(string, int) bool, allowsAuthenticated func(string, int, string) bool) (Request, *bufio.Reader, error) {
	if connection == nil || maximumBytes < 1024 || timeout <= 0 || (allows == nil) == (allowsAuthenticated == nil) {
		return Request{}, nil, &Error{Reason: ReasonMalformed}
	}
	if err := connection.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return Request{}, nil, &Error{Reason: ReasonMalformed}
	}
	defer connection.SetReadDeadline(time.Time{})
	reader := bufio.NewReaderSize(connection, maximumBytes+1)
	total := 0
	requestLine, err := readLine(reader, &total, maximumBytes)
	if err != nil {
		return Request{}, nil, err
	}
	parts := strings.Split(requestLine, " ")
	if len(parts) != 3 || parts[2] != "HTTP/1.1" {
		return Request{}, nil, &Error{Reason: ReasonMalformed}
	}
	request := Request{}
	switch {
	case parts[0] == "CONNECT":
		target, targetErr := parseAuthority(parts[1])
		if targetErr != nil {
			return Request{}, nil, targetErr
		}
		request = Request{Kind: KindConnect, Target: target}
	case parts[0] == "GET" && parts[1] == "/readyz":
		request = Request{Kind: KindReadiness}
	default:
		reason := ReasonMalformed
		if parts[0] != "CONNECT" {
			reason = ReasonMethod
		}
		return Request{}, nil, &Error{Reason: reason}
	}
	headerCount := 0
	hostCount := 0
	credentialCount := 0
	credential := ""
	var hostTarget Target
	for {
		line, lineErr := readLine(reader, &total, maximumBytes)
		if lineErr != nil {
			return Request{}, nil, lineErr
		}
		if line == "" {
			break
		}
		headerCount++
		if headerCount > 64 || line[0] == ' ' || line[0] == '\t' {
			return Request{}, nil, &Error{Reason: ReasonOversized}
		}
		separator := strings.IndexByte(line, ':')
		if separator <= 0 {
			return Request{}, nil, &Error{Reason: ReasonMalformed}
		}
		name := strings.ToLower(line[:separator])
		value := strings.TrimSpace(line[separator+1:])
		if !validHeaderName(name) || !validHeaderValue(value) {
			return Request{}, nil, &Error{Reason: ReasonMalformed}
		}
		switch name {
		case "host":
			hostCount++
			if hostCount != 1 {
				return Request{}, nil, &Error{Reason: ReasonAuthority}
			}
			if request.Kind == KindConnect {
				hostTarget, err = parseAuthority(value)
			} else {
				hostTarget, err = parseReadinessAuthority(value)
			}
			if err != nil {
				return Request{}, nil, &Error{Reason: ReasonAuthority}
			}
		case "content-length", "transfer-encoding", "expect":
			return Request{}, nil, &Error{Reason: ReasonBody}
		case "proxy-authorization":
			if allowsAuthenticated == nil {
				return Request{}, nil, &Error{Reason: ReasonCredentials}
			}
			credentialCount++
			credential, err = parseProxyCredential(value)
			if credentialCount != 1 || err != nil {
				return Request{}, nil, &Error{Reason: ReasonCredentials}
			}
		case "authorization", "cookie":
			return Request{}, nil, &Error{Reason: ReasonCredentials}
		}
	}
	if hostCount != 1 || request.Kind == KindConnect && hostTarget != request.Target {
		return Request{}, nil, &Error{Reason: ReasonAuthority}
	}
	if reader.Buffered() != 0 {
		return Request{}, nil, &Error{Reason: ReasonBody}
	}
	if request.Kind == KindConnect {
		if allowsAuthenticated != nil && credentialCount == 0 {
			return Request{}, nil, &Error{Reason: ReasonAuthenticationRequired}
		}
		if allows != nil && !allows(request.Target.Hostname, request.Target.Port) ||
			allowsAuthenticated != nil && (credentialCount != 1 || !allowsAuthenticated(request.Target.Hostname, request.Target.Port, credential)) {
			return Request{}, nil, &Error{Reason: ReasonPolicy}
		}
	}
	return request, reader, nil
}

// parseProxyCredential decodes only the bounded Basic form reserved for runtime proxy grants.
// parseProxyCredential декодирует только ограниченную форму Basic для runtime proxy grants.
func parseProxyCredential(value string) (string, error) {
	const prefix = "Basic "
	if !strings.HasPrefix(value, prefix) {
		return "", errors.New("proxy credential is invalid")
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	if err != nil || len(decoded) < len("kodex:")+1 || len(decoded) > maximumCredentialBytes || !bytes.HasPrefix(decoded, []byte("kodex:")) {
		return "", errors.New("proxy credential is invalid")
	}
	credential := string(decoded[len("kodex:"):])
	if strings.TrimSpace(credential) != credential || credential == "" {
		return "", errors.New("proxy credential is invalid")
	}
	return credential, nil
}

// parseAuthority normalizes a CONNECT host and admits only registered tunnel ports.
// parseAuthority нормализует узел CONNECT и допускает только зарегистрированные порты туннеля.
func parseAuthority(value string) (Target, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, "@") {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || portValue == "" {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || strconv.Itoa(port) != portValue || (port != 443 && port != 465 && port != 587 && port != 995 && port != 110 && port != 993 && port != 143) {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	hostname, err := policy.NormalizeHostname(host)
	if err != nil {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	return Target{Hostname: hostname, Port: port}, nil
}

// parseReadinessAuthority admits only service readiness ports and a normalized host.
// parseReadinessAuthority допускает только служебные порты готовности и нормализованный узел.
func parseReadinessAuthority(value string) (Target, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, "@") {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || (portValue != "8080" && portValue != "8081" && portValue != "8082" && portValue != "8083") {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	hostname, err := policy.NormalizeHostname(host)
	if err != nil {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	port, _ := strconv.Atoi(portValue)
	return Target{Hostname: hostname, Port: port}, nil
}

// readLine requires CRLF and counts every consumed byte against the request limit.
// readLine требует CRLF и учитывает каждый прочитанный байт в лимите запроса.
func readLine(reader *bufio.Reader, total *int, maximum int) (string, error) {
	value, err := reader.ReadSlice('\n')
	*total += len(value)
	if *total > maximum || errors.Is(err, bufio.ErrBufferFull) {
		return "", &Error{Reason: ReasonOversized}
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", &Error{Reason: ReasonMalformed}
		}
		return "", &Error{Reason: ReasonMalformed}
	}
	if len(value) < 2 || !bytes.HasSuffix(value, []byte("\r\n")) {
		return "", &Error{Reason: ReasonMalformed}
	}
	return string(value[:len(value)-2]), nil
}

// validHeaderName accepts only lowercase HTTP token characters after name normalization.
// validHeaderName допускает только символы HTTP token после нормализации имени в нижний регистр.
func validHeaderName(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && !strings.ContainsRune("!#$%&'*+-.^_`|~", character) {
			return false
		}
	}
	return true
}

// validHeaderValue rejects control characters that could alter request framing.
// validHeaderValue отклоняет управляющие символы, способные изменить обрамление запроса.
func validHeaderValue(value string) bool {
	for _, character := range value {
		if character < 0x20 && character != '\t' || character == 0x7f {
			return false
		}
	}
	return true
}
