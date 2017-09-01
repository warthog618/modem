package info

import "testing"

func TestHasPrefix(t *testing.T) {
	l := "cmd: blah"
	// Has
	if !HasPrefix(l, "cmd") {
		t.Error("didn't find prefix")
	}
	// Hasn't
	if HasPrefix(l, "cmd:") {
		t.Error("found prefix")
	}
}

func TestTrimPrefix(t *testing.T) {
	// no prefix
	i := TrimPrefix("info line", "cmd")
	if i != "info line" {
		t.Errorf("expected trimmed line 'info line' but got '%s'", i)
	}
	// prefix
	i = TrimPrefix("cmd:info line", "cmd")
	if i != "info line" {
		t.Errorf("expected trimmed line 'info line' but got '%s'", i)
	}

	// prefix and space
	i = TrimPrefix("cmd: info line", "cmd")
	if i != "info line" {
		t.Errorf("expected trimmed line 'info line' but got '%s'", i)
	}
}
