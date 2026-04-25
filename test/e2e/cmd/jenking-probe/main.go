//go:build integration

// jenking-probe is an interactive REPL for driving jenking under a PTY.
// Intended for LLM-driven bug hunting: each command is issued over stdin,
// the result is printed to stdout, and the agent reacts to what it sees.
//
// Usage:
//
//	go run -tags=integration ./test/e2e/cmd/jenking-probe/ [binary-path]
//
// Protocol (one command per line):
//
//	start [context]         Start a fresh session. Uses current_context if omitted.
//	stop                    Stop the current session.
//	keys <string>           Send keystrokes (<cr>, <esc>, <c-c>, <up>, <down>, etc.)
//	type <literal>          Send literal runes without escape parsing.
//	resize <cols> <rows>    Resize the PTY.
//	wait <text> [timeoutMs] Wait for text to appear (default 10000ms).
//	snap [name]             Save snapshot to test/e2e/snapshots/ and print path.
//	grid                    Print current terminal grid.
//	diff <snap1> <snap2>    Line-diff two snapshot files.
//	log                     Print debug.log lines since last 'log' call.
//	dump [path]             Dump session command history as YAML.
//	exit                    Quit the REPL.
//
// Every command times out after 10 seconds. On timeout TIMEOUT: <grid> is printed.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/Breina/Jenking/test/e2e/harness"
)

const defaultTimeout = 10 * time.Second

type session struct {
	h         *harness.Harness
	tmpHome   string
	tmpCache  string
	history   []historyEntry
	logOffset int
}

type historyEntry struct {
	Command string `yaml:"cmd"`
	Args    string `yaml:"args,omitempty"`
}

func main() {
	binaryPath := os.Getenv("JENKING_BINARY")
	if len(os.Args) > 1 {
		binaryPath = os.Args[1]
	}
	if binaryPath == "" {
		binaryPath = buildBinary()
	}

	fmt.Fprintf(os.Stderr, "jenking-probe ready. Binary: %s\n", binaryPath)
	fmt.Fprintf(os.Stderr, "Commands: start [ctx], stop, keys, type, resize, wait, snap, grid, diff, log, dump, exit\n")
	fmt.Println("READY")

	var sess *session
	scanner := bufio.NewScanner(os.Stdin)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToLower(parts[0])
		args := ""
		if len(parts) > 1 {
			args = parts[1]
		}

		switch cmd {
		case "exit", "quit":
			if sess != nil && sess.h != nil {
				sess.h.Stop()
			}
			fmt.Println("OK: bye")
			return

		case "start":
			if sess != nil && sess.h != nil {
				sess.h.Stop()
			}
			ctx := strings.TrimSpace(args)
			var err error
			sess, err = startSession(binaryPath, ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERR: start failed: %v\n", err)
				fmt.Printf("ERR: %v\n", err)
				continue
			}
			record(sess, "start", ctx)
			fmt.Printf("OK: session started (context: %q)\n", ctx)

		case "stop":
			if sess == nil || sess.h == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			sess.h.Stop()
			sess = nil
			fmt.Println("OK: session stopped")

		case "keys":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			record(sess, "keys", args)
			sess.h.SendKeys(args)
			fmt.Println("OK")

		case "type":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			record(sess, "type", args)
			sess.h.SendRaw([]byte(args))
			fmt.Println("OK")

		case "resize":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			colsStr, rowsStr, _ := strings.Cut(args, " ")
			cols, err1 := strconv.Atoi(strings.TrimSpace(colsStr))
			rows, err2 := strconv.Atoi(strings.TrimSpace(rowsStr))
			if err1 != nil || err2 != nil {
				fmt.Printf("ERR: resize wants '<cols> <rows>', got %q\n", args)
				continue
			}
			record(sess, "resize", args)
			sess.h.Resize(cols, rows)
			fmt.Printf("OK: resized to %dx%d\n", cols, rows)

		case "wait":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			textPart, msStr, hasMs := strings.Cut(args, " ")
			timeout := defaultTimeout
			if hasMs {
				if ms, err := strconv.Atoi(strings.TrimSpace(msStr)); err == nil {
					timeout = time.Duration(ms) * time.Millisecond
				}
			}
			record(sess, "wait", args)
			if err := sess.h.WaitForText(textPart, timeout); err != nil {
				fmt.Printf("TIMEOUT: %v\n=== GRID ===\n%s=== END ===\n", err, sess.h.Grid())
			} else {
				fmt.Println("OK: found")
			}

		case "snap":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			name := strings.TrimSpace(args)
			record(sess, "snap", name)
			path := sess.h.Snapshot(name)
			fmt.Printf("OK: %s\n", path)

		case "grid":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			record(sess, "grid", "")
			fmt.Printf("=== GRID ===\n%s=== END ===\n", sess.h.Grid())

		case "diff":
			parts2 := strings.Fields(args)
			if len(parts2) < 2 {
				fmt.Println("ERR: diff needs two snapshot paths")
				continue
			}
			fmt.Println(harness.DiffSnapshots(parts2[0], parts2[1]))

		case "log":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			record(sess, "log", "")
			logPath := harness.DebugLogPath(sess.tmpHome)
			fmt.Printf("=== LOG ===\n%s\n=== END ===\n", harness.TailLog(logPath, 50))

		case "dump":
			if sess == nil {
				fmt.Println("ERR: no active session")
				continue
			}
			outPath := strings.TrimSpace(args)
			if outPath == "" {
				outPath = filepath.Join("test", "e2e", "snapshots",
					fmt.Sprintf("session-%s.yaml", time.Now().Format("20060102-150405")))
			}
			dumpHistory(sess, outPath)
			fmt.Printf("OK: %s\n", outPath)

		default:
			fmt.Printf("ERR: unknown command %q — try: start, stop, keys, type, resize, wait, snap, grid, diff, log, dump, exit\n", cmd)
		}
	}
}

func startSession(binaryPath, contextName string) (*session, error) {
	tmpHome, err := os.MkdirTemp("", "jenking-probe-home-*")
	if err != nil {
		return nil, fmt.Errorf("mkdirtemp home: %w", err)
	}
	tmpCache, err := os.MkdirTemp("", "jenking-probe-cache-*")
	if err != nil {
		os.RemoveAll(tmpHome)
		return nil, fmt.Errorf("mkdirtemp cache: %w", err)
	}

	env, err := harness.BakeConfigRaw(contextName, tmpHome, tmpCache)
	if err != nil {
		os.RemoveAll(tmpHome)
		os.RemoveAll(tmpCache)
		return nil, fmt.Errorf("bake config: %w", err)
	}

	h, err := harness.StartManual(binaryPath, env, tmpHome)
	if err != nil {
		os.RemoveAll(tmpHome)
		os.RemoveAll(tmpCache)
		return nil, fmt.Errorf("start harness: %w", err)
	}

	return &session{h: h, tmpHome: tmpHome, tmpCache: tmpCache}, nil
}

func record(sess *session, cmd, args string) {
	sess.history = append(sess.history, historyEntry{Command: cmd, Args: args})
}

func dumpHistory(sess *session, path string) {
	type scenario struct {
		Steps []historyEntry `yaml:"steps"`
	}
	sc := scenario{Steps: sess.history}
	data, err := yaml.Marshal(sc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "yaml marshal: %v\n", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
	}
}

func buildBinary() string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := findRepoRoot(thisFile)

	binPath := filepath.Join(os.TempDir(), fmt.Sprintf("jenking-probe-%d", os.Getpid()))
	fmt.Fprintf(os.Stderr, "Building jenking binary (repo: %s)...\n", repoRoot)

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/jenking")
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
		os.Exit(1)
	}
	return binPath
}

func findRepoRoot(fromFile string) string {
	dir := filepath.Dir(fromFile)
	for dir != "/" && dir != "." {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}
