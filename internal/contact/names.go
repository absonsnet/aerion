// Package contact — display-name helpers shared by the provider write paths.
//
// SplitDisplayName splits a display name into given and family names on the
// last space ("Mary Jane Watson" -> "Mary Jane" / "Watson"; "Madonna" ->
// "Madonna" / ""). Providers that take a structured name (Google People,
// vCard N) use both parts; providers that take a plain display name
// (Microsoft Graph) use the original string untouched.
package contact

import "strings"

// SplitDisplayName splits a display name into given and family names.
func SplitDisplayName(displayName string) (given, family string) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return "", ""
	}
	if idx := strings.LastIndex(displayName, " "); idx > 0 {
		return strings.TrimSpace(displayName[:idx]), strings.TrimSpace(displayName[idx+1:])
	}
	return displayName, ""
}
