package gateway

import (
	"net/http"
	"strings"
)

// maximumWebSocketExtensionBytes caps the complete extension header accepted by the pinned provider profile.
// maximumWebSocketExtensionBytes ограничивает весь заголовок расширений закреплённого профиля провайдера.
const maximumWebSocketExtensionBytes = 256

// webSocketDeflateParameters records the bounded offer or response parameters needed for negotiation.
// webSocketDeflateParameters хранит ограниченные параметры offer или response для согласования.
type webSocketDeflateParameters struct {
	present             bool
	serverNoContext     bool
	serverWindow        uint8
	clientWindowOffered bool
	clientWindow        uint8
}

// parseWebSocketDeflate accepts only the pinned RFC 7692 token profile without decompressing frames.
// parseWebSocketDeflate принимает только закреплённый профиль токенов RFC 7692 без распаковки кадров.
// The pinned Codex 0.160.0 fork offers client_max_window_bits and supports window sizes 9..15.
// Закреплённый fork Codex 0.160.0 предлагает client_max_window_bits и поддерживает размеры окна 9..15.
func parseWebSocketDeflate(header http.Header, response bool) (webSocketDeflateParameters, bool) {
	var parameters webSocketDeflateParameters
	values := header.Values("Sec-WebSocket-Extensions")
	if len(values) == 0 {
		return parameters, true
	}
	if len(values) != 1 || len(values[0]) == 0 || len(values[0]) > maximumWebSocketExtensionBytes {
		return parameters, false
	}
	value := values[0]
	for index := range len(value) {
		if value[index] != '\t' && (value[index] < 32 || value[index] > 126) {
			return parameters, false
		}
	}
	parts := strings.Split(value, ";")
	if len(parts) > 5 || strings.Trim(parts[0], " \t") != "permessage-deflate" {
		return parameters, false
	}
	parameters.present = true
	var seen uint8
	for _, part := range parts[1:] {
		name, value, hasValue := strings.Cut(strings.Trim(part, " \t"), "=")
		name, value = strings.Trim(name, " \t"), strings.Trim(value, " \t")
		var flag uint8
		switch name {
		case "server_no_context_takeover":
			flag = 1
			if hasValue {
				return parameters, false
			}
			parameters.serverNoContext = true
		case "client_no_context_takeover":
			flag = 2
			if hasValue {
				return parameters, false
			}
		case "server_max_window_bits":
			flag = 4
			parameters.serverWindow = webSocketDeflateWindow(value)
			if !hasValue || parameters.serverWindow == 0 {
				return parameters, false
			}
		case "client_max_window_bits":
			flag = 8
			parameters.clientWindowOffered = true
			parameters.clientWindow = webSocketDeflateWindow(value)
			if hasValue && parameters.clientWindow == 0 || response && !hasValue {
				return parameters, false
			}
		default:
			return parameters, false
		}
		if seen&flag != 0 {
			return parameters, false
		}
		seen |= flag
	}
	return parameters, true
}

// webSocketDeflateWindow maps only the pinned supported decimal window sizes to numeric values.
// webSocketDeflateWindow преобразует только поддерживаемые десятичные размеры окна в числа.
func webSocketDeflateWindow(value string) uint8 {
	switch value {
	case "9":
		return 9
	case "10":
		return 10
	case "11":
		return 11
	case "12":
		return 12
	case "13":
		return 13
	case "14":
		return 14
	case "15":
		return 15
	default:
		return 0
	}
}

// validWebSocketExtensionOffer rejects unsolicited or malformed client extension offers.
// validWebSocketExtensionOffer отклоняет неожиданные и неверно оформленные предложения расширений клиента.
func validWebSocketExtensionOffer(header http.Header) bool {
	_, valid := parseWebSocketDeflate(header, false)
	return valid
}

// validWebSocketExtensionResponse requires a server answer within the client's offered deflate limits.
// validWebSocketExtensionResponse требует, чтобы ответ сервера не превышал предложенные клиентом пределы deflate.
func validWebSocketExtensionResponse(response, request http.Header) bool {
	offer, validOffer := parseWebSocketDeflate(request, false)
	answer, validAnswer := parseWebSocketDeflate(response, true)
	if !validOffer || !validAnswer {
		return false
	}
	if !answer.present {
		return true
	}
	if !offer.present || offer.serverNoContext && !answer.serverNoContext {
		return false
	}
	if offer.serverWindow != 0 && (answer.serverWindow == 0 || answer.serverWindow > offer.serverWindow) {
		return false
	}
	if answer.clientWindowOffered && (!offer.clientWindowOffered || offer.clientWindow != 0 && answer.clientWindow > offer.clientWindow) {
		return false
	}
	// RFC 7692 lets the server narrow context takeover and its window, but the client window needs an offer.
	// RFC 7692 разрешает серверу сузить context takeover и своё окно, а окно клиента требует соответствующего offer.
	return true
}
