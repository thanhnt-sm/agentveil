package proxy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/vurakit/agentveil/internal/vault"
)

// sseRehydrator wraps an SSE response body and rehydrates PII tokens line-by-line
type sseRehydrator struct {
	source    io.ReadCloser // P1 FIX #5: store original body for proper Close()
	reader    *bufio.Scanner
	vault     *vault.Vault
	sessionID string
	mappings  map[string]string
	replacer  *strings.Replacer // BUG-04 FIX: O(N+M) Aho-Corasick
	loaded    bool
	buf       *bytes.Buffer
	done      bool
}

func newSSERehydrator(body io.ReadCloser, v *vault.Vault, sessionID string) io.ReadCloser {
	return &sseRehydrator{
		source:    body,
		reader:    bufio.NewScanner(body),
		vault:     v,
		sessionID: sessionID,
		buf:       &bytes.Buffer{},
	}
}

func (s *sseRehydrator) Read(p []byte) (int, error) {
	// If we have buffered data, return it first
	if s.buf.Len() > 0 {
		return s.buf.Read(p)
	}

	if s.done {
		return 0, io.EOF
	}

	// Lazy-load mappings on first read
	if !s.loaded {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		mappings, err := s.vault.LookupAll(ctx, s.sessionID)
		if err != nil {
			slog.Warn("failed to load vault mappings for SSE", "error", err, "session", s.sessionID)
		}
		s.mappings = mappings
		// BUG-04 FIX: build Aho-Corasick replacer once
		if len(mappings) > 0 {
			pairs := make([]string, 0, len(mappings)*2)
			for token, original := range mappings {
				pairs = append(pairs, token, original)
			}
			s.replacer = strings.NewReplacer(pairs...)
		}
		s.loaded = true
	}

	// Read next SSE line
	if !s.reader.Scan() {
		s.done = true
		if err := s.reader.Err(); err != nil {
			return 0, err
		}
		return 0, io.EOF
	}

	line := s.reader.Text()

	// Rehydrate any PII tokens found in this SSE line
	if s.replacer != nil && strings.Contains(line, "[") {
		line = s.replacer.Replace(line)
	}

	// Write the processed line + newline to buffer
	s.buf.WriteString(line)
	s.buf.WriteByte('\n')

	return s.buf.Read(p)
}

// P1 FIX #5: Close the underlying response body to prevent connection leaks
func (s *sseRehydrator) Close() error {
	if s.source != nil {
		return s.source.Close()
	}
	return nil
}
