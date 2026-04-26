package pkg

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

func Transfer(ctx context.Context, destination, source net.Conn) {
	defer func() {
		destination.Close()
		source.Close()
		atomic.AddInt64(&ActiveConnections, -1)
	}()

	stopCancelHook := context.AfterFunc(ctx, func() {
		// Unblock io.CopyBuffer when graceful shutdown starts.
		_ = source.SetReadDeadline(time.Now())
		_ = destination.SetWriteDeadline(time.Now())
	})
	defer stopCancelHook()

	buf := make([]byte, 32*1024)
	_, err := io.CopyBuffer(destination, source, buf)
	if err != nil &&
		err != io.EOF &&
		!strings.Contains(err.Error(), "use of closed network connection") &&
		!strings.Contains(err.Error(), "context canceled") &&
		!(ctx.Err() != nil && errors.Is(err, os.ErrDeadlineExceeded)) {
		log.Printf("Transfer error: %v", err)
	}
}
