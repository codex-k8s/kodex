// Package egressproxy проверяет тот же namespace-local proxy path, который
// используют внешние provider/Git effects.
package egressproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

func Check(ctx context.Context, rawURL string) error {
	proxy, err := url.Parse(rawURL)
	if err != nil || proxy.Scheme != "http" || proxy.Host == "" {
		return errors.New("management egress proxy URL is invalid")
	}
	ready := *proxy
	ready.Path = "/readyz"
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		ResponseHeaderTimeout: 2 * time.Second,
		DisableKeepAlives:     true,
	}
	defer transport.CloseIdleConnections()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ready.String(), nil)
	if err != nil {
		return errors.New("create management egress readiness request")
	}
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return errors.New("management egress proxy is unavailable")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
	if response.StatusCode != http.StatusNoContent {
		return errors.New("management egress proxy readiness was rejected")
	}
	return nil
}
