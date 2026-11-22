package managedkubernetes

import (
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// ValidHTTPSOrigin checks a canonical scheme/host/port coordinate without resolving or dialing it.
// Loopback is allowed as explicit kubeconfig source metadata, never as an implicit transport route.
// ValidHTTPSOrigin проверяет canonical scheme/host/port без DNS либо сетевого обращения.
// Loopback допустим как explicit kubeconfig source metadata, не как implicit transport route.
func ValidHTTPSOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || len(origin) > 1024 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery ||
		strings.ContainsAny(u.Host, "\\%") || u.String() != origin {
		return false
	}
	host := u.Hostname()
	if address, err := netip.ParseAddr(host); err == nil {
		if address.String() != host || address.Is4In6() || address.Zone() != "" || address.IsMulticast() {
			return false
		}
	} else {
		if len(validation.IsDNS1123Subdomain(host)) != 0 {
			return false
		}
		for _, label := range strings.Split(host, ".") {
			if len(validation.IsDNS1123Label(label)) != 0 {
				return false
			}
		}
	}
	expectedHost := host
	if port := u.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
			return false
		}
		expectedHost = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		expectedHost = "[" + host + "]"
	}
	return u.Host == expectedHost
}

// ValidKubeconfigSourceOrigins accepts a bounded exact operator-owned alias set without defaults.
// Every alias belongs to its containing ClusterUID/target revision; syntax does not prove that binding.
// ValidKubeconfigSourceOrigins принимает bounded exact operator-owned aliases без defaults.
// Alias относится к enclosing ClusterUID/target revision; syntax не доказывает эту binding.
func ValidKubeconfigSourceOrigins(origins []string) bool {
	if len(origins) == 0 || len(origins) > 8 {
		return false
	}
	seen := make(map[string]bool, len(origins))
	for _, origin := range origins {
		if !ValidHTTPSOrigin(origin) || seen[origin] {
			return false
		}
		seen[origin] = true
	}
	return true
}

// AllowsKubeconfigSourceOrigin matches bytes against the validated explicit alias set.
// The selected alias never becomes a network destination, TLS authority or source of authorization.
// AllowsKubeconfigSourceOrigin сопоставляет bytes с validated explicit aliases.
// Выбранный alias не становится network destination, TLS authority либо источником полномочий.
func AllowsKubeconfigSourceOrigin(origins []string, source string) bool {
	if !ValidKubeconfigSourceOrigins(origins) {
		return false
	}
	for _, origin := range origins {
		if origin == source {
			return true
		}
	}
	return false
}
