package managedssh

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// ErrIdentityUploadEndpointInvalid is a fixed configuration diagnostic that never reflects an origin, certificate name or credential supplied through startup input.
// ErrIdentityUploadEndpointInvalid задаёт fixed configuration diagnostic без отражения origin, certificate name либо credential из startup input.
var ErrIdentityUploadEndpointInvalid = errors.New("SSH identity upload endpoint configuration is invalid")

// IdentityUploadEndpoint is an operator-configured direct browser-to-broker HTTPS origin, not a caller-selected URL or a route through generic credential materialization.
// IdentityUploadEndpoint задаёт operator-configured direct browser-to-broker HTTPS origin, а не caller-selected URL либо generic credential materialization route.
type IdentityUploadEndpoint struct{ Origin, TLSServerName string }

// Valid requires a canonical origin whose exact TLS host matches the selected destination; credentials, paths, queries, fragments and ambiguous default ports are rejected.
// Valid требует canonical origin с exact TLS host выбранного destination; credentials, paths, queries, fragments и ambiguous default ports запрещены.
func (endpoint IdentityUploadEndpoint) Valid() bool {
	u, err := url.Parse(endpoint.Origin)
	if err != nil || len(endpoint.Origin) > 2048 || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Host == "" || u.Hostname() == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" ||
		u.String() != endpoint.Origin || u.Hostname() != endpoint.TLSServerName || len(endpoint.TLSServerName) > 253 || endpoint.TLSServerName != strings.ToLower(endpoint.TLSServerName) {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 || n == 443 || strconv.Itoa(n) != port {
			return false
		}
	}
	canonicalHost := endpoint.TLSServerName
	if strings.Contains(canonicalHost, ":") {
		canonicalHost = "[" + canonicalHost + "]"
	}
	if u.Port() != "" {
		canonicalHost = net.JoinHostPort(endpoint.TLSServerName, u.Port())
	}
	if u.Host != canonicalHost {
		return false
	}
	if address := net.ParseIP(endpoint.TLSServerName); address != nil {
		return address.String() == endpoint.TLSServerName
	}
	for _, label := range strings.Split(endpoint.TLSServerName, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-') {
				return false
			}
		}
	}
	return true
}
