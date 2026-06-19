package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// recentServer is a previously-used server, remembered so the user does not
// have to type its address again.
type recentServer struct {
	Name     string    `json:"name"`
	Addr     string    `json:"addr"`
	Private  bool      `json:"private"`
	LastUsed time.Time `json:"lastUsed"`
}

func configPath() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "chokuto", "servers.json")
}

func loadRecents() []recentServer {
	data, err := os.ReadFile(configPath())
	if err != nil {
		return nil
	}
	var servers []recentServer
	if json.Unmarshal(data, &servers) != nil {
		return nil
	}
	sort.Slice(servers, func(i, j int) bool {
		return servers[i].LastUsed.After(servers[j].LastUsed)
	})
	return servers
}

// rememberServer records a successful connection, de-duplicated by address and
// capped to the 10 most recent.
func rememberServer(s recentServer) {
	s.LastUsed = time.Now()
	servers := loadRecents()
	out := []recentServer{s}
	for _, old := range servers {
		if old.Addr != s.Addr {
			out = append(out, old)
		}
	}
	if len(out) > 10 {
		out = out[:10]
	}

	path := configPath()
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0o600)
}
