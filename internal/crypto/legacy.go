// Legacy key-derivation input.
//
// TODO: remove this file in a later version, once installs that predate
// the Daak rename have had a chance to upgrade. The pre-rename key
// derivation input is kept here only so Encryptor can still decrypt
// values encrypted by the old build; new encryptions always use the new
// derivation input. No other file may reference the old input.

package crypto

// legacyKeyPrefix is the key-derivation prefix used by pre-rename builds.
// It must stay byte-identical: changing it would make previously
// encrypted credentials undecryptable.
const legacyKeyPrefix = "aerion"

// legacyDeriveKey derives the pre-rename encryption key from a salt, using
// the same machine-specific data as deriveKeyWithPrefix.
func legacyDeriveKey(salt []byte) []byte {
	return deriveKeyWithPrefix(salt, legacyKeyPrefix)
}
