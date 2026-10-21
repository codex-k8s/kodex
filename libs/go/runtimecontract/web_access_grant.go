package runtimecontract

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	// runtimeWebAccessGrantVersion pins the proxy grant payload schema.
	// runtimeWebAccessGrantVersion закрепляет схему разрешения для proxy.
	runtimeWebAccessGrantVersion = 1
	// maximumWebAccessGrantBytes bounds the decoded proxy grant payload.
	// maximumWebAccessGrantBytes ограничивает размер разобранного разрешения proxy.
	maximumWebAccessGrantBytes = 16 << 10
)

// RuntimeWebAccessGrant binds proxy access to the exact workload and immutable network-policy digest;
// its signed value travels only in Proxy-Authorization, never in logs or user events.
// RuntimeWebAccessGrant связывает доступ proxy с точной нагрузкой и неизменным дайджестом сетевой политики;
// подписанное значение передаётся лишь в Proxy-Authorization, не в логах или событиях пользователя.
type RuntimeWebAccessGrant struct {
	Version       int              `json:"v"`
	Audience      string           `json:"aud"`
	WorkloadRef   string           `json:"workload_ref"`
	NetworkDigest string           `json:"network_digest"`
	WebAccess     RuntimeWebAccess `json:"web_access"`
}

// SignRuntimeWebAccessGrant validates bounded claims and signs their JSON bytes with HMAC-SHA256.
// SignRuntimeWebAccessGrant проверяет ограниченные поля и подписывает их JSON-байты через HMAC-SHA256.
func SignRuntimeWebAccessGrant(key []byte, workloadRef, networkDigest string, access RuntimeWebAccess) (string, error) {
	claims := RuntimeWebAccessGrant{Version: runtimeWebAccessGrantVersion, Audience: "kodex-runtime-egress",
		WorkloadRef: workloadRef, NetworkDigest: networkDigest, WebAccess: access}
	if err := validateRuntimeWebAccessGrant(claims); err != nil || len(key) < 32 {
		return "", errors.New("runtime web access grant input is invalid")
	}
	payload, err := json.Marshal(claims)
	if err != nil || len(payload) > maximumWebAccessGrantBytes {
		return "", errors.New("encode runtime web access grant")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

// VerifyRuntimeWebAccessGrant verifies size, encoding, constant-time MAC, and strict claim shape.
// VerifyRuntimeWebAccessGrant проверяет размер, кодировку, MAC в постоянное время и строгую форму полей.
func VerifyRuntimeWebAccessGrant(key []byte, token string) (RuntimeWebAccessGrant, error) {
	if len(key) < 32 || len(token) == 0 || len(token) > maximumWebAccessGrantBytes*2 {
		return RuntimeWebAccessGrant{}, errors.New("runtime web access grant is invalid")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return RuntimeWebAccessGrant{}, errors.New("runtime web access grant is invalid")
	}
	payload, payloadErr := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[1])
	if payloadErr != nil || signatureErr != nil || len(payload) == 0 || len(payload) > maximumWebAccessGrantBytes || len(signature) != sha256.Size {
		return RuntimeWebAccessGrant{}, errors.New("runtime web access grant is invalid")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return RuntimeWebAccessGrant{}, errors.New("runtime web access grant is invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	var claims RuntimeWebAccessGrant
	if decoder.Decode(&claims) != nil || validateRuntimeWebAccessGrant(claims) != nil {
		return RuntimeWebAccessGrant{}, errors.New("runtime web access grant is invalid")
	}
	return claims, nil
}

// validateRuntimeWebAccessGrant rejects wrong audience, workload, digest, or access policy.
// validateRuntimeWebAccessGrant отклоняет неверные audience, нагрузку, дайджест или политику доступа.
func validateRuntimeWebAccessGrant(claims RuntimeWebAccessGrant) error {
	if claims.Version != runtimeWebAccessGrantVersion || claims.Audience != "kodex-runtime-egress" ||
		claims.WorkloadRef == "" || len(claims.WorkloadRef) > 512 ||
		len(claims.NetworkDigest) != sha256.Size*2 || strings.ToLower(claims.NetworkDigest) != claims.NetworkDigest ||
		validateRuntimeWebAccess(claims.WebAccess) != nil {
		return errors.New("runtime web access grant claims are invalid")
	}
	if _, err := hex.DecodeString(claims.NetworkDigest); err != nil {
		return errors.New("runtime web access grant digest is invalid")
	}
	return nil
}

// RuntimeWebAccessAllowsHost checks a domain rule or unrestricted public-mode host access.
// RuntimeWebAccessAllowsHost проверяет доменное правило или доступ к хосту в полном публичном режиме.
func RuntimeWebAccessAllowsHost(access RuntimeWebAccess, hostname string) bool {
	return runtimeWebAccessRule(access, hostname) != nil || access.Mode == RuntimeWebAccessFullPublic
}

// RuntimeWebAccessAllowsRequest checks the exact HTTP method after managed proxy TLS termination;
// checking only the CONNECT hostname would turn read-only access into an unrestricted tunnel.
// RuntimeWebAccessAllowsRequest проверяет точный HTTP-метод после TLS-терминации управляемым proxy;
// проверка лишь CONNECT-хоста превратила бы режим чтения в неограниченный туннель.
func RuntimeWebAccessAllowsRequest(access RuntimeWebAccess, hostname, method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	if access.Mode == RuntimeWebAccessFullPublic {
		return containsString(runtimeWebAccessMethods(RuntimeWebAccessAllowlistFull), method)
	}
	rule := runtimeWebAccessRule(access, hostname)
	if rule == nil {
		return false
	}
	return containsString(rule.HTTPMethods, method)
}

// runtimeWebAccessRule selects an exact, one-level, or recursive domain rule after normalization.
// runtimeWebAccessRule выбирает точное, одноуровневое или рекурсивное доменное правило после нормализации.
func runtimeWebAccessRule(access RuntimeWebAccess, hostname string) *RuntimeWebAccessRule {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if !validRuntimeDomainPattern(hostname) {
		return nil
	}
	if access.Mode != RuntimeWebAccessAllowlistReadOnly && access.Mode != RuntimeWebAccessAllowlistFull {
		return nil
	}
	for index := range access.Rules {
		rule := &access.Rules[index]
		pattern := rule.DomainPattern
		switch {
		case strings.HasPrefix(pattern, "**."):
			suffix := strings.TrimPrefix(pattern, "**.")
			if hostname == suffix || strings.HasSuffix(hostname, "."+suffix) {
				return rule
			}
		case strings.HasPrefix(pattern, "*."):
			suffix := strings.TrimPrefix(pattern, "*.")
			prefix := strings.TrimSuffix(hostname, "."+suffix)
			if prefix != hostname && prefix != "" && !strings.Contains(prefix, ".") {
				return rule
			}
		case hostname == pattern:
			return rule
		}
	}
	return nil
}
