// Package info provides utility functions for manipulating info lines returned
// by the modem in response to AT commands.
package info

import "strings"

// HasPrefix returns true if the line begins with the info prefix for the command.
func HasPrefix(line, cmd string) bool {
	return strings.HasPrefix(line, cmd+":")
}

// TrimPrefix removes the command  prefix, if any, and any intervening space
// from the info line.
func TrimPrefix(line, cmd string) string {
	return strings.TrimLeft(strings.TrimPrefix(line, cmd+":"), " ")
}
