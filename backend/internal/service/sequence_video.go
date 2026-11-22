package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"comfyui-console/internal/models"
)

// 3D 编辑器导出的参考图序列（白模/法线/深度各一组，同一次导出的帧共用文件名里的时间戳）
// 合成成 mp4 之后能当 MiniMax H3 的视频参考：MiniMaxH3ReferenceToVideo 的 optional
// ref_videos 接 <Video k> 标签，比一张张图片更能表达「沿这条路径推拉」的机位。
const (
	sequenceVideoMinFrames  = 2
	sequenceVideoMaxFrames  = 240
	sequenceVideoDefaultFPS = 8
	sequenceVideoMaxFPS     = 30
	// H3 的视频参考帧按 24 fps 吃（ref_videos 的 tooltip 写死），合成时统一成这个帧率。
	sequenceVideoOutputFPS = 24
	sequenceVideoTimeout   = 5 * time.Minute
)

// clay-seq-20261122-174038-007.png → pass=clay stamp=20261122-174038 index=007
var sequenceFrameNamePattern = regexp.MustCompile(`^([a-z]+)-seq-(\d{8}-\d{6})-(\d{3})\.png$`)

var sequenceStampPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

var sequencePasses = map[string]bool{"clay": true, "normal": true, "depth": true, "ref": true}

// SequenceVideoRequest 把素材库里的一组序列帧合成 mp4。
type SequenceVideoRequest struct {
	Pass  string `json:"pass"`  // clay / normal / depth / ref
	Stamp string `json:"stamp"` // 20261122-174038，和帧文件名里的一致
	FPS   int    `json:"fps"`   // 缺省 8，1–30
}

// normalizeSequencePass 只认 3D 编辑器导出的那几种 pass，避免拿任意文件名拼路径。
func normalizeSequencePass(pass string) (string, error) {
	p := strings.ToLower(strings.TrimSpace(pass))
	if p == "" {
		p = "clay"
	}
	if !sequencePasses[p] {
		return "", fmt.Errorf("未知的参考图类型: %s", pass)
	}
	return p, nil
}

func normalizeSequenceFPS(fps int) int {
	if fps <= 0 {
		return sequenceVideoDefaultFPS
	}
	if fps > sequenceVideoMaxFPS {
		return sequenceVideoMaxFPS
	}
	return fps
}

// sequenceVideoName 合成结果的素材名：clay-seq-20261122-174038.mp4
func sequenceVideoName(pass, stamp string) string {
	return fmt.Sprintf("%s-seq-%s.mp4", pass, stamp)
}

// orderSequenceFrames 按帧号排好并校验连续：缺帧会让 ffmpeg 静默截断，必须在合成前拦住。
func orderSequenceFrames(names []string, pass, stamp string) ([]string, error) {
	type frame struct {
		index int
		name  string
	}
	frames := make([]frame, 0, len(names))
	for _, name := range names {
		match := sequenceFrameNamePattern.FindStringSubmatch(name)
		if match == nil || match[1] != pass || match[2] != stamp {
			continue
		}
		index, err := strconv.Atoi(match[3])
		if err != nil {
			continue
		}
		frames = append(frames, frame{index: index, name: name})
	}
	if len(frames) < sequenceVideoMinFrames {
		return nil, fmt.Errorf("素材库里 %s-seq-%s 只有 %d 帧，至少需要 %d 帧（先导出并「序列存库」）", pass, stamp, len(frames), sequenceVideoMinFrames)
	}
	if len(frames) > sequenceVideoMaxFrames {
		return nil, fmt.Errorf("序列 %d 帧超过 %d 帧上限", len(frames), sequenceVideoMaxFrames)
	}
	sort.Slice(frames, func(i, j int) bool { return frames[i].index < frames[j].index })
	ordered := make([]string, 0, len(frames))
	for position, frame := range frames {
		if frame.index != position {
			return nil, fmt.Errorf("序列缺第 %03d 帧（帧号必须从 000 连续）", position)
		}
		ordered = append(ordered, frame.name)
	}
	return ordered, nil
}

// sequenceVideoArgs 图片序列 → H.264 mp4：H3 的视频参考按 24 fps 的帧吃，
// 但序列帧本身是「按 fps 采样」出来的，所以输入按 fps、输出统一 24 fps
// （ffmpeg 复制帧补满，时长不变）——这样 8 fps 拍的 13 帧到了 H3 手里是 24 fps 的 39 帧。
func sequenceVideoArgs(pattern string, fps int, outputPath string) []string {
	return []string{
		"-y", "-hide_banner", "-loglevel", "error",
		"-framerate", strconv.Itoa(fps),
		"-start_number", "0",
		"-i", pattern,
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-r", strconv.Itoa(sequenceVideoOutputFPS),
		"-movflags", "+faststart",
		outputPath,
	}
}

// ComposeSequenceVideo 把素材库里同一次导出的序列帧合成 mp4 并入库（type=video）。
func (m *MaterialService) ComposeSequenceVideo(projectID *uint, req SequenceVideoRequest) (*models.Material, error) {
	pass, err := normalizeSequencePass(req.Pass)
	if err != nil {
		return nil, err
	}
	stamp := strings.TrimSpace(req.Stamp)
	if !sequenceStampPattern.MatchString(stamp) {
		return nil, fmt.Errorf("时间戳格式应为 20261122-174038")
	}
	fps := normalizeSequenceFPS(req.FPS)

	var rows []models.Material
	prefix := fmt.Sprintf("%s-seq-%s-", pass, stamp)
	if err := m.db.Where("type = ? AND name LIKE ?", "image", prefix+"%").Order("name ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, row.Name)
	}
	ordered, err := orderSequenceFrames(names, pass, stamp)
	if err != nil {
		return nil, err
	}
	// ffmpeg 只认本机路径；帧是按 task 目录落盘的，取第一帧所在目录当序列目录。
	if m.remote != nil && m.remote.Enabled() {
		return nil, fmt.Errorf("远程 ComfyUI 模式下暂不支持本机合成序列视频，请在本机模式使用")
	}
	root := m.upload.InputDir()
	dir := filepath.Dir(filepath.Join(root, filepath.FromSlash(rows[0].Path)))
	for _, name := range ordered {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr != nil {
			return nil, fmt.Errorf("序列帧不在本机磁盘上: %s", name)
		}
	}

	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	outName := sequenceVideoName(pass, stamp)
	tmpDir, err := os.MkdirTemp("", "seqvideo-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	outPath := filepath.Join(tmpDir, outName)
	pattern := filepath.Join(dir, fmt.Sprintf("%s-seq-%s-%%03d.png", pass, stamp))
	if output, err := runLocalProgram(ffmpeg, sequenceVideoArgs(pattern, fps, outPath), sequenceVideoTimeout); err != nil {
		return nil, fmt.Errorf("ffmpeg 合成失败: %v %s", err, strings.TrimSpace(output))
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("ffmpeg 没有产出视频")
	}
	return m.SaveStudioSequenceVideo(projectID, outName, data)
}

// SaveStudioSequenceVideo 3D 编辑器合成的序列视频入库：同名重复合成只留一条记录，
// 文件同路径覆盖（素材库删记录时按路径清理，不会残留多份）。
func (m *MaterialService) SaveStudioSequenceVideo(projectID *uint, name string, data []byte) (*models.Material, error) {
	m.db.Where("name = ? AND type = ?", name, "video").Delete(&models.Material{})
	return m.saveUpload(projectID, "video", name, data, "", nil, "studio_3d")
}
