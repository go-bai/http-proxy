package main

import (
	"crypto/tls"
	"log"
	"net/http"
	"net/netip"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"github.com/go-bai/http-proxy/pkg"
	"github.com/google/uuid"
)

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

func handler(w http.ResponseWriter, r *http.Request) {
	if auth == authOn {
		_, p, ok := pkg.BasicProxyAuth(r.Header.Get("Proxy-Authorization"))
		if !ok {
			w.Header().Set("Proxy-Authenticate", `Basic realm=Restricted`)
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
		pkg.HandleTunneling(w, r)
	} else {
		pkg.HandleHTTP(w, r)
	}
}

func main() {
	log.Printf("Listen on: %s\n", addr)
	log.Printf("Auth: %s\n", auth)
	if auth == authOn {
		log.Printf("Password: %s\n", pass)
	}

	go func() {
		for {
			time.Sleep(1 * time.Minute)
			log.Printf("active connections: %d, goroutine number: %d", atomic.LoadInt64(&pkg.ActiveConnections), runtime.NumGoroutine())
		}
	}()

	server := &http.Server{
		Addr:    addr,
		Handler: http.HandlerFunc(handler),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
		// set timeout for read, write and idle, prevent slowloris attack
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	pkg.ListenAndServeWithGracefulShutdown(server)
}
