package entity

import "time"

type AgentRun struct {
	ID                  int64
	RunID               string
	FlowID              string
	ProfileName         string
	Role                string
	Provider            string
	Owner               string
	Name                string
	BaseBranch          string
	HeadBranch          string
	Status              string
	KubernetesNamespace string
	JobName             string
	PVCName             string
	PRURL               string
	Summary             string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (run AgentRun) FullName() string {
	return run.Owner + "/" + run.Name
}
