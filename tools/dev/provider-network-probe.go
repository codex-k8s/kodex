// Probe проверяет только неавторизованный HTTPS transport из существующего Pod.
// Он не читает credentials, не запускает inference и не меняет сетевую policy.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

const probeProxy = "http://egress-gateway.kodex-system.svc.cluster.local:8080"

type probeResult struct {
	Host          string `json:"host"`
	ConnectStatus int    `json:"connectStatus"`
	HTTPStatus    int    `json:"httpStatus"`
	TLSVerified   bool   `json:"tlsVerified"`
	TransportOK   bool   `json:"transportOK"`
	Failure       string `json:"failure"`
}

func probeFailure(err error) string {
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "TIMEOUT"
	case errors.As(err, &certificate), errors.As(err, &unknownCA), errors.As(err, &hostname):
		return "TLS_CERTIFICATE"
	case errors.As(err, &dns):
		return "DNS"
	case errors.As(err, &network) && network.Timeout():
		return "TIMEOUT"
	default:
		return "TRANSPORT"
	}
}

func probeHost(ctx context.Context, host string) probeResult {
	result := probeResult{Host: host, Failure: "NONE"}
	if host != "chatgpt.com" && host != "auth.openai.com" {
		result.Failure = "TARGET_REJECTED"
		return result
	}
	proxy, _ := url.Parse(probeProxy)
	transport := &http.Transport{
		Proxy:               http.ProxyURL(proxy),
		DialContext:         (&net.Dialer{Timeout: 4 * time.Second}).DialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, ServerName: host},
		TLSHandshakeTimeout: 4 * time.Second, ResponseHeaderTimeout: 4 * time.Second,
		DisableKeepAlives: true,
		OnProxyConnectResponse: func(_ context.Context, actual *url.URL, request *http.Request, response *http.Response) error {
			if actual == nil || actual.String() != probeProxy || request == nil || request.Method != http.MethodConnect || request.Host != host+":443" || response == nil {
				return errors.New("proxy binding rejected")
			}
			result.ConnectStatus = response.StatusCode
			if response.StatusCode != http.StatusOK {
				return errors.New("proxy CONNECT rejected")
			}
			return nil
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("probe redirect rejected") }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/", nil)
	if err != nil {
		result.Failure = "REQUEST_REJECTED"
		return result
	}
	response, err := client.Do(request)
	if response != nil {
		defer response.Body.Close()
		result.HTTPStatus = response.StatusCode
		result.TLSVerified = response.TLS != nil && response.TLS.HandshakeComplete && response.TLS.Version >= tls.VersionTLS13 && len(response.TLS.VerifiedChains) > 0 && response.TLS.ServerName == host
	}
	if err != nil {
		result.Failure = probeFailure(err)
		if result.ConnectStatus != 0 && result.ConnectStatus != http.StatusOK {
			result.Failure = "CONNECT_REJECTED"
		}
		return result
	}
	result.TransportOK = result.ConnectStatus == http.StatusOK && result.TLSVerified
	return result
}

func main() {
	if len(os.Args) != 1 {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"failure": "ARGUMENTS_REJECTED"})
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Second)
	defer cancel()
	for _, host := range []string{"chatgpt.com", "auth.openai.com"} {
		_ = json.NewEncoder(os.Stdout).Encode(probeHost(ctx, host))
	}
}
