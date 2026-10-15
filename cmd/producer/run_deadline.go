package main

import (
	"context"
	"errors"
	"time"

	"github.com/rudderlabs/rudder-go-kit/logger"
)

// errRunDeadlineReached is the cause of the context returned by withRunDeadline once RUN_DEADLINE passes.
var errRunDeadlineReached = errors.New("run deadline reached")

// withRunDeadline returns the context that stops the load: one done when ctx is done (SIGTERM) or, if deadline isn't
// zero, once deadline passes, with errRunDeadlineReached as its cause. A deadline already past returns a context that
// is already done. Without a deadline it returns ctx itself, so nothing changes.
//
// It logs once when a deadline is set and once when it's reached; it doesn't log a reached deadline if ctx was done
// first. stop releases the context and waits for the goroutine that logs.
func withRunDeadline(
	ctx context.Context, log logger.Logger, deadline time.Time,
) (stopCtx context.Context, stop func()) {
	if deadline.IsZero() {
		return ctx, func() {}
	}

	log.Infon("Run deadline set, no requests will be sent after it",
		logger.NewTimeField("runDeadline", deadline),
		logger.NewDurationField("timeLeft", time.Until(deadline).Round(time.Second)),
	)
	stopCtx, cancel := context.WithDeadlineCause(ctx, deadline, errRunDeadlineReached)
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-stopCtx.Done()
		// The cause is set once, by whichever of the deadline, ctx or stop came first.
		if isRunDeadlineReached(stopCtx) {
			log.Infon("Run deadline reached, no more requests will be sent",
				logger.NewTimeField("runDeadline", deadline),
			)
		}
	}()
	return stopCtx, func() {
		cancel()
		<-done
	}
}

// isRunDeadlineReached reports whether ctx was done because RUN_DEADLINE passed.
func isRunDeadlineReached(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errRunDeadlineReached)
}
