package app

type AppStatus string

const (
	AppStatusCreated   AppStatus = "created"
	AppStatusDeploying AppStatus = "deploying"
	AppStatusHealthy   AppStatus = "healthy"
	AppStatusFailed    AppStatus = "failed"
	AppStatusUnknown   AppStatus = "unknown"
)

type ReleaseStatus string

const (
	ReleaseStatusPending    ReleaseStatus = "pending"
	ReleaseStatusDeploying  ReleaseStatus = "deploying"
	ReleaseStatusSucceeded  ReleaseStatus = "succeeded"
	ReleaseStatusFailed     ReleaseStatus = "failed"
	ReleaseStatusRolledBack ReleaseStatus = "rolled_back"
)

type DeploymentStatus string

const (
	DeploymentStatusPending   DeploymentStatus = "pending"
	DeploymentStatusDeploying DeploymentStatus = "deploying"
	DeploymentStatusSucceeded DeploymentStatus = "succeeded"
	DeploymentStatusFailed    DeploymentStatus = "failed"
)
