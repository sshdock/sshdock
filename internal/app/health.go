package app

type HealthReport struct {
	AppName                 string
	Health                  string
	Status                  AppStatus
	NodeID                  string
	CurrentMainCommit       string
	LatestReleaseID         string
	LatestReleaseStatus     ReleaseStatus
	LatestDeploymentID      string
	LatestDeploymentCommit  string
	LatestDeploymentTrigger DeploymentTrigger
	LatestDeploymentStatus  DeploymentStatus
	DomainCount             int
	ActiveRouteCount        int
	RouteAttentionCount     int
	RouteStatus             string
	ServiceCount            int
	RunningServiceCount     int
	AttentionServiceCount   int
	Services                []ServiceHealth
	LastFailureDeploymentID string
	LastFailure             string
	Checks                  []HealthCheck
}

type ServiceHealth struct {
	Name  string
	State string
}

type HealthCheck struct {
	Status string
	Name   string
	Detail string
}
