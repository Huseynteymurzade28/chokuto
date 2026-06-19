package protocol

import (
	"bytes"
	"testing"
)

func TestMessageRoundTrip(t *testing.T) {
	cases := []Message{
		{Type: TypeMessage, From: "alice", Body: "hello world"},
		{Type: TypeMessage, From: "bob", To: "alice", Body: "secret: with colons"},
		{Type: TypeJoin, From: "carol", Body: "carol joined"},
	}
	for _, want := range cases {
		got, err := Decode(want.Encode())
		if err != nil {
			t.Fatalf("Decode(%q) error: %v", want.Encode(), err)
		}
		if got != want {
			t.Errorf("round trip mismatch: got %+v want %+v", got, want)
		}
	}
}

func TestFileHeaderRoundTrip(t *testing.T) {
	want := FileHeader{From: "alice", To: "bob", ID: "alice-123", Size: 4096, Filename: "weird: name.txt"}
	got, err := DecodeFileHeader(want.Encode())
	if err != nil {
		t.Fatalf("DecodeFileHeader error: %v", err)
	}
	if got != want {
		t.Errorf("got %+v want %+v", got, want)
	}
}

func TestChunkRoundTrip(t *testing.T) {
	data := []byte{0, 1, 2, ':', '\n', 255}
	frame := EncodeChunk("xfer-1", data)
	if !IsChunk(frame) {
		t.Fatal("IsChunk = false for a chunk frame")
	}
	id, got, ok := DecodeChunk(frame)
	if !ok || id != "xfer-1" || !bytes.Equal(got, data) {
		t.Errorf("DecodeChunk = (%q, %v, %v)", id, got, ok)
	}
}

func TestAuth(t *testing.T) {
	if name, ok := DecodeAuth(EncodeAuth("alice")); !ok || name != "alice" {
		t.Errorf("DecodeAuth = (%q, %v)", name, ok)
	}
	if _, ok := DecodeAuth("MSG:alice::hi"); ok {
		t.Error("DecodeAuth accepted a non-auth frame")
	}
}
