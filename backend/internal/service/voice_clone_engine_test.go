package service

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"comfyui-console/internal/config"
	"comfyui-console/internal/models"
)

// newVoiceUploadService builds a Service whose uploads land in a temp asset root, so
// 参考语音上传 can be exercised without ComfyUI, an aliyun key, or a task service.
// indexTTSEnabled selects which dubbing engine the handler should believe is active.
func newVoiceUploadService(t *testing.T, indexTTSEnabled bool) (*Service, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Project{}, &models.Character{}, &models.Dialogue{}, &models.UploadFile{}); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Storage.AssetDir = t.TempDir()
	cfg.IndexTTS.Enabled = indexTTSEnabled
	cfg.IndexTTS.ExecutionMode = "comfyui"
	return &Service{
		DB:       db,
		Cfg:      cfg,
		Upload:   NewUploadManager(cfg, db, NewRemoteExec(config.RemoteConfig{})),
		Projects: &ProjectService{db: db, cfg: cfg},
	}, db
}

func voiceUploadContext(t *testing.T, params gin.Params, filename string, data []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/upload", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Params = params
	return c, rec
}

func decodeVoiceResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return payload
}

// IndexTTS 直接引用参考语音文件，所以上传不应再去注册阿里云复刻音色，
// 更不应该因为缺少云端 Key 而报一条与当前配音管线无关的警告。
func TestVoiceUploadSkipsCloudCloneWhenIndexTTSDubs(t *testing.T) {
	svc, db := newVoiceUploadService(t, true)
	project := models.Project{Title: "配音"}
	db.Create(&project)
	character := models.Character{ProjectID: project.ID, Name: "柳石"}
	db.Create(&character)

	params := gin.Params{{Key: "id", Value: "1"}, {Key: "cid", Value: "1"}}
	c, rec := voiceUploadContext(t, params, "ref.wav", []byte("fake-wav-bytes"))
	svc.HandleUploadCharacterVoice(c)

	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeVoiceResponse(t, rec)
	if warning, ok := payload["warning"]; ok {
		t.Fatalf("IndexTTS 配音不该出现音色注册警告: %v", warning)
	}
	if payload["voice_ref"] == "" {
		t.Fatal("参考语音文件未保存")
	}
	message, _ := payload["message"].(string)
	if message == "" || !bytes.Contains([]byte(message), []byte("IndexTTS")) {
		t.Fatalf("成功提示未说明音色来源: %q", message)
	}

	var saved models.Character
	if err := db.First(&saved, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VoiceRef == "" {
		t.Fatal("Character.VoiceRef 未写入，IndexTTS 将找不到音色来源")
	}
	if saved.VoiceID != "" || saved.VoiceModel != "" {
		t.Fatalf("不该写入云端复刻音色: id=%q model=%q", saved.VoiceID, saved.VoiceModel)
	}
}

// 关闭 IndexTTS 后配音确实走云端复刻音色，这时缺少 Key 的警告仍然要保留。
func TestVoiceUploadStillWarnsWhenCloudCloneIsTheDubbingPath(t *testing.T) {
	svc, db := newVoiceUploadService(t, false)
	project := models.Project{Title: "配音"}
	db.Create(&project)
	character := models.Character{ProjectID: project.ID, Name: "柳石"}
	db.Create(&character)

	params := gin.Params{{Key: "id", Value: "1"}, {Key: "cid", Value: "1"}}
	c, rec := voiceUploadContext(t, params, "ref.wav", []byte("fake-wav-bytes"))
	svc.HandleUploadCharacterVoice(c)

	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeVoiceResponse(t, rec)
	warning, _ := payload["warning"].(string)
	if warning == "" {
		t.Fatal("云端配音路径下缺少 Key 应当给出警告")
	}
	var saved models.Character
	if err := db.First(&saved, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.VoiceRef == "" {
		t.Fatal("注册失败也必须保留已保存的参考语音")
	}
}

// 「重试注册音色」在 IndexTTS 下是无意义操作，应当直接说明而不是去打云端请求。
func TestRetryVoiceCloneIsRejectedWhenIndexTTSDubs(t *testing.T) {
	svc, db := newVoiceUploadService(t, true)
	project := models.Project{Title: "配音"}
	db.Create(&project)
	character := models.Character{ProjectID: project.ID, Name: "柳石", VoiceRef: "ref.wav"}
	db.Create(&character)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/clone", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}, {Key: "cid", Value: "1"}}
	svc.HandleCloneCharacterVoice(c)

	if rec.Code != 400 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	payload := decodeVoiceResponse(t, rec)
	errText, _ := payload["error"].(string)
	if !bytes.Contains([]byte(errText), []byte("IndexTTS")) {
		t.Fatalf("拒绝理由未说明当前配音引擎: %q", errText)
	}
}
