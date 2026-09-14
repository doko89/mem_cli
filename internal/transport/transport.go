// Package transport serves the mem MCP server over stdio and HTTP.
package transport

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/time/rate"

	"mem_cli/internal/mcpserver"
)

// Options controls how the MCP server is exposed.
type Options struct {
	// DatabasePath is the SQLite database the tools operate on.
	DatabasePath string
	// HTTPAddr, when non-empty, serves Streamable HTTP on this address
	// (e.g. "127.0.0.1:8090"). When empty, the server runs over stdio.
	HTTPAddr string
	// TokenFile is where the HTTP bearer token is stored / loaded from.
	// Defaults to ~/.mem_cli/http_token.
	TokenFile string
	// RateLimit is requests per minute per client IP for HTTP. 0 = default 60.
	RateLimit int
	// MaxBodyBytes limits request body size for HTTP. 0 = default 2 MiB.
	MaxBodyBytes int64
}

const (
	defaultRateLimit = 60
	defaultMaxBody   = 2 << 20 // 2 MiB
)

// Serve runs the MCP server on the transport selected by opts and blocks
// until the context is cancelled or the transport fails.
func Serve(ctx context.Context, opts Options) error {
	server := mcpserver.New(opts.DatabasePath)

	if strings.TrimSpace(opts.HTTPAddr) == "" {
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	return serveHTTP(ctx, server, opts)
}

func serveHTTP(ctx context.Context, server *mcp.Server, opts Options) error {
	token, err := loadOrCreateToken(opts.TokenFile)
	if err != nil {
		return err
	}

	limit := opts.RateLimit
	if limit <= 0 {
		limit = defaultRateLimit
	}
	maxBody := opts.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxBody
	}

	limiter := newLimiter(limit)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		if !limiter.allow(clientIP(r)) {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		if !bearerOK(r, token) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mem_cli"`)
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		handler.ServeHTTP(w, r)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"mem_cli-mcp","status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              opts.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	listener, err := net.Listen("tcp", opts.HTTPAddr)
	if err != nil {
		return fmt.Errorf("unable to bind %s: %w", opts.HTTPAddr, err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(listener) }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// bearerOK validates the Authorization header against the configured token
// using a constant-time comparison.
func bearerOK(r *http.Request, token []byte) bool {
	const prefix = "Bearer "
	auth := r.Header.Get("Authorization")
	if len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return false
	}
	got := []byte(strings.TrimSpace(auth[len(prefix):]))
	return subtle.ConstantTimeCompare(got, token) == 1
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// limiter is a tiny per-IP token bucket.
type limiter struct {
	perMinute int
	buckets   map[string]*visitor
}

type visitor struct {
	limiter *rate.Limiter
	last    time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{perMinute: perMinute, buckets: map[string]*visitor{}}
}

func (l *limiter) allow(ip string) bool {
	v, ok := l.buckets[ip]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(rate.Every(time.Minute/time.Duration(l.perMinute)), l.perMinute)}
		l.buckets[ip] = v
	}
	v.last = time.Now()
	return v.limiter.Allow()
}

// loadOrCreateToken returns the HTTP bearer token, generating and persisting
// one (file mode 0600) on first use.
func loadOrCreateToken(tokenFile string) ([]byte, error) {
	if strings.TrimSpace(tokenFile) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("unable to resolve home directory: %w", err)
		}
		tokenFile = filepath.Join(home, ".mem_cli", "http_token")
	}
	if raw, err := os.ReadFile(tokenFile); err == nil {
		token := strings.TrimSpace(string(raw))
		if token != "" {
			return []byte(token), nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("unable to generate token: %w", err)
	}
	token := []byte(hex.EncodeToString(raw))
	if err := os.MkdirAll(filepath.Dir(tokenFile), 0o700); err != nil {
		return nil, fmt.Errorf("unable to create config directory: %w", err)
	}
	if err := os.WriteFile(tokenFile, token, 0o600); err != nil {
		return nil, fmt.Errorf("unable to store token: %w", err)
	}
	return token, nil
}
