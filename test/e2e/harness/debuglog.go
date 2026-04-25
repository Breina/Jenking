//go:build integration

package harness

import (
	"bufio"
	"os"
	"strings"
)

// panicSignatures are strings that indicate a crash in the debug log.
var panicSignatures = []string{
	"panic:",
	"runtime error:",
	"fatal error:",
	"goroutine ",
}

// redactPatterns are substrings that should be redacted from log output.
var redactPatterns = []string{
	"Authorization:",
	"token:",
	"Token:",
}

// ScanPanics reads the debug log at path and returns any lines that indicate
// a panic or runtime error. Authorization headers are redacted.
func ScanPanics(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var panics []string
	inPanic := false
	panicLines := 0

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := redact(scanner.Text())
		for _, sig := range panicSignatures {
			if strings.Contains(line, sig) {
				inPanic = true
				panicLines = 0
				break
			}
		}
		if inPanic && panicLines < 30 {
			panics = append(panics, line)
			panicLines++
		}
	}
	return panics
}

// TailLog returns the last n lines of the log at path, with sensitive values redacted.
func TailLog(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return "(debug.log not found)"
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, redact(scanner.Text()))
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func redact(line string) string {
	for _, pat := range redactPatterns {
		if idx := strings.Index(line, pat); idx != -1 {
			// Redact from the pattern to end of line
			line = line[:idx+len(pat)] + " [REDACTED]"
			break
		}
	}
	return line
}
