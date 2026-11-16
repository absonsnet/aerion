package undo

import "fmt"

// CompositeCommand groups several commands into one undoable action: a
// single Undo press reverses them all, in reverse order. Used by the
// code-chip action (mark read + move to Trash): one toast Undo must both
// move the message back and restore the unread flag.
type CompositeCommand struct {
	BaseCommand
	commands []Command
}

// NewCompositeCommand creates a composite of the given commands, undone in
// reverse order. Empty composites are rejected by the caller, not here.
func NewCompositeCommand(description string, commands ...Command) *CompositeCommand {
	return &CompositeCommand{
		BaseCommand: NewBaseCommand(description),
		commands:    commands,
	}
}

// Execute performs the action (already done at creation time).
func (c *CompositeCommand) Execute() error { return nil }

// Undo reverses every grouped command, last first. It keeps going after a
// failure so a partial undo still restores as much as possible, and
// reports the first error.
func (c *CompositeCommand) Undo() error {
	var firstErr error
	for i := len(c.commands) - 1; i >= 0; i-- {
		if err := c.commands[i].Undo(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("undo step %d (%s): %w", i, c.commands[i].Description(), err)
		}
	}
	return firstErr
}
