package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	FrameReferenceOriginal = "original"
	FrameReferenceClay     = "clay"
	FrameReferenceLineart  = "lineart"
	// FrameReferenceHD is a ComfyUI VOSR2 upscale of the tail frame; unlike clay and line art it
	// is still the full-colour opening picture of the next Scene.
	FrameReferenceHD = "hd"

	continuityFrameClaySkillCode    = "continuity-frame-clay"
	continuityFrameLineartSkillCode = "continuity-frame-lineart"

	// Conversion must keep the frame's aspect ratio; both the chosen Qwen canvas and the
	// returned image may differ from the source by at most this fraction.
	frameConvertMaxRatioDrift = 0.04
)

// continuityFrameClaySkill and continuityFrameLineartSkill are image-edit prompts sent to Qwen Edit
// as is. Wording is user-approved; raise Version whenever it changes.
var continuityFrameClaySkill = models.Skill{
	Name: "尾帧白模参考", Code: continuityFrameClaySkillCode, Operation: continuityFrameClaySkillCode, Version: 1,
	Description:  "把上一镜尾帧转成同构图的白色黏土模型，只约束姿势、站位、遮挡与场景几何（图像编辑提示词）",
	Stage:        models.SkillStageQwenImageEdit,
	SystemPrompt: "把参考图转换成同一画面的纯白哑光黏土模型渲染：只改颜色与表面材质，不重新构图。画幅比例、机位、透视、景别与裁切保持不变，不镜像、不扩展画面、不换角度。人物数量与各自位置不变，保留每个人的身体与头发轮廓、体型、服装体积和主要褶皱、头部朝向、手脚姿势、人物间距离、接触与前后遮挡；不增减人物、肢体或道具。场景的建筑、门窗、地面、家具、植物和所有可见道具保持原有形状、大小、位置与前后层次，背景完整保留，不换成空棚。所有表面统一为无纹理、无原色、无图案、无金属反光的白色黏土，柔和漫射光，保留形体明暗以区分前后物体；不添加文字、标注、边框或水印。",
	IsSystem:     true, Enabled: true, SortOrder: 31,
}

var continuityFrameLineartSkill = models.Skill{
	Name: "尾帧线稿参考", Code: continuityFrameLineartSkillCode, Operation: continuityFrameLineartSkillCode, Version: 1,
	Description:  "把上一镜尾帧转成同构图的黑线白底结构线稿，只约束姿势、站位、遮挡与场景几何（图像编辑提示词）",
	Stage:        models.SkillStageQwenImageEdit,
	SystemPrompt: "把参考图转换成同一画面的黑线白底结构线稿，不重新构图。画幅比例、机位、透视、景别与裁切保持不变，不镜像、不扩展画面、不换角度、不拼图。保留所有人物的数量与各自位置、五官位置与朝向、发型轮廓、体型、服装造型与主要褶皱、头部与手指手脚姿势、接触动作、人物间距离及前后遮挡。建筑、门窗、家具、帷幔、植物和道具的结构、大小、位置与前后层次用轮廓线完整画出，背景不得省略。线条为连续、干净的黑色或深灰细线，纸面纯白；去掉颜色、贴图、照片纹理、渐变明暗、投影和大面积黑块，不用密集排线或素描涂抹。人物保持原有比例与外形，不改成漫画或 Q 版；重叠的肢体与相邻物体要用线条分清。不增减人物、肢体或道具，只输出一张线稿，不添加文字、标注、边框或水印。",
	IsSystem:     true, Enabled: true, SortOrder: 32,
}

var frameConvertSkills = map[string]*models.Skill{
	FrameReferenceClay:    &continuityFrameClaySkill,
	FrameReferenceLineart: &continuityFrameLineartSkill,
}

var frameConvertLabels = map[string]string{FrameReferenceClay: "白模", FrameReferenceLineart: "线稿", FrameReferenceHD: "高清"}

// continuityReferenceFormLines describe a derived tail frame to H3. They deliberately avoid the
// opening-frame markers read by openingPictureTag, so a clay or line-art image never becomes
// the 0.00 s first frame. Wording is user-approved.
var continuityReferenceFormLines = map[string]string{
	FrameReferenceClay:    "上一镜尾帧白模（只参考人物姿势、站位、接触、遮挡与场景几何；不提供人物和场景的颜色、材质与光照，不作为开始画面；本镜0.00秒起即为原定画风的完整彩色画面，人物外观按各自人物参考图，场景外观按场景参考图，全程不出现白模或白模转彩色的过程）",
	FrameReferenceLineart: "上一镜尾帧线稿（只参考人物姿势、站位、接触、遮挡与场景几何；不提供颜色、材质与光照，不作为开始画面；本镜0.00秒起即为原定画风的完整彩色画面，全程不出现线稿、素描或逐渐上色的过程）",
}

var errFrameConvertActive = errors.New("此尾帧正在转换，请等待当前任务完成")

// qwenAspectOptions are the canvases offered by the Qwen Edit ResolutionSelector node.
var qwenAspectOptions = []struct {
	label string
	ratio float64
}{
	{"1:1 (Square)", 1}, {"2:3 (Portrait Photo)", 2.0 / 3}, {"3:2 (Photo)", 1.5}, {"3:4 (Portrait Standard)", 0.75},
	{"4:3 (Standard)", 4.0 / 3}, {"9:16 (Portrait Widescreen)", 9.0 / 16}, {"16:9 (Widescreen)", 16.0 / 9}, {"21:9 (Ultrawide)", 21.0 / 9},
}

func ratioDrift(got, want float64) float64 {
	return math.Abs(got-want) / want
}

// closestQwenAspect returns the canvas nearest to ratio, or an error when even the nearest one
// would distort the frame beyond frameConvertMaxRatioDrift.
func closestQwenAspect(ratio float64) (string, error) {
	best, bestDrift := "", math.Inf(1)
	for _, opt := range qwenAspectOptions {
		if d := ratioDrift(opt.ratio, ratio); d < bestDrift {
			best, bestDrift = opt.label, d
		}
	}
	if bestDrift > frameConvertMaxRatioDrift {
		return "", fmt.Errorf("尾帧宽高比 %.3f 与可用画幅相差 %.1f%%，超过 4%%，无法保持原构图", ratio, bestDrift*100)
	}
	return best, nil
}

func imageRatio(data []byte) (float64, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, fmt.Errorf("无法读取图片尺寸: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, fmt.Errorf("图片尺寸无效")
	}
	return float64(cfg.Width) / float64(cfg.Height), nil
}

func (s *ProjectService) readProjectInput(projectID uint, name string) ([]byte, error) {
	if s.upload == nil || s.upload.remote == nil {
		return nil, fmt.Errorf("文件服务未配置")
	}
	f, err := s.upload.remote.Open(s.upload.ResolveReadPath(fmt.Sprint(projectID), name))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 64<<20))
}

func frameConvertPrompt(skills *SkillService, kind string) string {
	skill := frameConvertSkills[kind]
	code := skill.Code
	if skills != nil {
		if latest, err := skills.latestEnabledSkillByCode(code); err == nil {
			skill = latest
		}
	}
	return skill.SystemPrompt
}

// StartFrameConversion submits a Qwen Edit task turning one tail frame of sceneID into a clay or
// line-art reference, or a VOSR2 task upscaling it (kind hd). The source frame row and its image
// are never modified; the result becomes a separate derived FrameCandidate once the task finishes.
func (s *ProjectService) StartFrameConversion(projectID, sceneID, frameID uint, kind string) (*models.FrameCandidate, error) {
	if _, ok := frameConvertSkills[kind]; !ok && kind != FrameReferenceHD {
		return nil, fmt.Errorf("参考形式只能是白模、线稿或高清")
	}
	if s.tasks == nil {
		return nil, fmt.Errorf("尾帧转换依赖 ComfyUI 任务服务")
	}
	var frame models.FrameCandidate
	if err := s.db.Where("id = ? AND project_id = ? AND scene_id = ? AND type IN ?", frameID, projectID, sceneID,
		[]models.FrameCandidateType{models.FrameCandidateCandidate, models.FrameCandidateSelected}).First(&frame).Error; err != nil {
		return nil, fmt.Errorf("候选帧不存在")
	}
	if frame.ConvertTaskID != "" {
		return nil, errFrameConvertActive
	}
	var scene models.Scene
	if err := s.db.Where("id = ? AND project_id = ?", sceneID, projectID).First(&scene).Error; err != nil {
		return nil, fmt.Errorf("场景不存在")
	}
	if frame.VideoTaskID != scene.VideoTaskID {
		return nil, fmt.Errorf("候选帧来自旧视频，请重新提取")
	}
	data, err := s.readProjectInput(projectID, frame.ImageFile)
	if err != nil {
		return nil, fmt.Errorf("读取尾帧失败: %w", err)
	}
	var req CreateTaskReq
	if kind == FrameReferenceHD {
		if req, err = s.frameUpscaleTaskRequest(projectID, frame.ImageFile, data); err != nil {
			return nil, err
		}
	} else {
		ratio, err := imageRatio(data)
		if err != nil {
			return nil, err
		}
		aspect, err := closestQwenAspect(ratio)
		if err != nil {
			return nil, err
		}
		tpl, err := enabledTemplateByCode(s.db, TemplateQwen21MultiEdit)
		if err != nil {
			return nil, err
		}
		params := qwenPortraitParams()
		params["aspect_ratio"] = aspect
		req = CreateTaskReq{TemplateID: tpl.ID, Prompt: frameConvertPrompt(s.skills, kind), Params: params,
			Files: map[string][]FileMeta{"ref_images": {{TaskID: fmt.Sprint(projectID), Name: frame.ImageFile}}}}
	}
	task, err := s.tasks.CreateTask(req)
	if err != nil {
		return nil, fmt.Errorf("创建%s任务失败: %w", frameConvertLabels[kind], err)
	}
	sum := sha256.Sum256(data)
	// The task has not executed yet, so losing this conditional claim to a concurrent request
	// costs nothing: the loser cancels its own task and reports the conflict.
	claim := s.db.Model(&models.FrameCandidate{}).Where("id = ? AND COALESCE(convert_task_id, '') = ''", frame.ID).Updates(map[string]any{
		"convert_task_id": task.TaskID, "convert_kind": kind, "convert_error": "", "convert_source_sha256": hex.EncodeToString(sum[:]),
	})
	if claim.Error != nil || claim.RowsAffected == 0 {
		_ = s.tasks.CancelTask(task.TaskID)
		if claim.Error != nil {
			return nil, claim.Error
		}
		return nil, errFrameConvertActive
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[frame %d] execute %s task %s failed: %v", frame.ID, kind, task.TaskID, err)
		}
	}()
	frame.ConvertTaskID, frame.ConvertKind, frame.ConvertError = task.TaskID, kind, ""
	s.pushProject(nil)
	return &frame, nil
}

func (s *ProjectService) syncFrameConversions() bool {
	var frames []models.FrameCandidate
	if !s.db.Migrator().HasTable(&models.FrameCandidate{}) || s.db.Where("convert_task_id != ''").Find(&frames).Error != nil {
		return false
	}
	changed := false
	for i := range frames {
		frame := &frames[i]
		label := frameConvertLabels[frame.ConvertKind]
		updates, ok := s.imageTaskUpdates(frame.ConvertTaskID, fmt.Sprintf("continuity_s%d_%s_%d", frame.SceneID, frame.ConvertKind, time.Now().UnixNano()),
			frame.ProjectID, "尾帧"+label, "result_file", "convert_task_id", "convert_error")
		if !ok {
			continue
		}
		file, _ := updates["result_file"].(string)
		errText, _ := updates["convert_error"].(string)
		if file != "" {
			if err := s.applyFrameConversion(frame, file); err != nil {
				errText = err.Error()
			}
		}
		res := s.db.Model(&models.FrameCandidate{}).Where("id = ? AND convert_task_id = ?", frame.ID, frame.ConvertTaskID).
			Updates(map[string]any{"convert_task_id": "", "convert_error": errText})
		changed = changed || res.RowsAffected > 0
	}
	return changed
}

// applyFrameConversion records a finished conversion as a new derived row. It refuses the result
// when the source image changed since submission or the output drifted from the source ratio;
// either way the source frame stays as it was.
func (s *ProjectService) applyFrameConversion(frame *models.FrameCandidate, resultFile string) error {
	label := frameConvertLabels[frame.ConvertKind]
	var current models.FrameCandidate
	if err := s.db.First(&current, frame.ID).Error; err != nil {
		return fmt.Errorf("原尾帧已删除，%s结果已丢弃", label)
	}
	source, err := s.readProjectInput(current.ProjectID, current.ImageFile)
	if err != nil {
		return fmt.Errorf("读取原尾帧失败，%s结果已丢弃: %w", label, err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != frame.ConvertSourceSHA256 {
		return fmt.Errorf("原尾帧在转换期间已变化，%s结果已丢弃，请重新转换", label)
	}
	result, err := s.readProjectInput(current.ProjectID, resultFile)
	if err != nil {
		return fmt.Errorf("读取%s结果失败: %w", label, err)
	}
	sourceRatio, err := imageRatio(source)
	if err != nil {
		return err
	}
	resultRatio, err := imageRatio(result)
	if err != nil {
		return err
	}
	if d := ratioDrift(resultRatio, sourceRatio); d > frameConvertMaxRatioDrift {
		return fmt.Errorf("%s结果宽高比与原尾帧相差 %.1f%%，超过 4%%，已丢弃", label, d*100)
	}
	if frame.ConvertKind == FrameReferenceHD {
		if resultFile, err = s.finishFrameUpscale(&current, source, result, resultFile); err != nil {
			return err
		}
	}
	_, err = s.recordDerivedFrame(&current, frame.ConvertKind, resultFile)
	return err
}

// recordDerivedFrame stores resultFile as a new derived row of current (kind clay/lineart/hd) and
// invalidates the Scenes that already use that form of the frame.
func (s *ProjectService) recordDerivedFrame(current *models.FrameCandidate, kind, resultFile string) (*models.FrameCandidate, error) {
	var derived models.FrameCandidate
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// FrameIndex is part of the history key; derived rows take a fresh index after every frame.
		var maxIndex int
		if err := tx.Model(&models.FrameCandidate{}).Where("scene_id = ? AND video_task_id = ?", current.SceneID, current.VideoTaskID).
			Select("COALESCE(MAX(frame_index), 0)").Scan(&maxIndex).Error; err != nil {
			return err
		}
		derived = models.FrameCandidate{ProjectID: current.ProjectID, SceneID: current.SceneID, VideoTaskID: current.VideoTaskID,
			Type: models.FrameCandidateDerived, FrameIndex: maxIndex + 1, TimestampMS: current.TimestampMS, ImageFile: resultFile,
			Source: kind, DerivedFromID: &current.ID, DerivedSourceFile: current.ImageFile}
		if err := tx.Create(&derived).Error; err != nil {
			return err
		}
		// Scenes already using this form of the frame now receive a different reference image.
		var dependentIDs []uint
		if err := tx.Model(&models.SceneContinuity{}).Where("selected_frame_id = ? AND reference_form = ?", current.ID, kind).
			Pluck("scene_id", &dependentIDs).Error; err != nil {
			return err
		}
		for _, id := range dependentIDs {
			if err := invalidateSceneVideoTx(tx, current.ProjectID, id, "上一镜尾帧"+frameConvertLabels[kind]+"已更新"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &derived, nil
}

// DerivedReference returns the newest clay/line-art row made from frame's current image, or nil.
// A row converted from an image that was later replaced is ignored.
func (s *ContinuityService) DerivedReference(frame *models.FrameCandidate, kind string) (*models.FrameCandidate, error) {
	if frame == nil {
		return nil, nil
	}
	var derived models.FrameCandidate
	result := s.db.Where("derived_from_id = ? AND source = ? AND derived_source_file = ? AND type = ?", frame.ID, kind, frame.ImageFile, models.FrameCandidateDerived).
		Order("id DESC").Limit(1).Find(&derived)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, result.Error
	}
	return &derived, nil
}

func (s *ContinuityService) derivedReferences(frames []models.FrameCandidate) map[uint]map[string]models.FrameCandidate {
	out := map[uint]map[string]models.FrameCandidate{}
	if len(frames) == 0 {
		return out
	}
	ids := make([]uint, 0, len(frames))
	files := map[uint]string{}
	for _, f := range frames {
		ids = append(ids, f.ID)
		files[f.ID] = f.ImageFile
	}
	var rows []models.FrameCandidate
	if s.db.Where("derived_from_id IN ? AND type = ?", ids, models.FrameCandidateDerived).Order("id ASC").Find(&rows).Error != nil {
		return out
	}
	for _, row := range rows {
		if row.DerivedFromID == nil || files[*row.DerivedFromID] != row.DerivedSourceFile {
			continue
		}
		if out[*row.DerivedFromID] == nil {
			out[*row.DerivedFromID] = map[string]models.FrameCandidate{}
		}
		out[*row.DerivedFromID][row.Source] = row
	}
	return out
}

func normalizeFrameReferenceForm(form, mode string) (string, error) {
	switch strings.TrimSpace(form) {
	case "", FrameReferenceOriginal:
		return FrameReferenceOriginal, nil
	case FrameReferenceClay, FrameReferenceLineart, FrameReferenceHD:
		if mode != models.ContinuityModeContinue {
			return "", fmt.Errorf("白模、线稿和高清参考只用于「从上一镜末尾帧继续」；首尾桥接必须使用原图")
		}
		return strings.TrimSpace(form), nil
	}
	return "", fmt.Errorf("不支持的参考形式")
}

func (s *Service) HandleConvertFrameCandidate(c *gin.Context) {
	pid, sid, ok := continuityIDs(c)
	if !ok {
		return
	}
	fid, ok := pathUint(c, "fid")
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	frame, err := s.Projects.StartFrameConversion(pid, sid, fid, req.Kind)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errFrameConvertActive) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"frame": s.frameResponses([]models.FrameCandidate{*frame})[0], "message": "尾帧" + frameConvertLabels[req.Kind] + "任务已提交，原尾帧保持不变"})
}
