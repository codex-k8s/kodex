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
	runtimeWebAccessGrantVersion = 1
	maximumWebAccessGrantBytes   = 16 << 10
)

// RuntimeWebAccessGrant связывает доступ proxy с exact execution lease и
// immutable digest сетевой policy. Значение переносится только в
// Proxy-Authorization и не должно попадать в логи или пользовательские events.
type RuntimeWebAccessGrant struct {
	Version       int              `json:"v"`
	Audience      string           `json:"aud"`
	WorkloadRef   string           `json:"workload_ref"`
	NetworkDigest string           `json:"network_digest"`
	WebAccess     RuntimeWebAccess `json:"web_access"`
}

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

func RuntimeWebAccessAllowsHost(access RuntimeWebAccess, hostname string) bool {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if !validRuntimeDomainPattern(hostname) {
		return false
	}
	if access.Mode == RuntimeWebAccessFullPublic {
		return true
	}
	if access.Mode != RuntimeWebAccessAllowlistReadOnly && access.Mode != RuntimeWebAccessAllowlistFull {
		return false
	}
	for _, rule := range access.Rules {
		pattern := rule.DomainPattern
		switch {
		case strings.HasPrefix(pattern, "**."):
			suffix := strings.TrimPrefix(pattern, "**.")
			if hostname != suffix && strings.HasSuffix(hostname, "."+suffix) {
				return true
			}
		case strings.HasPrefix(pattern, "*."):
			suffix := strings.TrimPrefix(pattern, "*.")
			prefix := strings.TrimSuffix(hostname, "."+suffix)
			if prefix != hostname && prefix != "" && !strings.Contains(prefix, ".") {
				return true
			}
		case hostname == pattern:
			return true
		}
	}
	return false
}
