package callerscope

type CallerScope string

var (
	// ServerCaller indicates the token request is originating from server-side code.
	ServerCaller CallerScope = "server"
	// RequesterCaller indicates the token request is originating from a request handler or similar context.
	RequesterCaller CallerScope = "requester"
)
