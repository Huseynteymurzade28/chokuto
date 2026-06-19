package protocol

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type MessageType string

const (
	TypeMessage  MessageType = "MSG"
	TypeJoin     MessageType = "JOIN"
	TypeLeave    MessageType = "LEAVE"
	TypeFile     MessageType = "FILE"
	TypeTyping   MessageType = "TYPING"
	TypeUserList MessageType = "USERLIST"
)

// chunkPrefix marks a frame carrying raw file bytes: "CHUNK:<id>:<bytes>".
const chunkPrefix = "CHUNK:"

// authPrefix marks the first frame a client sends. On a private room it is
// encrypted, so a server that decrypts it successfully and sees this prefix
// knows the client has the right password and is speaking the same protocol.
const authPrefix = "HELLO:"

// EncodeAuth builds the handshake frame announcing a username.
func EncodeAuth(username string) string { return authPrefix + username }

// DecodeAuth validates and extracts the username from a handshake frame.
func DecodeAuth(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, authPrefix) {
		return "", false
	}
	return strings.TrimSpace(s[len(authPrefix):]), true
}

// Message is a control/text message. To is the DM target username; empty means
// broadcast to everyone.
type Message struct {
	Type MessageType
	From string
	To   string
	Body string
}

// FileHeader announces a file transfer. To empty means broadcast. ID groups the
// chunk frames that follow. Filename comes last so it may contain colons.
type FileHeader struct {
	From     string
	To       string
	ID       string
	Size     int64
	Filename string
}

func (m Message) Encode() string {
	return fmt.Sprintf("%s:%s:%s:%s", m.Type, m.From, m.To, m.Body)
}

func Decode(raw string) (Message, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 {
		return Message{}, fmt.Errorf("invalid message format")
	}
	return Message{
		Type: MessageType(parts[0]),
		From: parts[1],
		To:   parts[2],
		Body: parts[3],
	}, nil
}

func (f FileHeader) Encode() string {
	return fmt.Sprintf("FILE:%s:%s:%s:%d:%s", f.From, f.To, f.ID, f.Size, f.Filename)
}

func DecodeFileHeader(raw string) (FileHeader, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ":", 6)
	if len(parts) != 6 || parts[0] != "FILE" {
		return FileHeader{}, fmt.Errorf("invalid file header format")
	}
	size, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil {
		return FileHeader{}, fmt.Errorf("invalid file size")
	}
	return FileHeader{
		From:     parts[1],
		To:       parts[2],
		ID:       parts[3],
		Size:     size,
		Filename: parts[5],
	}, nil
}

// IsChunk reports whether a frame carries file bytes.
func IsChunk(frame []byte) bool {
	return bytes.HasPrefix(frame, []byte(chunkPrefix))
}

// EncodeChunk builds a chunk frame: "CHUNK:<id>:<raw bytes>".
func EncodeChunk(id string, data []byte) []byte {
	out := make([]byte, 0, len(chunkPrefix)+len(id)+1+len(data))
	out = append(out, chunkPrefix...)
	out = append(out, id...)
	out = append(out, ':')
	out = append(out, data...)
	return out
}

// DecodeChunk extracts the transfer ID and raw bytes from a chunk frame.
func DecodeChunk(frame []byte) (id string, data []byte, ok bool) {
	if !IsChunk(frame) {
		return "", nil, false
	}
	rest := frame[len(chunkPrefix):]
	i := bytes.IndexByte(rest, ':')
	if i < 0 {
		return "", nil, false
	}
	return string(rest[:i]), rest[i+1:], true
}
