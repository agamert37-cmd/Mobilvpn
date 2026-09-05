package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

var nodeIDSanitizer = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func sanitizeNodeID(id string) string {
	return nodeIDSanitizer.ReplaceAllString(id, "")
}

// newSessionID mints a globally-unique, cryptographically random session
// token prefixed with the owning node's (sanitized) ID. That prefix is what
// lets a later /disconnect or /telemetry call figure out — statelessly,
// with no extra lookup table — whether the session belongs to this node or
// must be proxied to a fleet sibling (see server.go's findOwningNode).
func newSessionID(nodeID string) (string, error) {
	buf := make([]byte, 12) // 96 bits of entropy
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("httpapi: generating session id: %w", err)
	}
	return "sess_" + sanitizeNodeID(nodeID) + "_" + hex.EncodeToString(buf), nil
}
