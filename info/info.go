package info

import "strings"

// TrimPrefix removes the command  prefix, if any, and any intervening space
// from the info line.
func TrimPrefix(line, prefix string) string {
	return strings.TrimLeft(strings.TrimPrefix(line, prefix+":"), " ")
}
