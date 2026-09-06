package mcp

import (
	"errors"
	"sync"

	"github.com/Breina/Jenking/internal/domain/jmodel"
)

// capabilities tracks which plugin-gated endpoints this controller lacks,
// discovered lazily: the first tool call to a missing endpoint is the probe.
// State is per-session and never persisted — a plugin installed while the
// server runs simply is not re-probed until restart, which is an acceptable
// trade for not hammering a 404 on every call.
type capabilities struct {
	mu      sync.Mutex
	missing map[string]string // capability name -> actionable hint
}

func newCapabilities() *capabilities {
	return &capabilities{missing: map[string]string{}}
}

// reason returns the recorded hint if the capability is already known missing.
func (c *capabilities) reason(name string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	h, ok := c.missing[name]
	return h, ok
}

func (c *capabilities) markMissing(name, hint string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.missing[name] = hint
}

// gateBefore short-circuits a gated tool when its capability is already known
// missing, returning the actionable hint as an error without a network round
// trip. Returns nil when the capability is untested or present.
func (s *Server) gateBefore(name string) error {
	if hint, missing := s.caps.reason(name); missing {
		return errors.New(hint)
	}
	return nil
}

// gateAfter inspects a gated call's error. A 404 means the plugin endpoint is
// absent: it records the capability as missing and returns the actionable hint.
// For any other error (including nil) it returns nil, leaving the caller to
// decide how to handle non-capability failures.
func (s *Server) gateAfter(name, hint string, err error) error {
	if err != nil && jmodel.IsNotFound(err) {
		s.caps.markMissing(name, hint)
		return errors.New(hint)
	}
	return nil
}
