package pkg

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

func ListenAndServeWithGracefulShutdown(server *http.Server) {
	shutdownCtx, shutdownCancelFunc = context.WithCancel(context.Background())
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	<-stop
	log.Println("shutting down server...")
	shutdownCancelFunc()

	// create a deadline for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// attempt graceful shutdown
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("error during server shutdown: %s", err.Error())
	}

	// wait for hijacked connections to finish
	for atomic.LoadInt64(&ActiveConnections) > 0 {
		log.Printf("waiting for %d hijacked connections to finish", atomic.LoadInt64(&ActiveConnections))
		time.Sleep(200 * time.Millisecond)
	}

	// wait for the server goroutine to finish
	wg.Wait()

	log.Println("server gracefully stopped")
}
