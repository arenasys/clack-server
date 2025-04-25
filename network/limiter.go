package network

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/time/rate"
)

// Simulates network latency and limits bandwidth.
func LimiterMiddleware(latency time.Duration, limit int) mux.MiddlewareFunc {
	// limit is in bytes per second
	limiter := rate.NewLimiter(rate.Limit(limit), limit) // Burst equals bandwidthLimit

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Wrap the ResponseWriter to limit the write speed
			lbw := &LimiterResponseWriter{
				ResponseWriter: w,
				limiter:        limiter,
				latency:        latency,
				limit:          limit,
			}

			next.ServeHTTP(lbw, r)
		})
	}
}

type LimiterConn struct {
	net.Conn
	limiter *rate.Limiter
	latency time.Duration
}

func NewLimiterConn(conn net.Conn, limit int, latency time.Duration) *LimiterConn {
	return &LimiterConn{
		Conn:    conn,
		limiter: rate.NewLimiter(rate.Limit(limit), limit),
		latency: latency,
	}
}

func (lc *LimiterConn) Read(p []byte) (int, error) {
	time.Sleep(lc.latency / 2)
	n, err := lc.Conn.Read(p)
	if n > 0 {
		if err := lc.limiter.WaitN(context.Background(), n); err != nil {
			return n, err
		}
	}
	return n, err
}

func (lc *LimiterConn) Write(p []byte) (int, error) {
	time.Sleep(lc.latency / 2)
	total := 0
	for len(p) > 0 {
		// Limit chunk size to 1KB
		n := len(p)
		if n > 1024 {
			n = 1024
		}

		// Wait until the limiter allows writing n bytes
		if err := lc.limiter.WaitN(context.Background(), n); err != nil {
			return total, err
		}

		written, err := lc.Conn.Write(p[:n])
		total += written
		if err != nil {
			return total, err
		}

		p = p[n:]
	}
	return total, nil
}

type LimiterResponseWriter struct {
	http.ResponseWriter
	limiter *rate.Limiter
	latency time.Duration
	limit   int
}

func (lw *LimiterResponseWriter) Write(p []byte) (int, error) {
	time.Sleep(lw.latency)
	total := 0
	for len(p) > 0 {
		// Determine the chunk size based on the available rate
		n := len(p)
		if n > 1024 { // Limit chunk size to 1KB
			n = 1024
		}

		// Wait until the limiter allows writing n bytes
		err := lw.limiter.WaitN(context.Background(), n)
		if err != nil {
			return total, err
		}

		// Write the chunk
		written, err := lw.ResponseWriter.Write(p[:n])
		total += written
		if err != nil {
			return total, err
		}

		// Move to the next chunk
		p = p[n:]
	}
	return total, nil
}

func (lw *LimiterResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := lw.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not implement http.Hijacker")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, nil, err
	}

	time.Sleep(lw.latency)
	LimiterConn := NewLimiterConn(conn, lw.limit, lw.latency)

	return LimiterConn, rw, nil
}

func (lw *LimiterResponseWriter) Flush() {
	if flusher, ok := lw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (lw *LimiterResponseWriter) Push(target string, opts *http.PushOptions) error {
	if pusher, ok := lw.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, opts)
	}
	return http.ErrNotSupported
}
