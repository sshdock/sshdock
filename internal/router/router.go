package router

type Route struct {
	AppID       string
	ServiceName string
	DomainName  string
	Port        int
	HTTPS       bool
}
