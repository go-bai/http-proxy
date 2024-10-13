package pkg

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
)

type TransferResult struct {
	N   int
	Err error
}

func read(ctx context.Context, conn net.Conn, buf []byte) TransferResult {
	ch := make(chan TransferResult)
	go func() {
		// ignore if context is done
		n, err := conn.Read(buf)
		ch <- TransferResult{n, err}
	}()
	select {
	case <-ctx.Done():
		return TransferResult{0, ctx.Err()}
	case res := <-ch:
		return res
	}
}

func write(ctx context.Context, conn net.Conn, buf []byte) TransferResult {
	ch := make(chan TransferResult)
	go func() {
		// ignore if context is done
		n, err := conn.Write(buf)
		ch <- TransferResult{n, err}
	}()
	select {
	case <-ctx.Done():
		return TransferResult{0, ctx.Err()}
	case res := <-ch:
		return res
	}
}

func Transfer(ctx context.Context, destination, source net.Conn) {
	defer func() {
		destination.Close()
		source.Close()
		atomic.AddInt64(&ActiveConnections, -1)
	}()

	buf := make([]byte, 32*1024)
	for {
		readRes := read(ctx, source, buf)
		if readRes.N > 0 {
			writeRes := write(ctx, destination, buf[0:readRes.N])
			// TODO: check nw < 0 or nr < nw, record transferred bytes
			// ignore errors
			if writeRes.Err != nil {
				return
			}
			if readRes.N != writeRes.N {
				return
			}
		}
		if readRes.Err != nil {
			// TODO filter use of closed network connection error and EOF
			if readRes.Err != io.EOF &&
				!strings.Contains(readRes.Err.Error(), "use of closed network connection") &&
				!strings.Contains(readRes.Err.Error(), "context canceled") {
				log.Printf("Read error: %v", readRes.Err)
			}
			return
		}
	}
}
