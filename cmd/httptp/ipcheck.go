package main

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
)

func parseAllowedIPs(allowedIPs string) (map[uint32]struct{}, error) {
	if len(allowedIPs) == 0 {
		return nil, nil
	}

	m := make(map[uint32]struct{})
	for _, ipStr := range strings.Split(allowedIPs, ",") {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid ip %q", ipStr)
		}
		ip = ip.To4()
		if ip == nil {
			return nil, fmt.Errorf("invalid IPv4 %q", ipStr)
		}
		n := ip2Uint32(ip)
		m[n] = struct{}{}
	}
	return m, nil
}

type ipCheckListener struct {
	net.Listener
	allowedIPs map[uint32]struct{}
}

func (ln *ipCheckListener) Accept() (net.Conn, error) {
	for {
		conn, err := ln.accept()
		if err == errDisallowedIP {
			log.Printf("%q<->%q: %s", conn.RemoteAddr(), conn.LocalAddr(), err)
			conn.Close()
			continue
		}
		return conn, err
	}
}

func (ln *ipCheckListener) accept() (net.Conn, error) {
	conn, err := ln.Listener.Accept()
	if err != nil {
		return nil, err
	}

	raddr := conn.RemoteAddr()
	tcpAddr, ok := raddr.(*net.TCPAddr)
	if !ok {
		return conn, nil
	}

	ip := tcpAddr.IP.To4()
	if ip == nil {
		return conn, nil
	}

	n := ip2Uint32(ip)
	if _, ok := ln.allowedIPs[n]; !ok {
		// The connection is closed by the caller
		return conn, errDisallowedIP
	}

	return conn, nil
}

var errDisallowedIP = errors.New("disallowed access for the given client ip")

func ip2Uint32(ip []byte) uint32 {
	return uint32(ip[3]) | (uint32(ip[2]) << 8) | (uint32(ip[1]) << 16) | (uint32(ip[0]) << 24)
}
