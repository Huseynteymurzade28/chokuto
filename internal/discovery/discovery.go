package discovery

import (
	"fmt"
	"net"
	"strings"
	"time"
)

const (
	discoverMsg  = "LANDROP_DISCOVER"
	hereMsg      = "LANDROP_HERE"
	discoverPort = 9999
)

// ServerInfo describes a server found on the LAN.
type ServerInfo struct {
	Name    string
	Addr    string // host:port
	Private bool
}

// Serve answers discovery probes in the background, advertising the server's
// name and whether it is password protected. The returned function stops the
// responder (closing the UDP socket); call it when the server shuts down.
func Serve(chatPort, name string, private bool) (stop func(), err error) {
	addr := &net.UDPAddr{Port: discoverPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return nil, err
	}

	priv := "0"
	if private {
		priv = "1"
	}
	// Name is sanitised so the colon-delimited reply stays parseable.
	name = strings.ReplaceAll(name, ":", " ")

	go func() {
		buf := make([]byte, 1024)
		for {
			n, remoteAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // socket closed via stop()
			}
			if string(buf[:n]) == discoverMsg {
				reply := fmt.Sprintf("%s:%s:%s:%s", hereMsg, chatPort, priv, name)
				conn.WriteToUDP([]byte(reply), remoteAddr)
			}
		}
	}()
	return func() { conn.Close() }, nil
}

func parseReply(raw string, remoteIP string) (ServerInfo, bool) {
	// LANDROP_HERE:<port>:<priv>:<name>
	parts := strings.SplitN(raw, ":", 4)
	if len(parts) != 4 || parts[0] != hereMsg {
		return ServerInfo{}, false
	}
	ip := remoteIP
	if ip == "0.0.0.0" || ip == "<nil>" || ip == "" {
		ip = "127.0.0.1"
	}
	return ServerInfo{
		Name:    parts[3],
		Addr:    fmt.Sprintf("%s:%s", ip, parts[1]),
		Private: parts[2] == "1",
	}, true
}

// FindServers broadcasts a probe and collects every server that replies within
// the timeout window, de-duplicated by address.
func FindServers(timeout time.Duration) []ServerInfo {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	// Probe localhost (Linux does not loop broadcast back to the sender) and the
	// LAN broadcast address.
	targets := []*net.UDPAddr{
		{IP: net.IPv4(127, 0, 0, 1), Port: discoverPort},
		{IP: net.IPv4(255, 255, 255, 255), Port: discoverPort},
	}
	for _, t := range targets {
		conn.WriteToUDP([]byte(discoverMsg), t)
	}

	deadline := time.Now().Add(timeout)
	conn.SetReadDeadline(deadline)

	seen := make(map[string]bool)
	var servers []ServerInfo
	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			break // timeout
		}
		info, ok := parseReply(string(buf[:n]), remoteAddr.IP.String())
		if !ok || seen[info.Addr] {
			continue
		}
		seen[info.Addr] = true
		servers = append(servers, info)
	}
	return servers
}
