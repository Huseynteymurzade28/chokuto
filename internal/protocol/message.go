package protocol

import (
	"fmt"
	"strings"
)	

type MessaeType string

const (
	TypeMessage MessaeType = "message"
	TypeJoin    MessaeType = "join"
	TypeLeave   MessaeType = "leave"
)

type Message struct {
	Type MessaeType
	From string
	Body string
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
		Type: MessaeType(parts[0]),
		From: parts[1],
		Body: parts[2],
	}, nil
}