package app

import "time"

type Node struct {
	ID        string
	Name      string
	Address   string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type App struct {
	ID           string
	Name         string
	NodeID       string
	RepoPath     string
	WorktreePath string
	ComposePath  string
	Status       AppStatus
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Release struct {
	ID          string
	AppID       string
	CommitSHA   string
	ComposePath string
	Status      ReleaseStatus
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type DeploymentTrigger string

const (
	DeploymentTriggerLegacy   DeploymentTrigger = "legacy"
	DeploymentTriggerPush     DeploymentTrigger = "push"
	DeploymentTriggerRedeploy DeploymentTrigger = "redeploy"
	// Retained only so historical deployment rows remain readable after upgrade.
	DeploymentTriggerRollback        DeploymentTrigger = "rollback"
	DeploymentTriggerStartupRecovery DeploymentTrigger = "startup_recovery"
)

type Deployment struct {
	ID            string
	AppID         string
	ReleaseID     string
	CommitSHA     string
	Trigger       DeploymentTrigger
	Status        DeploymentStatus
	StartedAt     time.Time
	FinishedAt    time.Time
	FailureStage  string
	FailureDetail string
	RetryGuidance string
	ErrorMessage  string
}

type Domain struct {
	ID          string
	AppID       string
	ServiceName string
	DomainName  string
	Port        int
	HTTPS       bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Event struct {
	ID        string
	AppID     string
	Type      string
	Message   string
	CreatedAt time.Time
}
