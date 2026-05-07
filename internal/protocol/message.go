package protocol

import (
	"fmt"
	"strings"
)

type MessageType string

const (
	TypeMessage MessageType = "MSG"
	TypeJoin    MessageType = "JOIN"
	TypeLeave   MessageType = "LEAVE"
	TypeFile    MessageType = "FILE"
)

type Message struct {
	Type MessageType
	From string
	Body string
}

type FileHeader struct {
	From     string
	Filename string
	Size     int64
}

func (m Message) Encode() string {
	return fmt.Sprintf("%s:%s:%s\n", m.Type, m.From, m.Body)
}

func Decode(raw string) (Message, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) != 3 {
		return Message{}, fmt.Errorf("invalid message format")
	}
	return Message{
		Type: MessageType(parts[0]),
		From: parts[1],
		Body: parts[2],
	}, nil
}

func (f FileHeader) Encode() string {
	return fmt.Sprintf("FILE:%s:%s:%d\n", f.From, f.Filename, f.Size)
}

func DecodeFileHeader(raw string) (FileHeader, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 {
		return FileHeader{}, fmt.Errorf("invalid file header format")
	}
	var size int64
	fmt.Sscanf(parts[3], "%d", &size)
	return FileHeader{
		From:     parts[1],
		Filename: parts[2],
		Size:     size,
	}, nil
}
