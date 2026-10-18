package editor

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

// logger receives diagnostics about servers, grammars and themes. It
// discards everything until SetLogFile succeeds.
var logger = log.New(io.Discard, "", 0)

// logPath is the file the logger writes to, empty when none.
var logPath string

// SetLogFile sends editor logs to path, creating its directory.
func SetLogFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	logger = log.New(f, "", 0)
	logPath = path

	return nil
}

// LogPath returns the current log file, empty when logging is off.
func LogPath() string {
	return logPath
}

// logf writes one timestamped line to the log file.
func (e *Editor) logf(format string, args ...any) {
	logger.Printf("%s %s", time.Now().Format("2006-01-02 15:04:05"), fmt.Sprintf(format, args...))
}
