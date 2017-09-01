package trace

import (
	"bytes"
	"log"
	"testing"
)

func TestNew(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", log.LstdFlags)
	// vanilla
	tr := New(mrw, l)
	if tr == nil {
		t.Error("new failed")
	}
	// with opts
	tr = New(mrw, l, ReadFormat("r: %v"))
	if tr == nil {
		t.Error("new failed")
	}
}

func TestRead(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := New(mrw, l)
	if tr == nil {
		t.Error("new failed")
	}
	i := make([]byte, 10)
	n, err := tr.Read(i)
	if err != nil {
		t.Error("unexpected error:", err)
	}
	if n != 3 {
		t.Error("unexpected length:", n)
	}
	if bytes.Compare(b.Bytes(), []byte("r: one\n")) != 0 {
		t.Errorf("unexpected log: '%s'", b.Bytes())
	}
}

func TestWrite(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := New(mrw, l)
	if tr == nil {
		t.Error("new failed")
	}
	n, err := tr.Write([]byte("two"))
	if err != nil {
		t.Error("unexpected error:", err)
	}
	if n != 3 {
		t.Error("unexpected length:", n)
	}
	if bytes.Compare(b.Bytes(), []byte("w: two\n")) != 0 {
		t.Errorf("unexpected log: '%s'", b.Bytes())
	}
}

func TestReadFormat(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := New(mrw, l, ReadFormat("R: %v"))
	if tr == nil {
		t.Error("new failed")
	}
	i := make([]byte, 10)
	n, err := tr.Read(i)
	if err != nil {
		t.Error("unexpected error:", err)
	}
	if n != 3 {
		t.Error("unexpected length:", n)
	}
	if bytes.Compare(b.Bytes(), []byte("R: [111 110 101]\n")) != 0 {
		t.Errorf("unexpected log: '%s'", b.Bytes())
	}
}

func TestWriteFormat(t *testing.T) {
	mrw := bytes.NewBufferString("one")
	b := bytes.Buffer{}
	l := log.New(&b, "", 0)
	tr := New(mrw, l, WriteFormat("W: %v"))
	if tr == nil {
		t.Error("new failed")
	}
	n, err := tr.Write([]byte("two"))
	if err != nil {
		t.Error("unexpected error:", err)
	}
	if n != 3 {
		t.Error("unexpected length:", n)
	}
	if bytes.Compare(b.Bytes(), []byte("W: [116 119 111]\n")) != 0 {
		t.Errorf("unexpected log: '%s'", b.Bytes())
	}
}
