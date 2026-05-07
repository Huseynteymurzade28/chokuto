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
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Println("discovery dinlenemedi:", err)
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

func FindServer(timeout time.Duration) (string, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return "", err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	broadcastAddr := &net.UDPAddr{
		IP:   net.IPv4(255, 255, 255, 255),
		Port: discoverPort,
	}

	_, err = conn.WriteToUDP([]byte(discoverMsg), broadcastAddr)
	if err != nil {
		return "", err
	}

	buf := make([]byte, 1024)
	n, remoteAddr, err := conn.ReadFromUDP(buf)
	if err != nil {
		return "", fmt.Errorf("server bulunamadı")
	}

	msg := string(buf[:n])
	if len(msg) <= len(hereMsg)+1 {
		return "", fmt.Errorf("geçersiz cevap")
	}

	port := msg[len(hereMsg)+1:]
	return fmt.Sprintf("%s:%s", remoteAddr.IP.String(), port), nil
}
