package serial

import (
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	if _, err := os.Stat("/dev/ttyUSB0"); os.IsNotExist(err) {
		t.Skip("no modem available")
	}
	m, err := New("/dev/ttyUSB0", 115200)
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	if m == nil {
		t.Fatal("New returned nil modem")
	}
	m.Close()
}

func TestNewFail(t *testing.T) {
	// bogus path
	m, err := New("bogusmodem", 115200)
	if err == nil {
		t.Error("New succeeded")
	}
	if m != nil {
		t.Error("New returned unexpected modem")
	}
}
