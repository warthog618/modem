package serial

import (
	"testing"
)

func TestNew(t *testing.T) {
	// bogus path
	m, err := New("bogusmodem", 115200)
	if err == nil {
		t.Error("New succeeded")
	}
	if m != nil {
		t.Error("New returned unexpected modem")
	}
	// valid path - assumes modem exists
	m, err = New("/dev/gsmmodem", 115200)
	if err != nil {
		t.Error("New failed with", err)
	}
	if m == nil {
		t.Error("New returned nil modem")
	}
	m.Close()
}
