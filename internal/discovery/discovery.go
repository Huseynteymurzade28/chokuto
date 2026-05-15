package discovery

import (
	"fmt"
	"net"
	"time"
)

const (
	discoverMsg  = "LANDROP_DISCOVER"
	hereMsg      = "LANDROP_HERE"
	discoverPort = 9999
)

func ListenAndRespond(chatPort string) {
	addr := &net.UDPAddr{Port: discoverPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		fmt.Println("discovery listen failed:", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}
		if string(buf[:n]) == discoverMsg {
			reply := fmt.Sprintf("%s:%s", hereMsg, chatPort)
			conn.WriteToUDP([]byte(reply), remoteAddr)
		}
	}
}

func probe(conn *net.UDPConn, target *net.UDPAddr, deadline time.Time) (string, error) {
	conn.SetDeadline(deadline)
	if _, err := conn.WriteToUDP([]byte(discoverMsg), target); err != nil {
		return "", err
	}
	buf := make([]byte, 1024)
	n, remoteAddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return "", err
	}
	msg := string(buf[:n])
	if len(msg) <= len(hereMsg)+1 {
		return "", fmt.Errorf("invalid response")
	}
	port := msg[len(hereMsg)+1:]
	ip := remoteAddr.IP.String()
	if ip == "0.0.0.0" || ip == "<nil>" {
		ip = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%s", ip, port), nil
}

func FindServer(timeout time.Duration) (string, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: 0})
	if err != nil {
		return "", err
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	slice := timeout / 3

	// Try localhost first (client and server on same machine).
	// Linux does not loop broadcast back to the sender's machine.
	localhost := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: discoverPort}
	if addr, err := probe(conn, localhost, time.Now().Add(slice)); err == nil {
		return addr, nil
	}

	// Fall back to LAN broadcast.
	broadcast := &net.UDPAddr{IP: net.IPv4(255, 255, 255, 255), Port: discoverPort}
	if addr, err := probe(conn, broadcast, deadline); err == nil {
		return addr, nil
	}

	return "", fmt.Errorf("server not found")
}
