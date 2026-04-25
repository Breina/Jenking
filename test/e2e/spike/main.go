//go:build integration

// spike/main.go: Throwaway program to verify vt10x renders jenking's alt-screen correctly.
// Run: go run -tags=integration ./test/e2e/spike/
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/creack/pty"
	"github.com/hinshun/vt10x"
)

func main() {
	binaryPath := "/tmp/jenking-test"
	if len(os.Args) > 1 {
		binaryPath = os.Args[1]
	}

	cols, rows := 160, 40

	cmd := exec.Command(binaryPath)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pty.Start: %v\n", err)
		os.Exit(1)
	}
	defer ptmx.Close()

	term := vt10x.New(vt10x.WithWriter(ptmx), vt10x.WithSize(cols, rows))

	// Parse PTY output in background
	done := make(chan struct{})
	go func() {
		defer close(done)
		r := bufio.NewReader(ptmx)
		for {
			if err := term.Parse(r); err != nil {
				return
			}
		}
	}()

	// Let jenking start and render (needs to connect to Jenkins)
	fmt.Fprintf(os.Stderr, "Waiting for jenking to render...\n")
	time.Sleep(5 * time.Second)

	// Print the current terminal grid
	term.Lock()
	c, r := term.Size()
	fmt.Printf("=== Terminal grid %dx%d ===\n", c, r)
	for row := 0; row < r; row++ {
		line := make([]rune, c)
		for col := 0; col < c; col++ {
			ch := term.Cell(col, row).Char
			if ch == 0 {
				ch = ' '
			}
			line[col] = ch
		}
		fmt.Printf("%s\n", string(line))
	}
	term.Unlock()

	// Quit
	fmt.Fprintf(os.Stderr, "Quitting...\n")
	ptmx.Write([]byte{3}) // Ctrl+C
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}
