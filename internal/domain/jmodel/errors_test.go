package jmodel

import (
	"errors"
	"fmt"
	"testing"
)

type statusErr struct{ code int }

func (e statusErr) Error() string       { return fmt.Sprintf("status %d", e.code) }
func (e statusErr) HTTPStatusCode() int { return e.code }

func TestStatusOf(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"plain", errors.New("boom"), 0},
		{"direct 404", statusErr{404}, 404},
		{"wrapped 500", fmt.Errorf("fetch: %w", statusErr{500}), 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StatusOf(tt.err); got != tt.want {
				t.Errorf("StatusOf = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(fmt.Errorf("x: %w", statusErr{404})) {
		t.Error("wrapped 404 should be NotFound")
	}
	if IsNotFound(statusErr{403}) {
		t.Error("403 is not NotFound")
	}
	if IsNotFound(nil) {
		t.Error("nil is not NotFound")
	}
}
