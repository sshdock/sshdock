package cli

import "io"

type Backend interface {
	CreateApp(name string) (App, string, error)
	ListApps() ([]App, error)
	GetApp(name string) (App, error)
	StartApp(name string) error
	StopApp(name string) error
	RestartApp(name string) error
	RestartService(appName string, serviceName string) error
	RedeployApp(name string) error
	RollbackApp(name string, releaseID string) error
	RemoveApp(name string) error
	AppHealth(name string) (AppHealth, error)
	Logs(request LogRequest, stdout io.Writer, stderr io.Writer) error
	ListReleases(appName string) ([]Release, error)
	ListDeployments(appName string) ([]Deployment, error)
	ListEvents(appName string) ([]Event, error)
	ListDomains(appName string) ([]Domain, error)
	CheckDomains(appName string) ([]DomainCheck, error)
	AttachDomain(domain Domain) error
	DetachDomain(appName string, domainName string) error
	SetServerGitHost(host string) error
	AddSSHKey(name string, publicKey string) error
	ListSSHKeys() ([]SSHKey, error)
	RemoveSSHKey(name string) error
	SetConfig(appName string, name string, value []byte) error
	ImportConfig(appName string, input io.Reader) (int, error)
	ListConfig(appName string) ([]ConfigEntry, error)
	GetConfig(appName string, name string) (string, error)
	UnsetConfig(appName string, name string) error
}
