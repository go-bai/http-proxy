package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
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

var (
	whitelist     sync.Map
	whitelistName = "conf/whitelist"
	whitelistPath = "conf"
)

func listenWhitelist() {
	log.Printf("start watching %s...", whitelistName)
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("watch %s failed: %s", whitelistName, err.Error())
		return
	}
	defer watcher.Close()
	defer log.Printf("watcher closed!!!")

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) && event.Name == whitelistName {
					updateWhitelist()
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("watcher error:", err)
			}
		}
	}()

	err = watcher.Add(whitelistPath)
	if err != nil {
		log.Fatalf("watcher add %s failed: %s", whitelistName, err)
	}

	<-make(chan struct{})
}

func updateWhitelist() {
	log.Printf("start loading %s...", whitelistName)
	defer log.Printf("%s has been loaded.", whitelistName)
	f, err := os.Open(whitelistName)
	if err != nil {
		log.Fatalf("open file %s failed: %s", whitelistName, err)
	}
	defer f.Close()

	newWhitelist := make(map[string]struct{}, 0)

	scanner := bufio.NewScanner(f)
	for i := 0; scanner.Scan(); i++ {
		line := strings.TrimSpace(scanner.Text())
		ipPrefix, err := netip.ParsePrefix(line)
		if err != nil {
			log.Printf("line %d: \"%s\" parse failed: %s", i, line, err.Error())
		}
		newWhitelist[line] = struct{}{}
		whitelist.Store(line, ipPrefix)
	}

	whitelist.Range(func(key, value any) bool {
		if _, ok := newWhitelist[key.(string)]; !ok {
			whitelist.Delete(key)
		}
		return true
	})
}

func init() {

	updateWhitelist()

	go listenWhitelist()
}

func handler(w http.ResponseWriter, r *http.Request) {
	log.Printf("%-21s %-7s %s %s", r.RemoteAddr, r.Method, r.Host, r.URL.Path)
	addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		log.Printf("parse addrPort %s failed: %s", r.RemoteAddr, err.Error())
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ok := true
	whitelist.Range(func(k, v any) bool {
		ok = !ok
		if ok = v.(netip.Prefix).Contains(addrPort.Addr()); ok {
			return false
		}
		return true
	})

	if !ok {
		log.Printf("ip \"%s\" not configed in IPWhitelist", addrPort.Addr())
		http.Error(w, fmt.Sprintf("ip \"%s\" not configed in IPWhitelist", addrPort.Addr()), http.StatusForbidden)
		return
	}

	if r.Method == http.MethodConnect {
		handleTunneling(w, r)
	} else {
		handleHTTP(w, r)
	}
}

func main() {
	httpProxyListenAddr := ":8888"
	s, b := os.LookupEnv("HTTP_PROXY_LISTEN_ADDR")
	if b {
		httpProxyListenAddr = s
	}

	log.Printf("Listen on %s", httpProxyListenAddr)
	server := &http.Server{
		Addr:    httpProxyListenAddr,
		Handler: http.HandlerFunc(handler),
		// Disable HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	log.Fatal(server.ListenAndServe())
}
