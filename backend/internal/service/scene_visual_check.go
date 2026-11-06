package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
)

const maxStoryboardCheckImageBytes int64 = 20 << 20

var storyboardCheckCategories = map[string]bool{
	"orientation": true, "target": true, "functional_end": true, "contact": true,
	"occlusion": true, "composition": true, "story": true,
}

type SceneVisualCheckItem struct {
	Category   string `json:"category"`
	Subject    string `json:"subject"`
	Evidence   string `json:"evidence"`
	Suggestion string `json:"suggestion"`
}

type SceneVisualCheckResult struct {
	Status      string                 `json:"status"`
	Reason      string                 `json:"reason,omitempty"`
	Checks      []SceneVisualCheckItem `json:"checks"`
	Uncertainty []string               `json:"uncertainty"`
}

func (s *Service) HandleCheckSceneStoryboard(c *gin.Context) {
	scene, ok := s.loadScene(c)
	if !ok {
		return
	}
	if s.Cfg == nil || strings.TrimSpace(scene.ImageFile) == "" {
		unableToAssess(c, "当前场景尚无可检查的分镜图")
		return
	}
	_, imageHash, imageBytes, err := s.currentStoryboardImageSnapshot(scene)
	if err != nil {
		unableToAssess(c, "分镜图无法安全读取；请确认实际 ComfyUI input 目录可在本机访问，远程存储暂不支持此检查")
		return
	}
	if s.TextProviderFact == nil || !s.TextProviderFact.Capabilities().MultimodalMessages {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "multimodal_unsupported", "status": "unable_to_assess", "reason": "当前文本供应商未启用可用的图片理解能力，请配置视觉模型；本次未进行图片检查", "advisory": true, "persisted": false})
		return
	}
	provider, ok := s.TextProviderFact.Create().(MultimodalImageBytesProvider)
	if !ok {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"code": "multimodal_unsupported", "status": "unable_to_assess", "reason": "当前文本供应商未启用可用的图片理解能力，请配置视觉模型；本次未进行图片检查", "advisory": true, "persisted": false})
		return
	}
	sourceHash := sceneVisualSourceHash(scene, imageHash)
	result, err := checkSceneStoryboardBytes(provider, scene, imageBytes)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "视觉模型请求失败或返回格式无效，本次不能视为检查通过"})
		return
	}
	var current models.Scene
	if err := s.DB.Where("id = ? AND project_id = ?", scene.ID, scene.ProjectID).First(&current).Error; err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "检查期间正文、空间关系或图片已变化，请重新检查"})
		return
	}
	_, currentImageHash, _, err := s.currentStoryboardImageSnapshot(&current)
	if err != nil || sceneVisualSourceHash(&current, currentImageHash) != sourceHash {
		c.JSON(http.StatusConflict, gin.H{"error": "检查期间正文、空间关系或图片已变化，请重新检查"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"advisory": true, "persisted": false, "scene_id": scene.ID, "source_hash": sourceHash, "status": result.Status, "reason": result.Reason, "checks": result.Checks, "uncertainty": result.Uncertainty})
}

// Use the production upload root, not Storage.DataDir (which contains the
// application database). Remote SFTP files are not local image inputs.
func (s *Service) currentStoryboardImageSnapshot(scene *models.Scene) (string, [32]byte, []byte, error) {
	if s.Cfg == nil {
		return "", [32]byte{}, nil, errors.New("storage not configured")
	}
	root := s.Cfg.Storage.DataDir
	if s.Upload != nil {
		if s.Upload.remote != nil && s.Upload.remote.Enabled() {
			return "", [32]byte{}, nil, errors.New("remote input is not locally accessible")
		}
		root = filepath.Dir(filepath.Clean(s.Upload.InputDir()))
	} else if s.Cfg.Comfy.ComfyDir != "" {
		root = s.Cfg.Comfy.ComfyDir
	}
	return secureStoryboardImageSnapshot(root, scene.ProjectID, scene.ImageFile)
}

func unableToAssess(c *gin.Context, reason string) {
	c.JSON(http.StatusUnprocessableEntity, gin.H{"status": "unable_to_assess", "reason": reason, "advisory": true, "persisted": false})
}

func sceneStoryboardPrompts(scene *models.Scene) (string, string) {
	system := `Inspect the supplied storyboard image against only the current Scene text and saved blocking context. Return exactly one JSON object: {"status":"issues_found|no_obvious_issues|unable_to_assess","reason":"","checks":[{"category":"orientation|target|functional_end|contact|occlusion|composition|story","subject":"","evidence":"","suggestion":""}],"uncertainty":[]}.
Do not modify or extend the story. Do not reason from the filename and do not invent appearance. Report only visible evidence. A still image cannot prove actual movement: when movement is expected, assess only whether the expected direction is visually indicated, if visible. Use unable_to_assess when the image cannot support an assessment. Maximum 30 checks; every evidence and suggestion must be nonblank.`
	return system, "CURRENT Scene text:\n" + scene.Content + "\n\nCURRENT saved blocking only:\n" + sceneBlockingPromptContext(scene)
}

func checkSceneStoryboard(provider MultimodalTextProvider, scene *models.Scene, imagePath string) (SceneVisualCheckResult, error) {
	system, user := sceneStoryboardPrompts(scene)
	raw, err := provider.ChatWithImage(system, user, imagePath)
	return parseSceneStoryboardResponse(raw, err)
}

func checkSceneStoryboardBytes(provider MultimodalImageBytesProvider, scene *models.Scene, imageBytes []byte) (SceneVisualCheckResult, error) {
	system, user := sceneStoryboardPrompts(scene)
	raw, err := provider.ChatWithImageBytes(system, user, imageBytes)
	return parseSceneStoryboardResponse(raw, err)
}

func parseSceneStoryboardResponse(raw string, err error) (SceneVisualCheckResult, error) {
	if err != nil {
		return SceneVisualCheckResult{}, err
	}
	if len(raw) > 128<<10 {
		return SceneVisualCheckResult{}, errors.New("assessment response too large")
	}
	var result SceneVisualCheckResult
	dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return result, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return result, errors.New("extra JSON content")
	}
	if err := validateSceneVisualCheck(result); err != nil {
		return result, err
	}
	return result, nil
}

func validateSceneVisualCheck(r SceneVisualCheckResult) error {
	if r.Status != "issues_found" && r.Status != "no_obvious_issues" && r.Status != "unable_to_assess" {
		return errors.New("invalid status")
	}
	if len(r.Checks) > 30 || len(r.Uncertainty) > 30 || len(r.Reason) > 2000 {
		return errors.New("assessment too large")
	}
	if r.Status == "issues_found" && len(r.Checks) == 0 || r.Status == "no_obvious_issues" && len(r.Checks) != 0 || r.Status == "unable_to_assess" && len(r.Checks) != 0 {
		return errors.New("checks conflict with status")
	}
	if r.Status == "unable_to_assess" && strings.TrimSpace(r.Reason) == "" {
		return errors.New("unable assessment requires reason")
	}
	for _, v := range r.Uncertainty {
		if len(v) > 2000 {
			return errors.New("uncertainty too large")
		}
	}
	for _, v := range r.Checks {
		if !storyboardCheckCategories[v.Category] || strings.TrimSpace(v.Evidence) == "" || strings.TrimSpace(v.Suggestion) == "" || len(v.Subject) > 2000 || len(v.Evidence) > 2000 || len(v.Suggestion) > 2000 {
			return errors.New("invalid check")
		}
	}
	return nil
}

func secureStoryboardImage(dataDir string, projectID uint, imageFile string) (string, [32]byte, error) {
	path, hash, _, err := secureStoryboardImageSnapshot(dataDir, projectID, imageFile)
	return path, hash, err
}

func secureStoryboardImageSnapshot(dataDir string, projectID uint, imageFile string) (string, [32]byte, []byte, error) {
	var zero [32]byte
	root := filepath.Join(dataDir, "input", fmt.Sprint(projectID))
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(imageFile)))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", zero, nil, errors.New("unsafe image path")
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", zero, nil, err
	}
	joined := filepath.Join(root, clean)
	pathReal, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", zero, nil, err
	}
	rel, err := filepath.Rel(rootReal, pathReal)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", zero, nil, errors.New("image escapes project input")
	}
	f, err := os.Open(pathReal)
	if err != nil {
		return "", zero, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxStoryboardCheckImageBytes {
		return "", zero, nil, errors.New("invalid image")
	}
	postReal, evalErr := filepath.EvalSymlinks(joined)
	postInfo, statErr := os.Stat(postReal)
	if evalErr != nil || statErr != nil || postReal != pathReal || !os.SameFile(info, postInfo) {
		return "", zero, nil, errors.New("image changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxStoryboardCheckImageBytes+1))
	if err != nil || int64(len(data)) > maxStoryboardCheckImageBytes || !validStoryboardSignature(data) {
		return "", zero, nil, errors.New("invalid image")
	}
	return pathReal, sha256.Sum256(data), data, nil
}

func validStoryboardSignature(b []byte) bool {
	return len(b) >= 12 && (string(b[:8]) == "\x89PNG\r\n\x1a\n" || b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff || string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP")
}

func sceneVisualSourceHash(scene *models.Scene, imageHash [32]byte) string {
	story := sha256.Sum256([]byte(scene.Content))
	blocking := sha256.Sum256([]byte(sceneBlockingPromptContext(scene)))
	imageID := sha256.Sum256([]byte(scene.ImageFile))
	identity := fmt.Sprintf("%d:%d:%d", scene.ID, scene.ProjectID, scene.Generation)
	sum := sha256.Sum256([]byte(identity + ":" + hex.EncodeToString(story[:]) + ":" + hex.EncodeToString(blocking[:]) + ":" + hex.EncodeToString(imageID[:]) + ":" + hex.EncodeToString(imageHash[:])))
	return hex.EncodeToString(sum[:])
}
