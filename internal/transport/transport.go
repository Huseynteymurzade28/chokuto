// Package transport provides a length-prefixed message framing over a TCP
// connection, with optional AES-256-GCM encryption keyed off a room password.
//
// Public rooms (no password) use plain length-prefixed frames. Private rooms
// derive a shared key from the password and encrypt every frame, so passive
// LAN sniffers cannot read traffic and a peer that doesn't know the password
// cannot produce frames the server can authenticate.
package transport

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

// maxFrame caps a single frame to guard against malformed/hostile length
// prefixes. File bodies are chunked well below this.
const maxFrame = 16 << 20 // 16 MiB

// appSalt is a fixed salt for key derivation. A fixed salt is acceptable for
// this LAN-scoped tool (everyone in a room shares the same password); it is not
// meant for use over the public internet.
var appSalt = []byte("chokuto-lan-drop-v1")

// DeriveKey turns a room password into a 32-byte AES-256 key.
func DeriveKey(pass string) ([]byte, error) {
	return scrypt.Key([]byte(pass), appSalt, 1<<15, 8, 1, 32)
}

// Conn wraps a net.Conn with message framing and optional encryption.
type Conn struct {
	raw net.Conn
	r   *bufio.Reader
	gcm cipher.AEAD // nil for public (unencrypted) rooms
	wmu sync.Mutex  // serialises writers
}

// NewConn wraps raw. If key is nil/empty the connection is unencrypted
// (public); otherwise frames are sealed with AES-256-GCM.
func NewConn(raw net.Conn, key []byte) (*Conn, error) {
	c := &Conn{raw: raw, r: bufio.NewReaderSize(raw, 64<<10)}
	if len(key) == 0 {
		return c, nil
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	c.gcm = gcm
	return c, nil
}

// WriteFrame writes one framed message. Safe for concurrent use.
func (c *Conn) WriteFrame(b []byte) error {
	payload := b
	if c.gcm != nil {
		nonce := make([]byte, c.gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		// payload = nonce || ciphertext+tag
		payload = c.gcm.Seal(nonce, nonce, b, nil)
	}
	if len(payload) > maxFrame {
		return fmt.Errorf("frame too large: %d", len(payload))
	}

	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.raw.Write(hdr[:]); err != nil {
		return err
	}
	_, err := c.raw.Write(payload)
	return err
}

// ReadFrame reads one framed message. Not safe for concurrent readers.
func (c *Conn) ReadFrame() ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(c.r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n > maxFrame {
		return nil, fmt.Errorf("frame too large: %d", n)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return nil, err
	}
	if c.gcm == nil {
		return payload, nil
	}
	ns := c.gcm.NonceSize()
	if len(payload) < ns {
		return nil, fmt.Errorf("frame too short")
	}
	nonce, ct := payload[:ns], payload[ns:]
	plain, err := c.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		// On a private room this means the peer used the wrong password.
		return nil, fmt.Errorf("decrypt failed (wrong password?): %w", err)
	}
	return plain, nil
}

// SetReadDeadline sets a deadline on the underlying connection's reads.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.raw.SetReadDeadline(t) }

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.raw.Close() }
