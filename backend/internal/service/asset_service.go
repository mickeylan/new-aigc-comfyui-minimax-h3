package service

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"time"

	"comfyui-console/internal/models"
	"gorm.io/gorm"
)

// ---------- 视觉资产（Asset Bible：道具 / 场景） ----------
//
// 把关键道具与主要场景从创作方案提升为独立资产：项目内可复用的设定 + 参考图。
// 分镜画面文生图时按 scene.location / scene.props 注入参考图（图生图）与文字设定，
// 纠偏 LLM 描述漂移，保证同一道具/场景跨分镜、跨集外观一致。

const (
	AssetKindProp     = "prop"     // 道具
	AssetKindLocation = "location" // 场景（地点）
)

// normalizeAssetKind 校验资产类别（URL :kind 段）
func normalizeAssetKind(kind string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(kind)) {
	case "prop", "props":
		return AssetKindProp, nil
	case "location", "locations", "scene":
		return AssetKindLocation, nil
	}
	return "", fmt.Errorf("无效的资产类别：%s（仅支持 prop/location）", kind)
}

// AssetKindLabel 类别中文名
func AssetKindLabel(kind string) string {
	if kind == AssetKindLocation {
		return "场景"
	}
	return "道具"
}

// assetFilePrefix 资产参考图文件名前缀
func assetFilePrefix(kind string) string {
	if kind == AssetKindLocation {
		return "loc"
	}
	return "prop"
}

// ListAssets 项目内资产列表（kind 为空返回全部类别）
func (s *ProjectService) ListAssets(projectID uint, kind string) ([]models.Asset, error) {
	var list []models.Asset
	query := s.db.Where("project_id = ?", projectID)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	query.Order("id").Find(&list)
	return list, nil
}

// AssetSceneCounts 统计每个资产在分镜中的出场次数（location 精确匹配、props 逗号分隔匹配）
func (s *ProjectService) AssetSceneCounts(projectID uint) (map[uint]int, error) {
	var assets []models.Asset
	if err := s.db.Where("project_id = ?", projectID).Find(&assets).Error; err != nil {
		return nil, err
	}
	var scenes []models.Scene
	s.db.Where("project_id = ?", projectID).Find(&scenes)
	count := map[uint]int{}
	for _, sc := range scenes {
		loc := strings.TrimSpace(sc.LocationName)
		propSet := map[string]bool{}
		for _, n := range parseSceneCharacters(sc.Props) {
			propSet[n] = true
		}
		for i := range assets {
			a := &assets[i]
			if (a.Kind == AssetKindLocation && loc != "" && a.Name == loc) ||
				(a.Kind == AssetKindProp && propSet[a.Name]) {
				count[a.ID]++
			}
		}
	}
	return count, nil
}

// RedesignAssetDescription expands a short user brief into an authoritative asset bible entry.
func (s *ProjectService) RedesignAssetDescription(p *models.Project, kind, name, brief string) (string, error) {
	kind, err := normalizeAssetKind(kind)
	if err != nil {
		return "", err
	}
	name, brief = strings.TrimSpace(name), strings.TrimSpace(brief)
	if name == "" || brief == "" {
		return "", fmt.Errorf("名称和简单说明不能为空")
	}
	if s.textProvider == nil {
		return "", fmt.Errorf("文本生成服务未配置")
	}
	system := `你是影视美术设定师。把用户的简单说明扩写为可长期复用的资产权威设定。
严格遵守项目题材、时代、地域文化和画风，不添加人物或剧情。
如果是场景：写清空间类型、建筑结构、固定布局、入口出口、前中后景、主要陈设、材质、主辅色、光源方向、时段与氛围；必须是无人物环境设定。
如果是道具：写清类别、形状比例、材质、颜色、纹样、磨损、可识别特征和尺寸参照；必须是无生命单一物件，不得因名称含人物或生物词而拟人化。
只输出一段具体中文描述，不要标题、解释、Markdown、JSON或空泛质量标签。`
	user := fmt.Sprintf("项目：%s\n题材：%s\n画风：%s\n故事：%s\n资产类型：%s\n名称：%s\n用户简单说明：%s", p.Title, p.Genre, p.Style, p.Synopsis, AssetKindLabel(kind), name, brief)
	output, err := s.textProvider.Chat(system, user)
	if err != nil {
		return "", fmt.Errorf("AI 重新设计%s失败: %w", AssetKindLabel(kind), err)
	}
	output = strings.TrimSpace(strings.Trim(strings.TrimSpace(output), "`"))
	if len([]rune(output)) < 20 {
		return "", fmt.Errorf("AI 返回的%s描述过短，请重试", AssetKindLabel(kind))
	}
	return output, nil
}

func normalizeAssetVisual(kind, visualType, megaType string) (string, string) {
	if kind != AssetKindLocation || strings.TrimSpace(visualType) != "megastructure" {
		return "normal", ""
	}
	return "megastructure", normalizeMegaType(megaType)
}

// CreateAsset 手动新建资产
func (s *ProjectService) CreateAsset(a models.Asset) (*models.Asset, error) {
	kind, err := normalizeAssetKind(a.Kind)
	if err != nil {
		return nil, err
	}
	a.Kind = kind
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" {
		return nil, fmt.Errorf("%s名不能为空", AssetKindLabel(kind))
	}
	a.Source = "manual"
	a.VisualType, a.MegaType = normalizeAssetVisual(a.Kind, a.VisualType, a.MegaType)
	a.ImageEngine, err = normalizeAssetImageEngine(a.ImageEngine)
	if err != nil {
		return nil, err
	}
	a.VisibleText = strings.TrimSpace(a.VisibleText)
	a.Image = "" // 新建无参考图
	if err := s.db.Create(&a).Error; err != nil {
		return nil, fmt.Errorf("%s名已存在或创建失败: %w", AssetKindLabel(kind), err)
	}
	s.pushProject(nil)
	return &a, nil
}

// UpdateAsset 编辑资产（改名需保证项目内同类别唯一）；不影响已生成的参考图
func (s *ProjectService) UpdateAsset(a *models.Asset, req models.Asset) error {
	updates := map[string]any{}
	newName := strings.TrimSpace(req.Name)
	if newName != "" && newName != a.Name {
		var cnt int64
		s.db.Model(&models.Asset{}).Where("project_id = ? AND kind = ? AND name = ? AND id != ?", a.ProjectID, a.Kind, newName, a.ID).Count(&cnt)
		if cnt > 0 {
			return fmt.Errorf("该%s名已存在", AssetKindLabel(a.Kind))
		}
		updates["name"] = newName
	}
	updates["description"] = strings.TrimSpace(req.Description)
	engine, err := normalizeAssetImageEngine(req.ImageEngine)
	if err != nil {
		return err
	}
	updates["image_engine"], updates["visible_text"] = engine, strings.TrimSpace(req.VisibleText)
	visualType, megaType := normalizeAssetVisual(a.Kind, req.VisualType, req.MegaType)
	if visualType != a.VisualType || megaType != a.MegaType {
		updates["visual_type"], updates["mega_type"] = visualType, megaType
		updates["image"], updates["image_task_id"], updates["image_error"] = "", "", ""
	}
	if err := s.db.Model(a).Updates(updates).Error; err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// DeleteAsset 删除资产（同时清理远程参考图文件）
func (s *ProjectService) DeleteAsset(projectID, id uint) error {
	var a models.Asset
	if err := s.db.Where("id = ? AND project_id = ?", id, projectID).First(&a).Error; err != nil {
		return err
	}
	if a.Image != "" && s.cfg != nil && s.remote != nil && s.upload != nil {
		path := filepath.Join(s.upload.InputDir(), fmt.Sprintf("%d", projectID), a.Image)
		if _, err := s.remote.Run("rm -f -- " + shellQuote(path)); err != nil {
			log.Printf("[asset %d] cleanup image %s failed: %v", a.ID, path, err)
		}
	}
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("project_id = ? AND entity_id = ? AND entity_type IN ?", projectID, id, []string{VariantAssetImage, VariantAssetSheet}).Delete(&models.AssetVariant{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ? AND project_id = ?", id, projectID).Delete(&models.Asset{}).Error
	}); err != nil {
		return err
	}
	s.pushProject(nil)
	return nil
}

// upsertAssetsFromPlan 从创作方案抽取道具/场景为独立资产。
// 已存在同名：仅补全空描述，保留用户编辑与参考图；新资产以 source=auto 入库。
func (s *ProjectService) upsertAssetsFromPlan(p *models.Project, plan *dramaPlan) {
	if plan == nil {
		return
	}
	type planAsset struct {
		kind, name, desc string
	}
	var incoming []planAsset
	for _, pa := range plan.Props {
		if name := strings.TrimSpace(pa.Name); name != "" {
			incoming = append(incoming, planAsset{AssetKindProp, name, strings.TrimSpace(pa.Description)})
		}
	}
	for _, la := range plan.Locations {
		if name := strings.TrimSpace(la.Name); name != "" {
			incoming = append(incoming, planAsset{AssetKindLocation, name, strings.TrimSpace(la.Description)})
		}
	}
	for _, in := range incoming {
		var existing models.Asset
		err := s.db.Where("project_id = ? AND kind = ? AND name = ?", p.ID, in.kind, in.name).First(&existing).Error
		if err == nil {
			if strings.TrimSpace(existing.Description) == "" && in.desc != "" {
				s.db.Model(&existing).Update("description", in.desc)
			}
			continue
		}
		s.db.Create(&models.Asset{
			ProjectID: p.ID, Kind: in.kind, Name: in.name, Description: in.desc, Source: "auto",
		})
	}
	if len(incoming) > 0 {
		s.pushProject(nil)
	}
}

// upsertAssetsFromScenes 分镜引用的道具/地点若尚未建卡则自动补建（source=auto），
// 保证后续参考图生成与一致性注入可按名称匹配。
func (s *ProjectService) upsertAssetsFromScenes(projectID uint, scenes []scriptScene) {
	type key struct{ kind, name string }
	seen := map[key]bool{}
	var add []models.Asset
	for _, sc := range scenes {
		if loc := strings.TrimSpace(sc.Location); loc != "" && !seen[key{AssetKindLocation, loc}] {
			seen[key{AssetKindLocation, loc}] = true
			add = append(add, models.Asset{ProjectID: projectID, Kind: AssetKindLocation, Name: loc, Source: "auto"})
		}
		for _, pn := range sc.Props {
			name := strings.TrimSpace(pn)
			if name == "" || seen[key{AssetKindProp, name}] {
				continue
			}
			seen[key{AssetKindProp, name}] = true
			add = append(add, models.Asset{ProjectID: projectID, Kind: AssetKindProp, Name: name, Source: "auto"})
		}
	}
	for i := range add {
		var cnt int64
		s.db.Model(&models.Asset{}).Where("project_id = ? AND kind = ? AND name = ?", projectID, add[i].Kind, add[i].Name).Count(&cnt)
		if cnt == 0 {
			s.db.Create(&add[i])
		}
	}
}

var errAssetImageActive = errors.New("资产参考图正在生成")

// GenerateAllAssetImages 一键生成全部缺少参考图的资产（kind 为空表示全部类别）。
// 已有活动任务的资产会跳过，不会重复创建 ComfyUI 任务。
func (s *ProjectService) GenerateAllAssetImages(p *models.Project, kind string) (int, error) {
	query := s.db.Where("project_id = ? AND image = ''", p.ID)
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	var assets []models.Asset
	if err := query.Find(&assets).Error; err != nil {
		return 0, err
	}
	submitted := 0
	for i := range assets {
		if err := s.StartAssetImage(&assets[i]); err != nil {
			if errors.Is(err, errAssetImageActive) {
				continue
			}
			return submitted, err
		}
		submitted++
	}
	return submitted, nil
}

// StartAssetImage 使用本地 ComfyUI Krea2 模板异步生成资产参考图。
func (s *ProjectService) StartAssetImage(a *models.Asset) error {
	if s.tasks == nil {
		return fmt.Errorf("Krea2 资产参考图生成依赖 ComfyUI 任务服务")
	}

	// 串行化“检查活动任务 → 创建任务 → 绑定资产”，避免并发请求重复排队。
	s.assetMu.Lock()
	defer s.assetMu.Unlock()
	var current models.Asset
	if err := s.db.First(&current, a.ID).Error; err != nil {
		return err
	}
	if current.ImageTaskID != "" {
		var active int64
		if err := s.db.Model(&models.Task{}).Where("task_id = ? AND status IN ?", current.ImageTaskID, []string{"pending", "queued", "running"}).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return fmt.Errorf("%w：%s「%s」", errAssetImageActive, AssetKindLabel(current.Kind), current.Name)
		}
	}
	var p models.Project
	if err := s.db.First(&p, current.ProjectID).Error; err != nil {
		return err
	}
	engine, err := normalizeAssetImageEngine(current.ImageEngine)
	if err != nil {
		return err
	}
	templateCode := "krea2_asset_reference"
	if engine == ImageEngineQwen21 {
		templateCode = TemplateQwen21T2I
	}
	tpl, err := enabledTemplateByCode(s.db, templateCode)
	if err != nil {
		return err
	}
	width, height := assetImageSize(&p, current.Kind)
	params := map[string]any{"width": width, "height": height}
	if engine == ImageEngineQwen21 { params = map[string]any{"aspect_ratio":qwenAspectRatio(p.AspectRatio),"megapixels":2.5,"multiple":8,"reference_resolution":1024,"steps":25,"cfg":1,"negative_prompt":""} }
	task, err := s.tasks.CreateTask(CreateTaskReq{TemplateID: tpl.ID, Prompt: buildAssetPrompt(&p, &current), Params: params})
	if err != nil {
		return fmt.Errorf("创建资产参考图任务失败: %w", err)
	}
	if err := s.db.Model(&models.Asset{}).Where("id = ?", current.ID).Updates(map[string]any{
		"image_task_id": task.TaskID, "image_error": "",
	}).Error; err != nil {
		_ = s.tasks.CancelTask(task.TaskID)
		return err
	}
	go func() {
		if err := s.tasks.Execute(task.TaskID); err != nil && !errors.Is(err, errNoFreeGPU) {
			log.Printf("[asset %d] execute Krea2 task %s failed: %v", current.ID, task.TaskID, err)
		}
	}()
	s.pushProject(nil)
	return nil
}

func assetImageSize(p *models.Project, kind string) (int, int) {
	if kind == AssetKindProp {
		return 1024, 1024
	}
	switch strings.TrimSpace(p.AspectRatio) {
	case "9:16":
		return 768, 1344
	case "1:1":
		return 1024, 1024
	default:
		return 1344, 768
	}
}

// buildAssetPrompt 资产参考图提示词：画风强约束 + 描述 + 类别定式（道具特写 / 场景空镜）
func buildAssetPrompt(p *models.Project, a *models.Asset) string {
	parts := make([]string, 0, 6)
	if a.Kind == AssetKindProp {
		// Put the asset class before its name. Fantasy names such as “元婴玉佩” contain
		// person-like tokens; without this guard portrait-biased checkpoints may draw a person.
		parts = append(parts,
			"【主体类型硬约束】这是一件无生命道具的产品参考图，画面主体只能是一个物件，人物数量必须为零",
			"道具名称「"+strings.TrimSpace(a.Name)+"」只是物品专名，名称中的人物、身份或生物词汇不得被画成人形；它不是人物、不是美女、不是婴儿、不是人体、不是雕像")
		if d := strings.TrimSpace(a.Description); d != "" {
			parts = append(parts, "物件外观必须精确遵守："+d)
		}
		if style := strings.TrimSpace(p.Style); style != "" {
			parts = append(parts, "只把项目画风「"+style+"」用于物件材质、色彩和照明，不得因此增加人物")
		}
		parts = append(parts, "道具特写参考图，单一完整物体，居中构图，中性纯色背景，产品摄影构图，材质与纹理清晰，画面中人物数量必须为零，禁止脸、人体、手、婴儿、美女、人像、拟人化、文字、水印和拼图")
		return strings.Join(parts, "，")
	}
	parts = append(parts, "【主体类型硬约束】这是无人物的环境场景空镜，人物数量必须为零")
	if desc := styleDescriptor(p.Style); desc != "" {
		parts = append(parts, desc)
	}
	parts = append(parts, "场景「"+strings.TrimSpace(a.Name)+"」")
	if d := strings.TrimSpace(a.Description); d != "" {
		parts = append(parts, "环境外观必须精确遵守："+d)
	}
	if text := strings.TrimSpace(a.VisibleText); text != "" {
		parts = append(parts, "画面中必须逐字清晰呈现可读文字：\""+strings.ReplaceAll(text, "\"", "\\\"")+"\"；不得改写、翻译、遗漏或新增其他文字")
	}
	if visualType, megaType := normalizeAssetVisual(a.Kind, a.VisualType, a.MegaType); visualType == "megastructure" {
		label := map[string]string{"architecture": "建筑巨构", "creature": "巨兽/生物巨构", "geological": "自然/地质巨构", "mechanical": "机械/载具巨构", "surreal": "超现实混合巨构"}[megaType]
		parts = append(parts, "【巨构场景专项】"+label+"，必须使用明确尺度参照、前中后景大气分层、可读的巨型结构、压倒性体量与重量感、广角远景构图；保持环境资产本身，不增加剧情人物")
	}
	ending := "场景空镜参考图，全景构图，无人物，环境陈设与光影氛围完整清晰，高质量，禁止人物、水印和拼图"
	if strings.TrimSpace(a.VisibleText) == "" {
		ending += "，禁止文字"
	}
	parts = append(parts, ending)
	return strings.Join(parts, "，")
}

// syncAssetImages 把已结束的 Krea2 任务结果同步到项目 input 目录。
func (s *ProjectService) syncAssetImages() {
	var assets []models.Asset
	if err := s.db.Where("image_task_id != ''").Find(&assets).Error; err != nil {
		return
	}
	changed := false
	for i := range assets {
		a := &assets[i]
		var task models.Task
		if err := s.db.Where("task_id = ?", a.ImageTaskID).First(&task).Error; err != nil {
			res := s.db.Model(a).Where("image_task_id = ?", a.ImageTaskID).Updates(map[string]any{
				"image_task_id": "", "image_error": "Krea2 资产参考图任务不存在，请重新生成",
			})
			changed = changed || res.RowsAffected > 0
			continue
		}
		switch task.Status {
		case "failed", "cancelled":
			msg := strings.TrimSpace(task.Error)
			if msg == "" {
				msg = "Krea2 任务已" + map[string]string{"failed": "失败", "cancelled": "取消"}[task.Status]
			}
			res := s.db.Model(a).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{"image_task_id": "", "image_error": msg})
			changed = changed || res.RowsAffected > 0
		case "success":
			file, _ := resultImageOf(&task)
			if file == "" || task.Port == nil || s.upload == nil || s.tasks == nil {
				res := s.db.Model(a).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{
					"image_task_id": "", "image_error": "Krea2 任务成功但未返回可用图片",
				})
				changed = changed || res.RowsAffected > 0
				continue
			}
			subfolder, filename := filepath.ToSlash(filepath.Dir(file)), filepath.Base(file)
			if subfolder == "." {
				subfolder = ""
			}
			client := NewComfyClient(s.tasks.comfyHostForTask(&task), *task.Port)
			data, err := client.DownloadOutput(filename, subfolder, "output")
			if err != nil {
				res := s.db.Model(a).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{
					"image_task_id": "", "image_error": "读取 Krea2 资产参考图失败: " + err.Error(),
				})
				changed = changed || res.RowsAffected > 0
				continue
			}
			ext := filepath.Ext(file)
			if ext == "" {
				ext = detectImageExt(data)
			}
			name := fmt.Sprintf("%s_%d_%d%s", assetFilePrefix(a.Kind), a.ID, time.Now().UnixNano(), ext)
			path, _, err := s.upload.SaveFile(fmt.Sprint(a.ProjectID), "image", name, data)
			if err != nil {
				res := s.db.Model(a).Where("image_task_id = ?", task.TaskID).Updates(map[string]any{
					"image_task_id": "", "image_error": "保存 Krea2 资产参考图失败: " + err.Error(),
				})
				changed = changed || res.RowsAffected > 0
				continue
			}
			_, applied, updateErr := updateSelectedAsset(s.db, a.ProjectID, VariantAssetImage, a.ID, filepath.Base(path), task.Prompt, "krea2_task:"+task.TaskID, map[string]any{
				"image_task_id": "", "image_error": "",
			}, "image_task_id", task.TaskID)
			if updateErr == nil && applied {
				changed = true
			}
		}
	}
	if changed {
		s.pushProject(nil)
	}
}

// sceneMatchedAssets 返回该分镜引用的资产（场景在前、道具在后，按引用顺序）。
// 优先按 scene.location/props 精确匹配；未标注时按资产名出现在分镜文案中模糊匹配兜底
// （兼容旧项目与 LLM 漏标），道具模糊匹配最多补 3 个。
func (s *ProjectService) sceneMatchedAssets(sc *models.Scene) []models.Asset {
	var assets []models.Asset
	s.db.Where("project_id = ?", sc.ProjectID).Find(&assets)
	if len(assets) == 0 {
		return nil
	}
	find := func(kind, name string) *models.Asset {
		for i := range assets {
			if assets[i].Kind == kind && assets[i].Name == name {
				return &assets[i]
			}
		}
		return nil
	}
	text := sc.ImagePrompt + "\n" + sc.Content + "\n" + sc.Title
	nameIn := func(name string) bool {
		return name != "" && strings.Contains(text, name)
	}

	var out []models.Asset
	locName := strings.TrimSpace(sc.LocationName)
	if locName != "" {
		if a := find(AssetKindLocation, locName); a != nil {
			out = append(out, *a)
		}
	} else {
		// 模糊匹配：提示词中出现的第一个场景资产
		for i := range assets {
			if assets[i].Kind == AssetKindLocation && assets[i].Image != "" && nameIn(assets[i].Name) {
				out = append(out, assets[i])
				break
			}
		}
	}
	matched := map[string]bool{}
	for _, n := range parseSceneCharacters(sc.Props) {
		if a := find(AssetKindProp, n); a != nil {
			out = append(out, *a)
			matched[a.Name] = true
		}
	}
	fuzzy := 0
	for i := range assets {
		if fuzzy >= 3 {
			break
		}
		if assets[i].Kind != AssetKindProp || matched[assets[i].Name] || assets[i].Image == "" || !nameIn(assets[i].Name) {
			continue
		}
		out = append(out, assets[i])
		matched[assets[i].Name] = true
		fuzzy++
	}
	return out
}

// sceneAssetRefs 匹配资产的参考图转为图生图参考图列表（与角色标准像同序拼接）。
// startIdx 为已有角色参考图数量，用于「图N」编号连续；返回 (参考图列表, 描述行)，与参考图一一对应。
func (s *ProjectService) sceneAssetRefs(sc *models.Scene, startIdx int) ([]ImageRef, []string) {
	assets := s.sceneMatchedAssets(sc)
	if len(assets) == 0 {
		return nil, nil
	}
	refs := make([]ImageRef, 0, len(assets))
	lines := make([]string, 0, len(assets))
	for _, a := range assets {
		if startIdx+len(refs) >= maxSceneReferenceImages {
			break
		}
		name, kind := a.Image, "参考图"
		if a.Kind == AssetKindProp && a.Sheet != "" {
			name, kind = a.Sheet, "四视图"
		}
		if name == "" {
			continue
		}
		abs := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprintf("%d", sc.ProjectID), name)
		f, err := s.remote.Open(abs)
		if err != nil {
			log.Printf("[project %d] open asset image %s: %v", sc.ProjectID, abs, err)
			continue
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			continue
		}
		ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
		if ext == "jpg" {
			ext = "jpeg"
		}
		if ext != "jpeg" && ext != "png" && ext != "webp" {
			ext = "jpeg"
		}
		refs = append(refs, ImageRef{URL: fmt.Sprintf("data:image/%s;base64,%s", ext, base64.StdEncoding.EncodeToString(data))})
		lines = append(lines, fmt.Sprintf("- 图%d：%s「%s」%s（%s）", startIdx+len(refs), AssetKindLabel(a.Kind), a.Name, kind, strings.TrimSpace(a.Description)))
	}
	return refs, lines
}

// assetContextForScene 拼装分镜引用资产的权威设定文本，注入文生图提示词纠偏描述漂移
func (s *ProjectService) assetContextForScene(sc *models.Scene) string {
	assets := s.sceneMatchedAssets(sc)
	if len(assets) == 0 {
		return ""
	}
	lines := make([]string, 0, len(assets))
	for _, a := range assets {
		desc := strings.TrimSpace(a.Description)
		if desc == "" {
			desc = "外观保持与参考图完全一致"
		}
		lines = append(lines, fmt.Sprintf("- %s「%s」：%s", AssetKindLabel(a.Kind), a.Name, desc))
	}
	return "【道具与场景设定（必须严格遵守，保证道具与环境一致）】\n" + strings.Join(lines, "\n")
}

// pendingSceneAssetRefs 当前集分镜引用、但参考图未就绪的资产数（流水线门控用）
func (s *ProjectService) pendingSceneAssetRefs(projectID uint, episodeN int, generation uint) int64 {
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", projectID, episodeN, generation).Find(&scenes).Error; err != nil || len(scenes) == 0 {
		return 0
	}
	var assets []models.Asset
	s.db.Where("project_id = ?", projectID).Find(&assets)
	if len(assets) == 0 {
		return 0
	}
	byKindName := map[string]bool{}
	for _, a := range assets {
		if a.Image == "" {
			byKindName[a.Kind+"|"+a.Name] = true
		}
	}
	var pending int64
	for _, sc := range scenes {
		if n := strings.TrimSpace(sc.LocationName); n != "" && byKindName[AssetKindLocation+"|"+n] {
			delete(byKindName, AssetKindLocation+"|"+n) // 每个缺图资产只计一次
			pending++
		}
		for _, pn := range parseSceneCharacters(sc.Props) {
			if byKindName[AssetKindProp+"|"+pn] {
				delete(byKindName, AssetKindProp+"|"+pn)
				pending++
			}
		}
	}
	return pending
}
