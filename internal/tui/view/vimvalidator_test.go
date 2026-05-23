package view

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Breina/Jenking/internal/jenkins"
)

type stubValidator struct {
	calls  atomic.Int32
	result jenkins.ValidationResult
	err    error
}

func (s *stubValidator) ValidateJenkinsfile(_ context.Context, _ string) (jenkins.ValidationResult, error) {
	s.calls.Add(1)
	return s.result, s.err
}

func TestRunVimValidator_WritesErrorsOnChange(t *testing.T) {
	dir := t.TempDir()
	rt := &vimRuntime{
		dir:       dir,
		bufferTmp: filepath.Join(dir, "buffer.groovy"),
		errorsQF:  filepath.Join(dir, "errors.qf"),
	}
	stub := &stubValidator{result: jenkins.ValidationResult{
		OK:     false,
		Issues: []jenkins.ValidationIssue{{Line: 7, Col: 3, Message: "boom"}},
	}}

	if err := os.WriteFile(rt.bufferTmp, []byte("pipeline { }\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go runVimValidator(ctx, stub, rt, done, 20*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	var got []byte
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(rt.errorsQF); err == nil && len(data) > 0 {
			got = data
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(done)

	if stub.calls.Load() == 0 {
		t.Fatalf("validator never invoked")
	}
	want := rt.bufferTmp + ":7:3: boom"
	if !strings.Contains(string(got), want) {
		t.Errorf("errors.qf missing expected line %q, got:\n%s", want, got)
	}
}

func TestRunVimValidator_ClearsOnPass(t *testing.T) {
	dir := t.TempDir()
	rt := &vimRuntime{
		dir:       dir,
		bufferTmp: filepath.Join(dir, "buffer.groovy"),
		errorsQF:  filepath.Join(dir, "errors.qf"),
	}
	stub := &stubValidator{result: jenkins.ValidationResult{OK: true}}

	if err := os.WriteFile(rt.bufferTmp, []byte("pipeline { agent any }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rt.errorsQF, []byte("stale:1: old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go runVimValidator(ctx, stub, rt, done, 20*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(rt.errorsQF); err == nil && len(data) == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	close(done)

	data, _ := os.ReadFile(rt.errorsQF)
	if len(data) != 0 {
		t.Errorf("expected empty errors.qf after clean validation, got %q", data)
	}
}
