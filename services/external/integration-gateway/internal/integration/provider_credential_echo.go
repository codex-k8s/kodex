package integration

import (
	"bytes"
	"encoding/json"
	"strings"
)

// providerCredentialEcho rejects raw or JSON-string echoes of the exact projected credential and its encoded HTTP authorization payload before a successful body reaches typed projection. It is deliberately not a general secret scanner or a claim about arbitrary encodings.
// providerCredentialEcho отклоняет raw либо JSON-string echo точного projected credential и encoded HTTP authorization payload до передачи успешного body в typed projection. Это не общий поиск секретов и не утверждение о произвольных кодированиях.
func providerCredentialEcho(body, credential []byte, authorization string) bool {
	payload := ""
	if _, value, exists := strings.Cut(authorization, " "); exists {
		payload = value
	}
	contains := func(text []byte) bool {
		return len(credential) > 0 && bytes.Contains(text, credential) || payload != "" && bytes.Contains(text, []byte(payload))
	}
	if contains(body) {
		return true
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		if text, ok := token.(string); ok && contains([]byte(text)) {
			return true
		}
	}
}
