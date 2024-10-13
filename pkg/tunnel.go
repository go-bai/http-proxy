package pkg

import (
	"net"
	"net/http"
	"sync/atomic"
)

func HandleTunneling(w http.ResponseWriter, r *http.Request) {
	destConn, err := net.DialTimeout("tcp", r.Host, dialDestTimeout)
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
		return
	}
	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, destConn, clientConn)
	atomic.AddInt64(&ActiveConnections, 1)
	go Transfer(shutdownCtx, clientConn, destConn)
}
