package pkg

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/netip"
	"strconv"
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

	cmdConnect = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	repSuccess             = 0x00
	repGeneralFailure      = 0x01
	repCommandNotSupported = 0x07
	repAddressNotSupported = 0x08
)

// Socks5Server handles SOCKS5 proxy connections.
type Socks5Server struct {
	AuthEnabled bool
	Password    string
}

func (s *Socks5Server) HandleConn(conn net.Conn) {
	if err := s.handshake(conn); err != nil {
		conn.Close()
		log.Printf("socks5 handshake failed: %v", err)
		return
	}

	targetAddr, err := s.handleRequest(conn)
	if err != nil {
		conn.Close()
		log.Printf("socks5 request failed: %v", err)
		return
	}

	addrPort, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err == nil {
		log.Printf("%-15s SOCKS5  %s", addrPort.Addr(), targetAddr)
	}

	destConn, err := net.DialTimeout("tcp", targetAddr, dialDestTimeout)
	if err != nil {
		s.sendReply(conn, repGeneralFailure, "0.0.0.0", 0)
		conn.Close()
		return
	}

	// send success reply
	localAddr := destConn.LocalAddr().(*net.TCPAddr)
	s.sendReply(conn, repSuccess, localAddr.IP.String(), uint16(localAddr.Port))

	// Transfer goroutines handle closing both connections
	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, destConn, conn)
	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, conn, destConn)
}

func (s *Socks5Server) handshake(conn net.Conn) error {
	// Read version and number of auth methods
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
		// No auth required
		_, err := conn.Write([]byte{socks5Version, authNone})
		return err
	}

	// Require username/password auth
	hasUserPass := false
	for _, m := range methods {
		if m == authUserPass {
			hasUserPass = true
			break
		}
	}
	if !hasUserPass {
		conn.Write([]byte{socks5Version, authNoAcceptable})
		return errors.New("client does not support username/password auth")
	}

	conn.Write([]byte{socks5Version, authUserPass})
	return s.authenticateUserPass(conn)
}

func (s *Socks5Server) authenticateUserPass(conn net.Conn) error {
	// RFC 1929: version(1) + ulen(1) + username(ulen) + plen(1) + password(plen)
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

func (s *Socks5Server) handleRequest(conn net.Conn) (string, error) {
	// version(1) + cmd(1) + rsv(1) + atyp(1)
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != socks5Version {
		return "", fmt.Errorf("unsupported version: %d", header[0])
	}
	if header[1] != cmdConnect {
		s.sendReply(conn, repCommandNotSupported, "0.0.0.0", 0)
		return "", fmt.Errorf("unsupported command: %d", header[1])
	}

	var host string
	switch header[3] {
	case atypIPv4:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case atypDomain:
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", err
		}
		domain := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, domain); err != nil {
			return "", err
		}
		host = string(domain)
	case atypIPv6:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	default:
		s.sendReply(conn, repAddressNotSupported, "0.0.0.0", 0)
		return "", fmt.Errorf("unsupported address type: %d", header[3])
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", err
	}
	port := binary.BigEndian.Uint16(portBuf)

	return net.JoinHostPort(host, strconv.Itoa(int(port))), nil
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

// ListenAndServeSocks5 starts a SOCKS5 proxy listener.
func ListenAndServeSocks5(addr string, server *Socks5Server) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("socks5 listen: %s", err)
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
