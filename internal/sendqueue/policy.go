package sendqueue

import (
	"strings"
	"time"
)

// Backoff returns the delay before the next attempt after
// `attempts` failed attempts: 1m, 2m, 4m, 8m, 16m, capped at 1h.
// The undo window itself is not a retry: the first dispatch happens
// at send time, and only a failed dispatch enters backoff.
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := time.Duration(1<<uint(attempts-1)) * time.Minute
	if d > time.Hour {
		return time.Hour
	}
	return d
}

// permanentMarkers are substrings (matched case-insensitively)
// that mark a send error as permanent: retrying will never help,
// so the item fails immediately instead of burning the attempt
// budget on a hopeless retry loop.
//
//   - Authentication failures (wrong password, revoked OAuth, 535):
//     the user must fix credentials and retry by hand.
//   - Receive-only accounts and missing recipients: rejected at
//     queue time, but the dispatcher re-checks — a stale queue row
//     must not retry forever.
//   - Message-build failures: the stored bytes are fixed, rebuilding
//     cannot succeed.
var permanentMarkers = []string{
	"authentication failed",
	"failed to login",
	"login failed",
	"535",
	"530",
	"unauthorized",
	"receive-only",
	"no recipients",
	"failed to build message",
	"account not found",
}

// IsPermanentError reports whether a dispatch error should fail the
// item at once instead of retrying with backoff. Everything else —
// connection resets, timeouts, DNS failures, 4xx greylisting, server
// busy — is transient and retried.
func IsPermanentError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, m := range permanentMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
