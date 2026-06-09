package models

type GitHubWebhookResponse struct {
	Status string `json:"status"`
	Event  string `json:"event"`
}
