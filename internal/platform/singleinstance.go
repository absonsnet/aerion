package platform

// SingleInstanceLock ensures only one instance of the application runs at a time.
// When a second instance is launched, it signals the first to show its window.
type SingleInstanceLock interface {
	// TryLock attempts to acquire the single-instance lock.
	// activateMsg is the command to send to the existing instance (e.g. "show" or "mailto:...").
	// Returns locked=true if this is the first instance.
	// Returns locked=false if an existing instance was activated.
	TryLock(activateMsg string) (locked bool, err error)

	// SetOnShow sets the callback invoked when a second instance sends a command.
	// The data parameter is the command string ("show" or "mailto:...").
	// Register it as early as possible — the lock starts accepting the
	// moment TryLock succeeds, so a command arriving before the callback
	// is set is dropped. The callback itself must be safe to invoke
	// before the app context exists (remember the command and honour it
	// once startup completes); it must not assume stores are initialised.
	SetOnShow(fn func(data string))

	// Unlock releases the lock and cleans up resources.
	Unlock()
}
