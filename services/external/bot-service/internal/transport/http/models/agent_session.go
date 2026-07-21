package models

// AgentSessionCompleteResponse подтверждает приём атомарного terminal/archive перехода.
type AgentSessionCompleteResponse struct {
	Status string `json:"status"`
}
