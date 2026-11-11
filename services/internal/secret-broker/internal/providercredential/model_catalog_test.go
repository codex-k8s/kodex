package providercredential

import (
	"net/http"
)

// catalogHTTPFunc adapts an in-memory response callback to the catalog HTTP port, keeping API parser
// tests off the network.
// catalogHTTPFunc преобразует callback ответа в памяти в catalog HTTP port, не допуская сетевых запросов
// тестов API parser.
type catalogHTTPFunc func(*http.Request) (*http.Response, error)

// Do forwards the supplied request directly to the synthetic callback, allowing assertions on URL,
// method, body and authorization without a transport.
// Do передаёт запрос прямо synthetic callback для проверки URL, method, body и authorization без
// транспорта.
func (call catalogHTTPFunc) Do(request *http.Request) (*http.Response, error) { return call(request) }
