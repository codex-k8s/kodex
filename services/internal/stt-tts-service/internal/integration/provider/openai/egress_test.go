package openai

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testEgressConfig() EgressConfig {
	return EgressConfig{Revision: "test-generation", Digest: strings.Repeat("a", 64)}
}
func testEgressHeaders() http.Header {
	result := http.Header{}
	for name, value := range map[string]string{"Revision": testEgressConfig().Revision, "Digest": testEgressConfig().Digest, "Profile": "openai-stt", "Workload": "stt-tts-service", "Operation": "openai.transcription"} {
		result.Set("X-Kodex-Egress-"+name, value)
	}
	return result
}

func TestEgressReadbackRequiresEveryExactHeader(t *testing.T) {
	for _, name := range []string{"Revision", "Digest", "Profile", "Workload", "Operation"} {
		for _, mode := range []string{"absent", "wrong", "duplicate"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				headers := testEgressHeaders()
				key := "X-Kodex-Egress-" + name
				switch mode {
				case "absent":
					headers.Del(key)
				case "wrong":
					headers.Set(key, "other")
				case "duplicate":
					headers.Add(key, headers.Get(key))
				}
				client, _ := NewWithHTTPClient(doerFunc(func(request *http.Request) (*http.Response, error) {
					if request.URL.String() != ProxyURL+"/readyz" || request.Header.Get("Authorization") != "" {
						t.Fatal("readback boundary mismatch")
					}
					return &http.Response{StatusCode: 204, Header: headers, Body: io.NopCloser(strings.NewReader(""))}, nil
				}))
				client.egress = testEgressConfig()
				if client.CheckEgress(t.Context()) == nil {
					t.Fatal("bad readback accepted")
				}
				proxy, _ := url.Parse(ProxyURL)
				request := &http.Request{Method: http.MethodConnect, Host: "api.openai.com:443", URL: &url.URL{Opaque: "api.openai.com:443"}}
				if testEgressConfig().onConnect(t.Context(), proxy, request, &http.Response{StatusCode: 200, Header: headers}) == nil {
					t.Fatal("bad CONNECT accepted")
				}
			})
		}
	}
	for _, status := range []int{204, 503} {
		client, _ := NewWithHTTPClient(doerFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: testEgressHeaders(), Body: io.NopCloser(strings.NewReader(""))}, nil
		}))
		client.egress = testEgressConfig()
		if (client.CheckEgress(t.Context()) == nil) != (status == 204) {
			t.Fatal("readback status mismatch")
		}
	}
}

// Два новых соединения к разным поколениям имитируют rolling replicas.
// Только совпадающий CONNECT допускает даже первый TLS byte.
func TestEveryCONNECTChecksGenerationBeforeTLS(t *testing.T) {
	observed := make(chan bool, 2)
	calls := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		headers := testEgressHeaders()
		if calls == 2 {
			headers.Set("X-Kodex-Egress-Revision", "stale-generation")
		}
		connection, buffer, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n")
		_ = headers.Write(buffer)
		_, _ = buffer.WriteString("\r\n")
		_ = buffer.Flush()
		_ = connection.SetReadDeadline(time.Now().Add(time.Second))
		first, err := buffer.ReadByte()
		observed <- err == nil && first == 22
	}))
	defer proxy.Close()
	client, err := New(testEgressConfig())
	if err != nil {
		t.Fatal(err)
	}
	transport := client.http.(*http.Client).Transport.(*http.Transport)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(proxy.URL, "http://"))
	}
	defer transport.CloseIdleConnections()
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint, nil)
		response, err := client.http.Do(request)
		cancel()
		if response != nil {
			response.Body.Close()
		}
		if err == nil {
			t.Fatal("fake tunnel unexpectedly reached provider")
		}
		if got := <-observed; got != (i == 0) {
			t.Fatal("CONNECT generation was not checked before TLS")
		}
	}
}
