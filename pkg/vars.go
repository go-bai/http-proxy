package pkg

import (
	"context"
	"time"
)

var (
	ActiveConnections  int64
	shutdownCtx        context.Context
	shutdownCancelFunc context.CancelFunc

	dialDestTimeout = 10 * time.Second
)
