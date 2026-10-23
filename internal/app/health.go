package app

type HealthReport struct {
	AppName                        string            `json:"app_name"`
	Health                         string            `json:"health"`
	Status                         AppStatus         `json:"status"`
	NodeID                         string            `json:"node_id"`
	CurrentMainCommit              string            `json:"current_main_commit"`
	AttemptReleaseID               string            `json:"attempt_release_id"`
	AttemptReleaseStatus           ReleaseStatus     `json:"attempt_release_status"`
	LatestDeploymentID             string            `json:"latest_deployment_id"`
	LatestDeploymentCommit         string            `json:"latest_deployment_commit"`
	LatestDeploymentTrigger        DeploymentTrigger `json:"latest_deployment_trigger"`
	LatestDeploymentStatus         DeploymentStatus  `json:"latest_deployment_status"`
	LastSuccessfulDeploymentID     string            `json:"last_successful_deployment_id"`
	LastSuccessfulDeploymentCommit string            `json:"last_successful_deployment_commit"`
	LastSuccessfulReleaseID        string            `json:"last_successful_release_id"`
	DomainCount                    int               `json:"domain_count"`
	ActiveRouteCount               int               `json:"active_route_count"`
	RouteAttentionCount            int               `json:"route_attention_count"`
	RouteStatus                    string            `json:"route_status"`
	ServiceCount                   int               `json:"service_count"`
	RunningServiceCount            int               `json:"running_service_count"`
	AttentionServiceCount          int               `json:"attention_service_count"`
	Services                       []ServiceHealth   `json:"services"`
	LastFailureDeploymentID        string            `json:"last_failure_deployment_id"`
	LastFailure                    string            `json:"last_failure"`
	Checks                         []HealthCheck     `json:"checks"`
}

type ServiceHealth struct {
	Name  string `json:"name"`
	State string `json:"state"`
}

type HealthCheck struct {
	Status string `json:"status"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}
