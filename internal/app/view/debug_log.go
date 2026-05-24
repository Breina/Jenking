package view

import (
	"fmt"
	"log"
	"os"
	"sync"
)

var (
	dbgOnce   sync.Once
	dbgLogger *log.Logger
)

func dbg(format string, args ...any) {
	dbgOnce.Do(func() {
		f, err := os.OpenFile("/tmp/jenking-debug.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return
		}
		dbgLogger = log.New(f, "", log.Ltime|log.Lmicroseconds)
	})
	if dbgLogger != nil {
		dbgLogger.Output(2, fmt.Sprintf(format, args...))
	}
}
