package applog

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestSetupMirrorsStandardAndRequestLogsToDatedFile(t *testing.T) {
	prevLog, prevOut, prevErr := log.Writer(), gin.DefaultWriter, gin.DefaultErrorWriter
	t.Cleanup(func() { log.SetOutput(prevLog); gin.DefaultWriter = prevOut; gin.DefaultErrorWriter = prevErr })

	dir := t.TempDir()
	closer, path, err := Setup(dir, time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "logs", "console-20261116.log"); path != want {
		t.Fatalf("path=%q want %q", path, want)
	}

	log.Printf("standard-logger-line")
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Logger())
	r.POST("/api/projects/1/scenes/17/storyboard-sheet/anchors", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/projects/1/scenes/17/storyboard-sheet/anchors", nil))
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"standard-logger-line", "200", `POST     "/api/projects/1/scenes/17/storyboard-sheet/anchors"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("log file missing %q:\n%s", want, text)
		}
	}
}

func TestSetupAppendsInsteadOfTruncating(t *testing.T) {
	prevLog, prevOut, prevErr := log.Writer(), gin.DefaultWriter, gin.DefaultErrorWriter
	t.Cleanup(func() { log.SetOutput(prevLog); gin.DefaultWriter = prevOut; gin.DefaultErrorWriter = prevErr })

	dir := t.TempDir()
	day := time.Date(2026, 11, 16, 9, 0, 0, 0, time.Local)
	for _, line := range []string{"first-start", "second-start"} {
		closer, _, err := Setup(dir, day)
		if err != nil {
			t.Fatal(err)
		}
		log.Printf("%s", line)
		log.SetOutput(io.Discard)
		closer.Close()
	}
	data, _ := os.ReadFile(filepath.Join(dir, "logs", "console-20261116.log"))
	if !strings.Contains(string(data), "first-start") || !strings.Contains(string(data), "second-start") {
		t.Fatalf("restart must append:\n%s", data)
	}
}
