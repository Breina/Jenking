package jmodel

import "errors"

// HTTPStatusError is implemented by adapter errors that carry an HTTP status
// code. It lets the domain and app layers react to specific statuses (e.g.
// enrich a 404) without importing an adapter package.
type HTTPStatusError interface{ HTTPStatusCode() int }

// StatusOf returns the HTTP status code carried by err via HTTPStatusError, or
// 0 if no such error is present in the chain.
func StatusOf(err error) int {
	var se HTTPStatusError
	if errors.As(err, &se) {
		return se.HTTPStatusCode()
	}
	return 0
}

// IsNotFound reports whether err carries an HTTP 404 status.
func IsNotFound(err error) bool { return StatusOf(err) == 404 }
