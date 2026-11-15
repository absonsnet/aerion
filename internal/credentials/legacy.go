// Legacy keyring service name and OAuth choice IDs.
//
// TODO: remove this file in a later version, once installs that predate
// the Daak rename have had a chance to upgrade. The pre-rename OS-keyring
// service name and stored OAuth choice values are kept here only so
// existing entries keep working: reads fall back to the old service name
// and copy the entry to the new one, and stored old choice values are
// translated to the new ones on read. No other file may reference the old
// service name or old choice values.

package credentials

import (
	gokeyring "github.com/zalando/go-keyring"
)

// legacyServiceName is the OS-keyring service name used by pre-rename
// builds. The OS keyring has no list/enumerate API, so entries are moved
// lazily: keyringGet falls back to this service when the new one has no
// entry, then copies the value to the new service and deletes the old one.
const legacyServiceName = "aerion"

// legacyOAuthChoiceShipped / legacyOAuthChoiceMail are the OAuth picker
// choice IDs stored by pre-rename builds in the settings table
// (oauth_active_choice:<slot>).
const (
	legacyOAuthChoiceShipped = "aerion-shipped"
	legacyOAuthChoiceMail    = "aerion-mail"
)

// normalizeOAuthChoice translates a stored pre-rename choice ID to its
// new equivalent. Unknown values (including "custom" and the new IDs)
// pass through unchanged.
func normalizeOAuthChoice(choice string) string {
	switch choice {
	case legacyOAuthChoiceShipped:
		return "daak-shipped"
	case legacyOAuthChoiceMail:
		return "daak-mail"
	default:
		return choice
	}
}

// keyringGet reads a value from the OS keyring under the new service name,
// falling back to the legacy service name. A value found under the legacy
// name is copied to the new service and the legacy entry deleted, so the
// move happens once, on first read.
func keyringGet(user string) (string, error) {
	value, err := gokeyring.Get(serviceName, user)
	if err == nil {
		return value, nil
	}
	if err != gokeyring.ErrNotFound {
		return "", err
	}
	value, err = gokeyring.Get(legacyServiceName, user)
	if err != nil {
		return "", err
	}
	if err := gokeyring.Set(serviceName, user, value); err == nil {
		_ = gokeyring.Delete(legacyServiceName, user)
	}
	return value, nil
}

// keyringSet writes a value to the OS keyring under the new service name.
func keyringSet(user, value string) error {
	return gokeyring.Set(serviceName, user, value)
}

// keyringDelete removes a value from the OS keyring under both the new and
// the legacy service names, so a not-yet-migrated entry cannot linger.
func keyringDelete(user string) error {
	_ = gokeyring.Delete(legacyServiceName, user)
	return gokeyring.Delete(serviceName, user)
}
