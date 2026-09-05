package dnsresolver

import "errors"

// ErrInvalidConfig не содержит имён resolver или caller input.
var ErrInvalidConfig = errors.New("DNS resolver bounds are invalid")

// Config ограничивает server-owned DNS, не назначая resolver addresses из payload.
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
