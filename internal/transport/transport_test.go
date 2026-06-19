package transport

import (
	"bytes"
	"net"
	"testing"
)

func roundTrip(t *testing.T, clientKey, serverKey []byte, payload []byte) ([]byte, error) {
	t.Helper()
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()

	sender, err := NewConn(c1, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	receiver, err := NewConn(c2, serverKey)
	if err != nil {
		t.Fatal(err)
	}

	errc := make(chan error, 1)
	go func() { errc <- sender.WriteFrame(payload) }()

	got, rerr := receiver.ReadFrame()
	if werr := <-errc; werr != nil {
		return nil, werr
	}
	return got, rerr
}

func TestPublicRoundTrip(t *testing.T) {
	payload := []byte("plain frame with : and \n bytes")
	got, err := roundTrip(t, nil, nil, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q want %q", got, payload)
	}
}

func TestPrivateRoundTrip(t *testing.T) {
	key, _ := DeriveKey("s3cret")
	payload := []byte("encrypted frame \x00\x01\xff")
	got, err := roundTrip(t, key, key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q want %q", got, payload)
	}
}

func TestWrongPasswordFails(t *testing.T) {
	good, _ := DeriveKey("correct")
	bad, _ := DeriveKey("wrong")
	if _, err := roundTrip(t, good, bad, []byte("hi")); err == nil {
		t.Fatal("expected decrypt failure with mismatched keys")
	}
}
