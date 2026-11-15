// Legacy keyring service name.
//
// TODO: remove this file in a later version, once installs that predate
// the Daak rename have had a chance to upgrade. The pre-rename OS-keyring
// service name is kept here only so existing entries keep working: reads
// fall back to the old service name and copy the entry to the new one.
// No other file may reference the old service name.

package keyring

import (
	gokeyring "github.com/zalando/go-keyring"
)

// legacyServiceName is the OS-keyring service name used by pre-rename
// builds. The OS keyring has no list/enumerate API, so entries are moved
// lazily: keyringGet falls back to this service when the new one has no
// entry, then copies the value to the new service and deletes the old one.
const legacyServiceName = "aerion"

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
