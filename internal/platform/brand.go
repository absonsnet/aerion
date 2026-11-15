package platform

// Brand constants: the single source of truth for the application name
// and ID. User-visible strings (window titles, dialogs, OAuth labels,
// notification app name, log prefixes) must use AppName instead of a
// hardcoded literal; platform paths use AppID-derived names via paths.go.
const (
	// AppName is the user-visible application name.
	AppName = "Daak"
	// AppID is the application identifier (reverse-DNS).
	AppID = "net.absons.daak"
)
