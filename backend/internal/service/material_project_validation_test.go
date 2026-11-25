package service

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// Nil services deliberately prove invalid IDs cannot reach persistence or media processing.
func TestMaterialHandlersRejectInvalidProjectBeforeSideEffects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []string{"", "0", "abc", "-1", "1.5", "4294967296"} {
		t.Run("list/"+value, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(rec)
			ctx.Request = httptest.NewRequest("GET", "/api/materials?project_id="+value, nil)
			(&Service{}).HandleListMaterials(ctx)
			if rec.Code != 400 || !strings.Contains(rec.Body.String(), "project_id") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
		for _, kind := range []string{"video", "3d"} {
			t.Run("upload/"+kind+"/"+value, func(t *testing.T) {
				var body bytes.Buffer
				writer := multipart.NewWriter(&body)
				if err := writer.WriteField("project_id", value); err != nil {
					t.Fatal(err)
				}
				if err := writer.WriteField("type", kind); err != nil {
					t.Fatal(err)
				}
				part, err := writer.CreateFormFile("file", "fixture.mp4")
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write([]byte("fixture only")); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(rec)
				ctx.Request = httptest.NewRequest("POST", "/api/materials", &body)
				ctx.Request.Header.Set("Content-Type", writer.FormDataContentType())
				(&Service{}).HandleUploadMaterial(ctx)
				if rec.Code != 400 || !strings.Contains(rec.Body.String(), "project_id") {
					t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
				}
			})
		}
	}
}

func TestMaterialUploadAbsentProjectPreservesValidationPath(t *testing.T) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest("POST", "/api/materials", nil)
	(&Service{}).HandleUploadMaterial(ctx)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "file required") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
