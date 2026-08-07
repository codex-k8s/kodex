package connect

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseAcceptsOnlyExactBodylessConnect(t *testing.T) {
	target, _, err := parseRequest(t, "CONNECT API.OpenAI.COM:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nUser-Agent: test\r\n\r\n", 4096)
	if err != nil || target.Hostname != "api.openai.com" || target.Port != 443 {
		t.Fatalf("unexpected target or error: %+v, %v", target, err)
	}
}

func TestParseRejectsHostileInputs(t *testing.T) {
	tests := []struct {
		name    string
		request string
		reason  Reason
	}{
		{"method", "GET api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\n", ReasonMethod},
		{"implicit port", "CONNECT api.openai.com HTTP/1.1\r\nHost: api.openai.com\r\n\r\n", ReasonAuthority},
		{"other port", "CONNECT api.openai.com:80 HTTP/1.1\r\nHost: api.openai.com:80\r\n\r\n", ReasonAuthority},
		{"IP", "CONNECT 127.0.0.1:443 HTTP/1.1\r\nHost: 127.0.0.1:443\r\n\r\n", ReasonAuthority},
		{"userinfo", "CONNECT user@api.openai.com:443 HTTP/1.1\r\nHost: user@api.openai.com:443\r\n\r\n", ReasonAuthority},
		{"conflicting Host", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: github.com:443\r\n\r\n", ReasonAuthority},
		{"duplicate Host", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nHost: api.openai.com:443\r\n\r\n", ReasonAuthority},
		{"body", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nContent-Length: 0\r\n\r\n", ReasonBody},
		{"transfer", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nTransfer-Encoding: chunked\r\n\r\n", ReasonBody},
		{"undeclared body", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\n\r\nbody", ReasonBody},
		{"credentials", "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nProxy-Authorization: value\r\n\r\n", ReasonCredentials},
		{"unknown", "CONNECT unknown.example:443 HTTP/1.1\r\nHost: unknown.example:443\r\n\r\n", ReasonPolicy},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseRequest(t, test.request, 4096)
			var parseErr *Error
			if !errors.As(err, &parseErr) || parseErr.Reason != test.reason {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestParseRejectsOversizedHeaders(t *testing.T) {
	request := "CONNECT api.openai.com:443 HTTP/1.1\r\nHost: api.openai.com:443\r\nX-Fill: " + strings.Repeat("a", 2048) + "\r\n\r\n"
	_, _, err := parseRequest(t, request, 1024)
	var parseErr *Error
	if !errors.As(err, &parseErr) || parseErr.Reason != ReasonOversized {
		t.Fatalf("unexpected error: %v", err)
	}
}

func parseRequest(t *testing.T, request string, maximum int) (Target, any, error) {
	t.Helper()
	server, client := net.Pipe()
	t.Cleanup(func() { server.Close(); client.Close() })
	go func() {
		_, _ = client.Write([]byte(request))
	}()
	target, reader, err := Parse(server, maximum, time.Second, func(host string, port int) bool {
		return host == "api.openai.com" && port == 443
	})
	return target, reader, err
}
