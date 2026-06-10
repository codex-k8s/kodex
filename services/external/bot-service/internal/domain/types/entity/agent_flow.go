package entity

import "time"

type AgentFlow struct {
	ID                    int64
	FlowID                string
	Status                string
	Provider              string
	Owner                 string
	Name                  string
	BaseBranch            string
	HeadBranch            string
	Title                 string
	Task                  string
	PRURL                 string
	PRNumber              int
	Attempt               int
	MaxAttempts           int
	CurrentDeveloperRunID string
	CurrentReviewerRunID  string
	OwnerUserID           string
	OwnerUser             string
	ControlChannelID      string
	ControlPostID         string
	ActionToken           string
	OwnerDecision         string
	Summary               string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (flow AgentFlow) FullName() string {
	return flow.Owner + "/" + flow.Name
}
