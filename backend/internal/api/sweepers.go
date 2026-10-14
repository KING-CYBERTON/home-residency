package api

import (
	"context"
	"time"
)

// RunSweepers runs the periodic jobs that the SQS consumers and EventBridge schedules will own once
// they exist. Each pass is idempotent, so running it on every instance is safe.
func (s *Server) RunSweepers(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.ReprocessCallbacks(ctx)
		}
	}
}
