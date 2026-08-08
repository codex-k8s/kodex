// Package connect разбирает ограниченный bodyless HTTP CONNECT.
package connect

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/egress-gateway/internal/policy"
)

// Reason — закрытый набор причин отказа CONNECT parser.
type Reason string

const (
	ReasonMalformed   Reason = "malformed"
	ReasonMethod      Reason = "method"
	ReasonAuthority   Reason = "authority"
	ReasonBody        Reason = "body"
	ReasonCredentials Reason = "credentials"
	ReasonOversized   Reason = "oversized"
	ReasonPolicy      Reason = "policy"
)

// Error не содержит недоверенные request values.
type Error struct{ Reason Reason }

func (err *Error) Error() string { return "CONNECT request rejected: " + string(err.Reason) }

// Kind — закрытый набор допустимых запросов listener.
type Kind uint8

const (
	KindConnect Kind = iota + 1
	KindReadiness
)

// Target — проверенный exact CONNECT destination.
type Target struct {
	Hostname string
	Port     int
}

// Request — проверенный CONNECT либо compatibility readiness request.
type Request struct {
	Kind   Kind
	Target Target
}

// Parse bounded-читает request, проверяет authority/Host и сохраняет reader для ClientHello.
func Parse(connection net.Conn, maximumBytes int, timeout time.Duration, allows func(string, int) bool) (Request, *bufio.Reader, error) {
	if connection == nil || maximumBytes < 1024 || timeout <= 0 || allows == nil {
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
		case "authorization", "proxy-authorization", "cookie":
			return Request{}, nil, &Error{Reason: ReasonCredentials}
		}
	}
	if hostCount != 1 || request.Kind == KindConnect && hostTarget != request.Target {
		return Request{}, nil, &Error{Reason: ReasonAuthority}
	}
	if reader.Buffered() != 0 {
		return Request{}, nil, &Error{Reason: ReasonBody}
	}
	if request.Kind == KindConnect && !allows(request.Target.Hostname, request.Target.Port) {
		return Request{}, nil, &Error{Reason: ReasonPolicy}
	}
	return request, reader, nil
}

func parseAuthority(value string) (Target, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, "@") {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || portValue == "" {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	port, err := strconv.Atoi(portValue)
	if err != nil || port != 443 || portValue != "443" {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	hostname, err := policy.NormalizeHostname(host)
	if err != nil {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	return Target{Hostname: hostname, Port: port}, nil
}

func parseReadinessAuthority(value string) (Target, error) {
	if value == "" || strings.TrimSpace(value) != value || strings.Contains(value, "@") {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	host, portValue, err := net.SplitHostPort(value)
	if err != nil || portValue != "8080" {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	hostname, err := policy.NormalizeHostname(host)
	if err != nil {
		return Target{}, &Error{Reason: ReasonAuthority}
	}
	return Target{Hostname: hostname, Port: 8080}, nil
}

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

func validHeaderValue(value string) bool {
	for _, character := range value {
		if character < 0x20 && character != '\t' || character == 0x7f {
			return false
		}
	}
	return true
}
