package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func webSocketExtensions(values ...string) http.Header {
	if len(values) == 0 {
		return http.Header{}
	}
	return http.Header{"Sec-Websocket-Extensions": values}
}

func TestProviderWebSocketExtensionOfferClosedPinnedProfile(t *testing.T) {
	for _, value := range []string{
		"permessage-deflate",
		"permessage-deflate; client_max_window_bits",
		"permessage-deflate; server_no_context_takeover; client_no_context_takeover; server_max_window_bits=9; client_max_window_bits=15",
		" \tpermessage-deflate \t; client_max_window_bits \t=\t 12 ",
		"permessage-deflate" + strings.Repeat(" ", maximumWebSocketExtensionBytes-len("permessage-deflate")),
	} {
		if !validWebSocketExtensionOffer(webSocketExtensions(value)) {
			t.Fatal("valid closed extension offer rejected")
		}
	}
	if !validWebSocketExtensionOffer(nil) {
		t.Fatal("absent extension offer rejected")
	}
	for _, value := range []string{
		"", " ", "foreign-extension", "PERMESSAGE-DEFLATE", "permessage-deflate, permessage-deflate", "permessage-deflate, foreign-extension",
		"permessage-deflate;", "permessage-deflate;; client_max_window_bits", "permessage-deflate; private_parameter=private-sentinel",
		"permessage-deflate; client_max_window_bits; client_max_window_bits", "permessage-deflate; server_no_context_takeover; server_no_context_takeover",
		"permessage-deflate; client_no_context_takeover=true", "permessage-deflate; server_no_context_takeover=", "permessage-deflate; server_max_window_bits",
		"permessage-deflate; client_max_window_bits=", "permessage-deflate; client_max_window_bits=8", "permessage-deflate; server_max_window_bits=16",
		"permessage-deflate; server_max_window_bits=09", "permessage-deflate; client_max_window_bits=+9", "permessage-deflate; server_max_window_bits=9.0",
		"permessage-deflate; client_max_window_bits=\"15\"", "permessage-deflate; client_max_window_bits=15=15",
		"permessage-deflate\r\nX-Private: sentinel", "permessage-deflate\x00", "permessage-deflate\u00a0",
		"permessage-deflate" + strings.Repeat(" ", maximumWebSocketExtensionBytes+1-len("permessage-deflate")),
	} {
		if validWebSocketExtensionOffer(webSocketExtensions(value)) {
			t.Fatal("invalid extension offer accepted")
		}
	}
	if validWebSocketExtensionOffer(webSocketExtensions("permessage-deflate", "permessage-deflate")) {
		t.Fatal("duplicate extension headers accepted")
	}
}

func TestProviderWebSocketExtensionResponseBoundToOffer(t *testing.T) {
	for _, test := range []struct {
		name, offer, answer string
		valid               bool
	}{
		{"declined", "permessage-deflate; client_max_window_bits", "", true},
		{"minimal", "permessage-deflate; client_max_window_bits", "permessage-deflate", true},
		{"defaultWindow", "permessage-deflate; client_max_window_bits", "permessage-deflate; client_max_window_bits=15", true},
		{"serverAddsKnownLimits", "permessage-deflate; client_max_window_bits", "permessage-deflate; client_max_window_bits=12; server_max_window_bits=9; server_no_context_takeover; client_no_context_takeover", true},
		{"serverTakeoverHonored", "permessage-deflate; server_no_context_takeover", "permessage-deflate; server_no_context_takeover", true},
		{"clientTakeoverHint", "permessage-deflate; client_no_context_takeover", "permessage-deflate", true},
		{"windowSubset", "permessage-deflate; server_max_window_bits=12; client_max_window_bits=13", "permessage-deflate; server_max_window_bits=10; client_max_window_bits=12", true},
		{"clientWindowIgnored", "permessage-deflate; client_max_window_bits=12", "permessage-deflate", true},
		{"unsolicited", "", "permessage-deflate", false},
		{"unofferedClientWindow", "permessage-deflate", "permessage-deflate; client_max_window_bits=15", false},
		{"bareResponseWindow", "permessage-deflate; client_max_window_bits", "permessage-deflate; client_max_window_bits", false},
		{"serverTakeoverMissing", "permessage-deflate; server_no_context_takeover", "permessage-deflate", false},
		{"serverWindowMissing", "permessage-deflate; server_max_window_bits=12", "permessage-deflate", false},
		{"serverWindowIncreased", "permessage-deflate; server_max_window_bits=12", "permessage-deflate; server_max_window_bits=13", false},
		{"clientWindowIncreased", "permessage-deflate; client_max_window_bits=12", "permessage-deflate; client_max_window_bits=13", false},
		{"unsupportedWindow", "permessage-deflate; client_max_window_bits", "permessage-deflate; server_max_window_bits=8", false},
		{"unknownParameter", "permessage-deflate; client_max_window_bits", "permessage-deflate; private_parameter=private-sentinel", false},
		{"duplicateParameter", "permessage-deflate; client_max_window_bits", "permessage-deflate; client_max_window_bits=12; client_max_window_bits=12", false},
		{"multipleExtensions", "permessage-deflate", "permessage-deflate, foreign-extension", false},
		{"invalidOfferDeclined", "foreign-extension", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			offer, answer := webSocketExtensions(), webSocketExtensions()
			if test.offer != "" {
				offer = webSocketExtensions(test.offer)
			}
			if test.answer != "" {
				answer = webSocketExtensions(test.answer)
			}
			if got := validWebSocketExtensionResponse(answer, offer); got != test.valid {
				t.Fatalf("negotiation valid=%t want=%t", got, test.valid)
			}
		})
	}
	if !validWebSocketExtensionResponse(nil, nil) {
		t.Fatal("uncompressed negotiation rejected")
	}
	offer := webSocketExtensions("permessage-deflate; client_max_window_bits")
	if validWebSocketExtensionResponse(webSocketExtensions("permessage-deflate", "permessage-deflate"), offer) || validWebSocketExtensionResponse(webSocketExtensions(""), offer) {
		t.Fatal("duplicate or empty response extension header accepted")
	}
	for bits := 9; bits <= 15; bits++ {
		value := "permessage-deflate; client_max_window_bits=" + strconv.Itoa(bits)
		if !validWebSocketExtensionResponse(webSocketExtensions(value), offer) {
			t.Fatal("supported window bits rejected")
		}
	}
}
