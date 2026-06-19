package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// histLine is the on-disk form of a chatLine (its fields are unexported).
type histLine struct {
	Kind string    `json:"kind"`
	From string    `json:"from"`
	Body string    `json:"body"`
	TS   time.Time `json:"ts"`
}

const historyTail = 200

func historyDir() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "chokuto", "history")
}

// historyFile maps a server address to its log file, sanitised for the FS.
func historyFile(serverID string) string {
	safe := strings.NewReplacer("/", "_", ":", "_", "\\", "_", " ", "_").Replace(serverID)
	if safe == "" {
		safe = "default"
	}
	return filepath.Join(historyDir(), safe+".jsonl")
}

// persistKinds are the line kinds worth keeping across sessions.
var persistKinds = map[string]bool{
	"me": true, "msg": true, "file": true, "dm": true, "dmout": true,
}

func loadHistory(serverID string) []chatLine {
	f, err := os.Open(historyFile(serverID))
	if err != nil {
		return nil
	}
	defer f.Close()

	var all []chatLine
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var h histLine
		if json.Unmarshal(sc.Bytes(), &h) != nil {
			continue
		}
		all = append(all, chatLine{kind: h.Kind, from: h.From, body: h.Body, ts: h.TS})
	}
	if len(all) > historyTail {
		all = all[len(all)-historyTail:]
	}
	return all
}

func appendHistory(serverID string, l chatLine) {
	if !persistKinds[l.kind] {
		return
	}
	path := historyFile(serverID)
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	data, err := json.Marshal(histLine{Kind: l.kind, From: l.from, Body: l.body, TS: l.ts})
	if err != nil {
		return
	}
	f.Write(append(data, '\n'))
}
