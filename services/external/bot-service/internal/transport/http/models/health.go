package models

type HealthResponse struct {
	Status               string   `json:"status"`
	Service              string   `json:"service"`
	Version              string   `json:"version"`
	MattermostConfigured bool     `json:"mattermost_configured"`
	BotTokenConfigured   bool     `json:"bot_token_configured"`
	SlashTokenConfigured bool     `json:"slash_token_configured"`
	DefaultTeam          string   `json:"default_team"`
	DefaultChannels      []string `json:"default_channels"`
}

type ReadyResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
