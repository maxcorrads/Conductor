package app

import (
	"fmt"
	"os"
	"time"
)

func (a *App) Logf(format string, args ...any) {
	file, err := os.OpenFile(a.Paths.Log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, "%s "+format+"\n", append([]any{time.Now().UTC().Format(time.RFC3339Nano)}, args...)...)
}
