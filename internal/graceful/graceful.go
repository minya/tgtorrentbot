// Package graceful runs an http.Server until SIGTERM or SIGINT and then
// shuts it down cleanly, so a docker stop or redeploy lets in-flight
// requests (a torrent add and its labels, a data deletion, a move_media)
// finish instead of cutting them mid-way.
package graceful

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/minya/logger"
)

// Timeout is how long shutdown waits for in-flight work. It stays below
// Docker's default 10 s stop grace period, after which Docker sends SIGKILL.
const Timeout = 8 * time.Second

// SignalContext returns a context that is done when the process receives
// SIGTERM or SIGINT. Only the first signal is caught: once the context is
// done, signals have their default effect again, so a second Ctrl-C or a
// supervisor's repeated SIGTERM ends a shutdown that is still waiting.
// Each binary creates it once and passes it to Serve and to its background
// work.
func SignalContext() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx
}

// Serve runs srv until ctx is done, usually SignalContext, then shuts it
// down within Timeout (see ServeContext). It returns nil after a clean
// shutdown and the listener's error if the server fails to start or run.
func Serve(ctx context.Context, srv *http.Server, drain func(context.Context)) error {
	return ServeContext(ctx, srv, drain, Timeout)
}

// ServeContext runs srv until ctx is done, then stops accepting
// connections and waits up to timeout for active requests (srv.Shutdown).
// drain, when set, then waits for work the handlers started in the
// background, under the same deadline. Connections still open at the
// deadline, such as long-lived event streams, are closed.
func ServeContext(ctx context.Context, srv *http.Server, drain func(context.Context), timeout time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	logger.Info("Shutting down: waiting up to %s for in-flight requests", timeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Warn("Shutdown did not finish in time (%v); closing remaining connections", err)
		_ = srv.Close()
	}
	if drain != nil {
		drain(shutdownCtx)
	}
	logger.Info("Shutdown complete")
	return nil
}
