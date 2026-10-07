package gateway

import (
	"net/http"
	"strings"
)

const maximumWebSocketExtensionBytes = 256

type webSocketDeflateParameters struct {
	present             bool
	serverNoContext     bool
	serverWindow        uint8
	clientWindowOffered bool
	clientWindow        uint8
}

// Закрытый token-only профиль RFC 7692. Закреплённый Codex 0.160.0 предлагает
// permessage-deflate; client_max_window_bits. Его tungstenite fork поддерживает
// окна 9..15, а не весь диапазон RFC 8..15. Прокси не распаковывает frames.
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

func validWebSocketExtensionOffer(header http.Header) bool {
	_, valid := parseWebSocketDeflate(header, false)
	return valid
}

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
	// RFC 7692 разрешает серверу дополнительно ограничить оба context takeover
	// и server window; client window допустим лишь при соответствующем offer.
	return true
}
