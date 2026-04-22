package pkg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
)

const (
	socks5Version    = 0x05
	authNone         = 0x00
	authUserPass     = 0x02
	authNoAcceptable = 0xFF

	userPassVersion = 0x01
	authSuccess     = 0x00
	authFailure     = 0x01

	cmdConnect      = 0x01
	cmdUDPAssociate = 0x03

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	repSuccess             = 0x00
	repGeneralFailure      = 0x01
	repCommandNotSupported = 0x07
	repAddressNotSupported = 0x08
)

// udpAssociation is one UDP ASSOCIATE session bound to a TCP control connection.
type udpAssociation struct {
	clientUDPAddr atomic.Pointer[net.UDPAddr] // confirmed on first UDP packet
	outConn       *net.UDPConn                // per-assoc outbound socket
}

// udpRelay owns the shared UDP socket and routes packets across all associations.
type udpRelay struct {
	conn   *net.UDPConn
	mu     sync.RWMutex
	assocs map[string]*udpAssociation // keyed by client IP
}

func newUDPRelay(addr string) (*udpRelay, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return &udpRelay{conn: conn, assocs: make(map[string]*udpAssociation)}, nil
}

func (r *udpRelay) register(clientIP string, assoc *udpAssociation) {
	r.mu.Lock()
	r.assocs[clientIP] = assoc
	r.mu.Unlock()
}

func (r *udpRelay) unregister(clientIP string) {
	r.mu.Lock()
	delete(r.assocs, clientIP)
	r.mu.Unlock()
}

// run is the single goroutine that reads all inbound UDP from clients and
// dispatches each packet to the correct association's outbound socket.
func (r *udpRelay) run() {
	buf := make([]byte, 64*1024)
	for {
		n, clientAddr, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if shutdownCtx.Err() != nil {
				return
			}
			log.Printf("udp relay read: %v", err)
			continue
		}
		r.forward(buf[:n], clientAddr)
	}
}

func normalizeIP(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	return ip.String()
}

func (r *udpRelay) forward(pkt []byte, clientAddr *net.UDPAddr) {
	// SOCKS5 UDP header: RSV(2) + FRAG(1) + ATYP(1) + DST.ADDR + DST.PORT(2)
	if len(pkt) < 4 {
		return
	}
	if pkt[2] != 0 { // fragmentation not supported
		return
	}

	destAddr, headerLen, err := parseUDPDestAddr(pkt[3:])
	if err != nil {
		return
	}
	payload := pkt[3+headerLen:]

	clientIP := normalizeIP(clientAddr.IP)
	r.mu.RLock()
	assoc := r.assocs[clientIP]
	r.mu.RUnlock()
	if assoc == nil {
		return
	}

	// Confirm the real client UDP addr on first packet (ASSOCIATE request often has 0.0.0.0:0)
	assoc.clientUDPAddr.CompareAndSwap(nil, clientAddr)

	udpDest, err := net.ResolveUDPAddr("udp", destAddr)
	if err != nil {
		return
	}
	assoc.outConn.WriteToUDP(payload, udpDest)
}

// parseUDPDestAddr parses ATYP+addr+port from the SOCKS5 UDP header (after RSV+FRAG).
// Returns the destination address string and number of bytes consumed.
func parseUDPDestAddr(b []byte) (string, int, error) {
	if len(b) < 1 {
		return "", 0, errors.New("short buffer")
	}
	var host string
	var consumed int
	switch b[0] {
	case atypIPv4:
		if len(b) < 1+4+2 {
			return "", 0, errors.New("short ipv4")
		}
		host = net.IP(b[1:5]).String()
		consumed = 1 + 4
	case atypDomain:
		if len(b) < 2 {
			return "", 0, errors.New("short domain len")
		}
		dlen := int(b[1])
		if len(b) < 2+dlen+2 {
			return "", 0, errors.New("short domain")
		}
		host = string(b[2 : 2+dlen])
		consumed = 2 + dlen
	case atypIPv6:
		if len(b) < 1+16+2 {
			return "", 0, errors.New("short ipv6")
		}
		host = net.IP(b[1:17]).String()
		consumed = 1 + 16
	default:
		return "", 0, fmt.Errorf("unknown atyp: %d", b[0])
	}
	port := binary.BigEndian.Uint16(b[consumed : consumed+2])
	return net.JoinHostPort(host, strconv.Itoa(int(port))), consumed + 2, nil
}

// buildUDPPacket wraps raw data with a SOCKS5 UDP header for the given source addr.
func buildUDPPacket(from *net.UDPAddr, data []byte) []byte {
	var atyp byte
	var addrBytes []byte
	if ip4 := from.IP.To4(); ip4 != nil {
		atyp = atypIPv4
		addrBytes = ip4
	} else {
		atyp = atypIPv6
		addrBytes = from.IP.To16()
	}
	hdr := make([]byte, 0, 4+len(addrBytes)+2)
	hdr = append(hdr, 0, 0, 0, atyp)
	hdr = append(hdr, addrBytes...)
	hdr = binary.BigEndian.AppendUint16(hdr, uint16(from.Port))
	return append(hdr, data...)
}

// Socks5Server handles SOCKS5 proxy connections.
type Socks5Server struct {
	AuthEnabled bool
	Password    string
	relay       *udpRelay
}

func (s *Socks5Server) HandleConn(conn net.Conn) {
	if err := s.handshake(conn); err != nil {
		conn.Close()
		log.Printf("socks5 handshake failed: %v", err)
		return
	}

	cmd, targetAddr, err := s.readRequest(conn)
	if err != nil {
		conn.Close()
		log.Printf("socks5 request failed: %v", err)
		return
	}

	switch cmd {
	case cmdConnect:
		s.handleConnect(conn, targetAddr)
	case cmdUDPAssociate:
		s.handleUDPAssociate(conn)
	}
}

func (s *Socks5Server) handleConnect(conn net.Conn, targetAddr string) {
	addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err == nil {
		log.Printf("%-15s SOCKS5  CONNECT %s", addrPort.Addr(), targetAddr)
	}

	destConn, err := net.DialTimeout("tcp", targetAddr, dialDestTimeout)
	if err != nil {
		s.sendReply(conn, repGeneralFailure, "0.0.0.0", 0)
		conn.Close()
		return
	}

	localAddr := destConn.LocalAddr().(*net.TCPAddr)
	s.sendReply(conn, repSuccess, localAddr.IP.String(), uint16(localAddr.Port))

	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, destConn, conn)
	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, conn, destConn)
}

func (s *Socks5Server) handleUDPAssociate(tcpConn net.Conn) {
	defer tcpConn.Close()

	localIP := tcpConn.LocalAddr().(*net.TCPAddr).IP.String()
	clientIP := normalizeIP(tcpConn.RemoteAddr().(*net.TCPAddr).IP)

	outConn, err := net.ListenUDP("udp", &net.UDPAddr{})
	if err != nil {
		s.sendReply(tcpConn, repGeneralFailure, "0.0.0.0", 0)
		return
	}

	assoc := &udpAssociation{outConn: outConn}
	s.relay.register(clientIP, assoc)
	defer func() {
		s.relay.unregister(clientIP)
		outConn.Close()
	}()

	relayPort := uint16(s.relay.conn.LocalAddr().(*net.UDPAddr).Port)
	s.sendReply(tcpConn, repSuccess, localIP, relayPort)

	addrPort, err := netip.ParseAddrPort(tcpConn.RemoteAddr().String())
	if err == nil {
		log.Printf("%-15s SOCKS5  UDP ASSOCIATE", addrPort.Addr())
	}

	// Forward replies from destinations back to client via shared relay socket.
	go func() {
		buf := make([]byte, 64*1024)
		for {
			n, from, err := outConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			clientUDPAddr := assoc.clientUDPAddr.Load()
			if clientUDPAddr == nil {
				continue
			}
			s.relay.conn.WriteToUDP(buildUDPPacket(from, buf[:n]), clientUDPAddr)
		}
	}()

	// Block until the TCP control connection closes (signals end of UDP session).
	io.ReadFull(tcpConn, make([]byte, 1))
}

func (s *Socks5Server) handshake(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != socks5Version {
		return fmt.Errorf("unsupported SOCKS version: %d", header[0])
	}

	methods := make([]byte, header[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return err
	}

	if !s.AuthEnabled {
		_, err := conn.Write([]byte{socks5Version, authNone})
		return err
	}

	if !slices.Contains(methods, byte(authUserPass)) {
		conn.Write([]byte{socks5Version, authNoAcceptable})
		return errors.New("client does not support username/password auth")
	}

	conn.Write([]byte{socks5Version, authUserPass})
	return s.authenticateUserPass(conn)
}

func (s *Socks5Server) authenticateUserPass(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	if header[0] != userPassVersion {
		return fmt.Errorf("unsupported auth version: %d", header[0])
	}

	username := make([]byte, header[1])
	if _, err := io.ReadFull(conn, username); err != nil {
		return err
	}

	plenBuf := make([]byte, 1)
	if _, err := io.ReadFull(conn, plenBuf); err != nil {
		return err
	}
	password := make([]byte, plenBuf[0])
	if _, err := io.ReadFull(conn, password); err != nil {
		return err
	}

	if string(password) != s.Password {
		conn.Write([]byte{userPassVersion, authFailure})
		return errors.New("authentication failed")
	}

	_, err := conn.Write([]byte{userPassVersion, authSuccess})
	return err
}

// readRequest reads a SOCKS5 request and returns cmd + address string.
// For CONNECT: address is "host:port". For UDP ASSOCIATE: address is the client hint (often "0.0.0.0:0").
func (s *Socks5Server) readRequest(conn net.Conn) (byte, string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return 0, "", err
	}
	if header[0] != socks5Version {
		return 0, "", fmt.Errorf("unsupported version: %d", header[0])
	}

	cmd := header[1]
	if cmd != cmdConnect && cmd != cmdUDPAssociate {
		s.sendReply(conn, repCommandNotSupported, "0.0.0.0", 0)
		return 0, "", fmt.Errorf("unsupported command: %d", cmd)
	}

	var host string
	switch header[3] {
	case atypIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return 0, "", err
		}
		host = net.IP(addr).String()
	case atypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return 0, "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return 0, "", err
		}
		host = string(domain)
	case atypIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return 0, "", err
		}
		host = net.IP(addr).String()
	default:
		s.sendReply(conn, repAddressNotSupported, "0.0.0.0", 0)
		return 0, "", fmt.Errorf("unsupported address type: %d", header[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return 0, "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	return cmd, net.JoinHostPort(host, strconv.Itoa(int(port))), nil
}

func (s *Socks5Server) sendReply(conn net.Conn, rep byte, bindAddr string, bindPort uint16) {
	ip := net.ParseIP(bindAddr)
	var atyp byte
	var addr []byte
	if ip4 := ip.To4(); ip4 != nil {
		atyp = atypIPv4
		addr = ip4
	} else if ip6 := ip.To16(); ip6 != nil {
		atyp = atypIPv6
		addr = ip6
	} else {
		atyp = atypIPv4
		addr = []byte{0, 0, 0, 0}
	}

	reply := make([]byte, 0, 4+len(addr)+2)
	reply = append(reply, socks5Version, rep, 0x00, atyp)
	reply = append(reply, addr...)
	reply = binary.BigEndian.AppendUint16(reply, bindPort)
	conn.Write(reply)
}

// ListenAndServeSocks5 starts a SOCKS5 proxy on addr (TCP + UDP on the same port).
func ListenAndServeSocks5(addr string, server *Socks5Server) {
	relay, err := newUDPRelay(addr)
	if err != nil {
		log.Fatalf("socks5 udp listen: %s", err)
	}
	server.relay = relay

	go func() {
		<-shutdownCtx.Done()
		relay.conn.Close()
	}()
	go relay.run()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("socks5 tcp listen: %s", err)
	}
	go func() {
		<-shutdownCtx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if shutdownCtx.Err() != nil {
				return
			}
			log.Printf("socks5 accept: %s", err)
			continue
		}
		go server.HandleConn(conn)
	}
}
