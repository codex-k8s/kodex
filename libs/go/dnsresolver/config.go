package dnsresolver

import "errors"

// ErrInvalidConfig reports a bound violation without exposing resolver names
// or caller-controlled configuration values.
// RU: ErrInvalidConfig сообщает о нарушении границ конфигурации без раскрытия
// имён DNS-серверов или значений, переданных вызывающей стороной.
var ErrInvalidConfig = errors.New("DNS resolver bounds are invalid")

// Config bounds the server-owned DNS work, cache, response size and timeout.
// The list of resolver addresses is supplied separately, never from a request.
// RU: Config ограничивает DNS-запросы, кэш, размер ответа и время ожидания,
// которыми управляет сервер. Адреса DNS-серверов передаются отдельно, не из запроса.
type Config struct {
	MinimumTTLSeconds        int `json:"minimumTTLSeconds"`
	MaximumTTLSeconds        int `json:"maximumTTLSeconds"`
	MaximumCacheEntries      int `json:"maximumCacheEntries"`
	MaximumQueries           int `json:"maximumQueries"`
	MaximumCNAMEDepth        int `json:"maximumCnameDepth"`
	MaximumRecords           int `json:"maximumRecords"`
	MaximumMessageBytes      int `json:"maximumMessageBytes"`
	QueryTimeoutMilliseconds int `json:"queryTimeoutMilliseconds"`
}

// Validate rejects missing, inverted or out-of-range DNS limits before any
// exchanger or cache is constructed; it returns one value-free sentinel.
// RU: Validate отклоняет отсутствующие, перевёрнутые и выходящие за границы
// лимиты DNS до создания обменника или кэша; возвращает один sentinel без значений.
func (c Config) Validate() error {
	if c.MinimumTTLSeconds < 5 || c.MinimumTTLSeconds > 300 ||
		c.MaximumTTLSeconds < c.MinimumTTLSeconds || c.MaximumTTLSeconds > 3600 ||
		c.MaximumCacheEntries < 4 || c.MaximumCacheEntries > 256 ||
		c.MaximumQueries < 2 || c.MaximumQueries > 32 ||
		c.MaximumCNAMEDepth < 1 || c.MaximumCNAMEDepth > 16 ||
		c.MaximumRecords < 4 || c.MaximumRecords > 256 ||
		c.MaximumMessageBytes < 512 || c.MaximumMessageBytes > 64<<10 ||
		c.QueryTimeoutMilliseconds < 100 || c.QueryTimeoutMilliseconds > 10_000 {
		return ErrInvalidConfig
	}
	return nil
}
