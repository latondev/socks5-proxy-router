package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const (
	socks5Version = 0x05
	cmdConnect    = 0x01
	atypIPv4      = 0x01
	atypDomain    = 0x03
	atypIPv6      = 0x04
)

type Server struct {
	listenAddr string
	pool       *ProxyPool
	authUser   string
	authPass   string
}

func NewServer(listenAddr string, pool *ProxyPool, authUser, authPass string) *Server {
	return &Server{
		listenAddr: listenAddr,
		pool:       pool,
		authUser:   authUser,
		authPass:   authPass,
	}
}

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}
	log.Printf("[server] SOCKS5 proxy listening on %s (auth: %v)", s.listenAddr, s.authUser != "")

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("[server] accept error: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	// 1. SOCKS5 handshake - read greeting with 10s deadline
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n < 2 || buf[0] != socks5Version {
		return
	}

	nMethods := int(buf[1])
	if n < 2+nMethods {
		return
	}
	methods := buf[2 : 2+nMethods]

	hasUserPass := false
	hasNoAuth := false
	for _, m := range methods {
		if m == 0x02 { // USERNAME/PASSWORD (RFC 1929)
			hasUserPass = true
		} else if m == 0x00 { // NO AUTHENTICATION REQUIRED
			hasNoAuth = true
		}
	}

	if s.authUser != "" && s.authPass != "" {
		if !hasUserPass {
			// Reject clients that don't support Username/Password authentication
			conn.Write([]byte{socks5Version, 0xFF})
			return
		}

		// Choose Method 0x02 (Username/Password)
		if _, err := conn.Write([]byte{socks5Version, 0x02}); err != nil {
			return
		}

		// Read RFC 1929 subnegotiation:
		authHdr := make([]byte, 2)
		if _, err := io.ReadFull(conn, authHdr); err != nil || authHdr[0] != 0x01 {
			return
		}
		uLen := int(authHdr[1])
		unameBuf := make([]byte, uLen)
		if _, err := io.ReadFull(conn, unameBuf); err != nil {
			return
		}

		pLenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, pLenBuf); err != nil {
			return
		}
		pLen := int(pLenBuf[0])
		passBuf := make([]byte, pLen)
		if _, err := io.ReadFull(conn, passBuf); err != nil {
			return
		}

		if string(unameBuf) != s.authUser || string(passBuf) != s.authPass {
			conn.Write([]byte{0x01, 0x01}) // Status 0x01 = Auth failed
			return
		}

		// Auth success: VER=0x01, STATUS=0x00
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	} else {
		if !hasNoAuth {
			conn.Write([]byte{socks5Version, 0xFF})
			return
		}
		// Reply: no auth required
		if _, err := conn.Write([]byte{socks5Version, 0x00}); err != nil {
			return
		}
	}

	// 2. Read connect request
	n, err = conn.Read(buf)
	if err != nil || n < 7 || buf[1] != cmdConnect {
		s.sendReply(conn, 0x07) // command not supported
		return
	}

	// Clear deadline for relaying data
	conn.SetDeadline(time.Time{})

	// Parse target address
	targetAddr, err := parseTarget(buf[:n])
	if err != nil {
		s.sendReply(conn, 0x04) // host unreachable
		return
	}

	// 3. Connect via fastest live proxy (with fast 3s dial timeout); if fails, prune and retry
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		upstream, ok := s.pool.Current()
		if !ok {
			break
		}

		// Fast 3s timeout so clients (like Zed / AI agents) don't hang
		remote, err := dialViaSOCKS5(upstream, targetAddr, 3*time.Second)
		if err != nil {
			log.Printf("[server] upstream %s dial failed: %v, pruning and trying next...", upstream.Addr(), err)
			s.pool.RemoveFailed(upstream.Addr())
			addActivityLog(fmt.Sprintf("Pruned dead proxy %s (%s), promoted next fastest", upstream.Addr(), upstream.Country), "switch")
			continue
		}

		// Success: upstream established!
		s.sendReply(conn, 0x00)
		s.relay(conn, remote, upstream)
		return
	}

	// 4. Emergency Failover: If all upstream proxies in pool failed or empty, fallback to DIRECT VPS connection!
	// This prevents "Connection Interrupted" for AI clients / Zed!
	log.Printf("[server] upstream pool exhausted/failed, falling back to DIRECT VPS connection for %s", targetAddr)
	addActivityLog(fmt.Sprintf("Emergency: Fallback to Direct VPS route for %s", targetAddr), "fallback")
	directRemote, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		log.Printf("[server] direct connection to %s failed: %v", targetAddr, err)
		s.sendReply(conn, 0x04) // host unreachable
		TriggerRefresh()
		return
	}

	s.sendReply(conn, 0x00)
	s.relayDirect(conn, directRemote)
}

func (s *Server) sendReply(conn net.Conn, status byte) {
	conn.Write([]byte{socks5Version, status, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0})
}

func parseTarget(buf []byte) (string, error) {
	if len(buf) < 7 {
		return "", fmt.Errorf("request too short")
	}

	var host string
	var portOffset int

	switch buf[3] {
	case atypIPv4:
		if len(buf) < 10 {
			return "", fmt.Errorf("ipv4 request too short")
		}
		host = fmt.Sprintf("%d.%d.%d.%d", buf[4], buf[5], buf[6], buf[7])
		portOffset = 8
	case atypDomain:
		domainLen := int(buf[4])
		if len(buf) < 5+domainLen+2 {
			return "", fmt.Errorf("domain request too short")
		}
		host = string(buf[5 : 5+domainLen])
		portOffset = 5 + domainLen
	case atypIPv6:
		if len(buf) < 22 {
			return "", fmt.Errorf("ipv6 request too short")
		}
		ip := net.IP(buf[4:20])
		host = ip.String()
		portOffset = 20
	default:
		return "", fmt.Errorf("unsupported address type: %d", buf[3])
	}

	port := int(buf[portOffset])<<8 | int(buf[portOffset+1])
	return fmt.Sprintf("%s:%d", host, port), nil
}

func dialViaSOCKS5(upstream Proxy, target string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", upstream.Addr(), timeout)
	if err != nil {
		return nil, err
	}
	conn.SetDeadline(time.Now().Add(timeout))

	conn.Write([]byte{0x05, 0x01, 0x00})
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		conn.Close()
		return nil, err
	}
	if buf[0] != 0x05 {
		conn.Close()
		return nil, fmt.Errorf("not socks5")
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		conn.Close()
		return nil, err
	}
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, atypIPv4)
			req = append(req, ip4...)
		} else {
			req = append(req, atypIPv6)
			req = append(req, ip...)
		}
	} else {
		req = append(req, atypDomain, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	req = append(req, byte(port>>8), byte(port&0xff))

	conn.Write(req)

	resp := make([]byte, 256)
	n, err := conn.Read(resp)
	if err != nil || n < 2 || resp[1] != 0x00 {
		conn.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("upstream connect failed, status: %d", resp[1])
	}

	conn.SetDeadline(time.Time{})
	return conn, nil
}

func (s *Server) relay(client, upstream net.Conn, px Proxy) {
	defer client.Close()
	defer upstream.Close()

	start := time.Now()
	var clientBytes, upstreamBytes int64
	var wg sync.WaitGroup
	wg.Add(2)

	// client -> upstream
	go func() {
		defer wg.Done()
		n, _ := io.Copy(upstream, client)
		atomic.StoreInt64(&clientBytes, n)
		if tc, ok := upstream.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// upstream -> client
	go func() {
		defer wg.Done()
		n, _ := io.Copy(client, upstream)
		atomic.StoreInt64(&upstreamBytes, n)
		if tc, ok := client.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
	duration := time.Since(start)

	cBytes := atomic.LoadInt64(&clientBytes)
	uBytes := atomic.LoadInt64(&upstreamBytes)

	// Zombie proxy detection:
	// If client sent meaningful request (e.g. TLS ClientHello > 40 bytes) but upstream closed connection immediately
	// without returning ANY response data (< 6 seconds duration), this proxy dropped/blocked SSL/TLS traffic!
	if cBytes > 40 && uBytes == 0 && duration < 6*time.Second {
		log.Printf("[server] zombie proxy detected: %s dropped connection with 0 bytes returned (sent: %d, dur: %v), pruning...", px.Addr(), cBytes, duration)
		s.pool.RemoveFailed(px.Addr())
		addActivityLog(fmt.Sprintf("Pruned zombie proxy %s (%s, 0 bytes returned)", px.Addr(), px.Country), "switch")
	}
}

func (s *Server) relayDirect(client, remote net.Conn) {
	defer client.Close()
	defer remote.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(remote, client)
		if tc, ok := remote.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(client, remote)
		if tc, ok := client.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}
