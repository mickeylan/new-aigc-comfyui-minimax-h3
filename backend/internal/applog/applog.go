// Package applog mirrors the backend's console output into a log file, so request timings and
// errors can still be checked after the console window is closed.
package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
)

// Setup appends the standard logger and gin's request log to <dataDir>/logs/console-YYYYMMDD.log
// while keeping the console output. It must run before the gin router is created, because
// gin.Logger() captures gin.DefaultWriter when it is constructed. The file is named after the
// start date; a process that runs for several days keeps writing to the same file.
func Setup(dataDir string, now time.Time) (io.Closer, string, error) {
	dir := filepath.Join(dataDir, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create log dir: %w", err)
	}
	path := filepath.Join(dir, "console-"+now.Format("20060102")+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, "", fmt.Errorf("open log file: %w", err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	gin.DefaultWriter = io.MultiWriter(os.Stdout, f)
	gin.DefaultErrorWriter = io.MultiWriter(os.Stderr, f)
	return f, path, nil
}
