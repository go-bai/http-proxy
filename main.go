package main

import (
	"crypto/tls"
	"encoding/base64"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
)

func handleTunneling(w http.ResponseWriter, r *http.Request) {
	destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}
	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	}
	go transfer(destConn, clientConn)
	go transfer(clientConn, destConn)
}

func transfer(destination io.WriteCloser, source io.ReadCloser) {
	defer destination.Close()
	defer source.Close()
	io.Copy(destination, source)
}

func handleHTTP(w http.ResponseWriter, req *http.Request) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func basicProxyAuth(proxyAuth string) (username, password string, ok bool) {
	if proxyAuth == "" {
		return
	}

	if !strings.HasPrefix(proxyAuth, "Basic ") {
		return
	}
	c, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(proxyAuth, "Basic "))
	if err != nil {
		return
	}
	cs := string(c)
	s := strings.IndexByte(cs, ':')
	if s < 0 {
		return
	}

	return cs[:s], cs[s+1:], true
}

func handler(w http.ResponseWriter, r *http.Request) {
	if auth == authOn {
		_, p, ok := basicProxyAuth(r.Header.Get("Proxy-Authorization"))
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm=go`)
			http.Error(w, "proxy auth required", http.StatusProxyAuthRequired)
			return
		}

		if p != pass {
			http.Error(w, "proxy authentication failed", http.StatusForbidden)
			return
		}
		r.Header.Del("Proxy-Authorization")
	}

	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		log.Printf("parse addrPort %s failed: %s", r.RemoteAddr, err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	log.Printf("%-15s %-7s %s %s", addrPort.Addr(), r.Method, r.Host, r.URL.Path)

	if r.Method == http.MethodConnect {
		handleTunneling(w, r)
	} else {
		handleHTTP(w, r)
	}
}

var (
	addr = ":38888"
	auth = "on"
	pass = ""
)

const (
	authOn  = "on"
	authOff = "off"
)

func init() {
	addrEnv, b := os.LookupEnv("HTTP_PROXY_ADDR")
	if b {
		addr = addrEnv
	}
	authEnv, b := os.LookupEnv("HTTP_PROXY_AUTH")
	if b {
		auth = authEnv
	}
	if auth == authOn {
		passEnv, b := os.LookupEnv("HTTP_PROXY_PASS")
		if b {
			pass = passEnv
		} else {
			pass = uuid.New().String()
		}
	}
}

func main() {
	log.Printf("Listen on: %s\n", addr)
	log.Printf("Auth: %s\n", auth)
	if auth == authOn {
		log.Printf("Password: %s\n", pass)
	}

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(handler),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Fatal(server.ListenAndServe())
}
