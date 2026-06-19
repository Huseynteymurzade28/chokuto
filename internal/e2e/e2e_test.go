package e2e

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lan-drop/internal/discovery"
	"lan-drop/internal/protocol"
	"lan-drop/internal/server"
	"lan-drop/internal/transport"
)

var serverBin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "chokuto-e2e")
	if err != nil {
		panic(err)
	}
	serverBin = filepath.Join(dir, "server")
	build := exec.Command("go", "build", "-o", serverBin, "../../cmd/server")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		panic("build server: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// startServer launches the server binary on a free port and returns its addr.
func startServer(t *testing.T, pass string, port int) string {
	t.Helper()
	args := []string{"--port", fmt.Sprint(port), "--name", "e2e"}
	if pass != "" {
		args = append(args, "--pass", pass)
	}
	cmd := exec.Command(serverBin, args...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// Wait for the listener.
	for i := 0; i < 50; i++ {
		if c, err := net.Dial("tcp", addr); err == nil {
			c.Close()
			return addr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not come up")
	return ""
}

func connect(t *testing.T, addr, pass, username string) *transport.Conn {
	t.Helper()
	var key []byte
	if pass != "" {
		key, _ = transport.DeriveKey(pass)
	}
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn, err := transport.NewConn(raw, key)
	if err != nil {
		t.Fatalf("new conn: %v", err)
	}
	if err := conn.WriteFrame([]byte(protocol.EncodeAuth(username))); err != nil {
		t.Fatalf("auth: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readMessage drains control frames until a chat MSG arrives.
func readMessage(t *testing.T, conn *transport.Conn) protocol.Message {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	defer conn.SetReadDeadline(time.Time{})
	for {
		frame, err := conn.ReadFrame()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if protocol.IsChunk(frame) || strings.HasPrefix(string(frame), "FILE:") {
			continue
		}
		msg, err := protocol.Decode(string(frame))
		if err != nil {
			continue
		}
		if msg.Type == protocol.TypeMessage {
			return msg
		}
	}
}

func TestBroadcastAndDM(t *testing.T) {
	addr := startServer(t, "", 18111)
	alice := connect(t, addr, "", "alice")
	bob := connect(t, addr, "", "bob")
	carol := connect(t, addr, "", "carol")
	time.Sleep(100 * time.Millisecond)

	// Broadcast from alice reaches bob.
	alice.WriteFrame([]byte(protocol.Message{Type: protocol.TypeMessage, From: "alice", Body: "hi all"}.Encode()))
	if got := readMessage(t, bob); got.Body != "hi all" || got.To != "" {
		t.Fatalf("broadcast: got %+v", got)
	}

	// DM from alice to bob reaches only bob.
	alice.WriteFrame([]byte(protocol.Message{Type: protocol.TypeMessage, From: "alice", To: "bob", Body: "psst"}.Encode()))
	if got := readMessage(t, bob); got.Body != "psst" || got.To != "bob" {
		t.Fatalf("dm to bob: got %+v", got)
	}
	// carol must NOT receive the DM (she should time out).
	carol.SetReadDeadline(time.Now().Add(400 * time.Millisecond))
	for {
		frame, err := carol.ReadFrame()
		if err != nil {
			break // good: timed out, no DM leaked
		}
		if m, e := protocol.Decode(string(frame)); e == nil && m.Type == protocol.TypeMessage && m.Body == "psst" {
			t.Fatal("carol received a DM not addressed to her")
		}
	}
}

func TestFileTransfer(t *testing.T) {
	addr := startServer(t, "s3cret", 18112)
	alice := connect(t, addr, "s3cret", "alice")
	bob := connect(t, addr, "s3cret", "bob")
	time.Sleep(100 * time.Millisecond)

	// 200 KB of random data exercises multi-chunk transfer.
	want := make([]byte, 200*1024)
	rand.Read(want)
	id := "alice-1"

	go func() {
		alice.WriteFrame([]byte(protocol.FileHeader{From: "alice", ID: id, Size: int64(len(want)), Filename: "blob.bin"}.Encode()))
		for off := 0; off < len(want); off += 32 * 1024 {
			end := off + 32*1024
			if end > len(want) {
				end = len(want)
			}
			alice.WriteFrame(protocol.EncodeChunk(id, want[off:end]))
		}
	}()

	bob.SetReadDeadline(time.Now().Add(5 * time.Second))
	var got bytes.Buffer
	var size int64
	header := false
	for {
		frame, err := bob.ReadFrame()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if cid, data, ok := protocol.DecodeChunk(frame); ok {
			if cid != id {
				continue
			}
			got.Write(data)
			if int64(got.Len()) >= size && header {
				break
			}
			continue
		}
		if strings.HasPrefix(string(frame), "FILE:") {
			fh, err := protocol.DecodeFileHeader(string(frame))
			if err != nil {
				t.Fatalf("bad header: %v", err)
			}
			size = fh.Size
			header = true
		}
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("file mismatch: got %d bytes want %d", got.Len(), len(want))
	}
}

func TestDiscovery(t *testing.T) {
	startServer(t, "s3cret", 18114)
	time.Sleep(150 * time.Millisecond)

	found := discovery.FindServers(1500 * time.Millisecond)
	var got *discovery.ServerInfo
	for i := range found {
		if found[i].Addr == "127.0.0.1:18114" {
			got = &found[i]
		}
	}
	if got == nil {
		t.Fatalf("server not discovered; found %+v", found)
	}
	if got.Name != "e2e" || !got.Private {
		t.Fatalf("discovery metadata wrong: %+v", got)
	}
}

func TestInProcessHost(t *testing.T) {
	// This is the path the client TUI uses when you "Create a room".
	inst, err := server.Start(server.Options{Name: "hosted", Pass: "k3y"})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer inst.Stop()
	if inst.Addr == "" || inst.Port == "0" {
		t.Fatalf("bad instance: %+v", inst)
	}

	alice := connect(t, inst.Addr, "k3y", "alice")
	bob := connect(t, inst.Addr, "k3y", "bob")
	time.Sleep(100 * time.Millisecond)

	alice.WriteFrame([]byte(protocol.Message{Type: protocol.TypeMessage, From: "alice", Body: "hosted hello"}.Encode()))
	if got := readMessage(t, bob); got.Body != "hosted hello" {
		t.Fatalf("got %+v", got)
	}
}

func TestWrongPasswordRejected(t *testing.T) {
	addr := startServer(t, "correct", 18113)
	badKey, _ := transport.DeriveKey("wrong")
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer raw.Close()
	conn, _ := transport.NewConn(raw, badKey)
	conn.WriteFrame([]byte(protocol.EncodeAuth("mallory")))

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := conn.ReadFrame(); err == nil {
		t.Fatal("server accepted a client with the wrong password")
	}
}
