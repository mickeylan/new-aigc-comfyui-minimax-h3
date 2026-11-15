package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"comfyui-console/internal/models"
)

// Local burned-in subtitle check. Frames are sampled with ffmpeg and inspected for a
// subtitle-like text band in the lower-centre of the picture whose content changes across
// samples. It reads pixels only, calls no model or paid service, never modifies the video
// and only produces advisory timestamp hints; "uncertain" needs a manual decision.
const (
	subtitleStatusClean     = "clean"
	subtitleStatusSubtitles = "subtitles"
	subtitleStatusUncertain = "uncertain"
	subtitleSampleWidth     = 640
	maxSubtitleSamples      = 120
	maxSubtitleVideoSeconds = 600
	subtitleEdgeThreshold   = 48
	subtitleSignatureBins   = 48
)

var errSubtitleCheckBusy = errors.New("该Scene正在做字幕检测，请等待完成")

type grayFrame struct {
	W, H int
	Pix  []byte
}

type subtitleBand struct {
	Y0, Y1 int // text rows, inclusive
	X0, X1 int // horizontal edge extent, inclusive
	Sig    []bool
}

type SubtitleHint struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

type SubtitleCheckView struct {
	Status    string         `json:"status"`
	Detail    string         `json:"detail"`
	Hints     []SubtitleHint `json:"hints"`
	Samples   int            `json:"samples"`
	Decision  string         `json:"decision"`
	DecidedAt *time.Time     `json:"decided_at"`
	Stale     bool           `json:"stale"`
	CheckedAt time.Time      `json:"checked_at"`
}

// findSubtitleBand returns the strongest subtitle-like text band of one frame, or nil.
// A band is a short run of edge-dense rows in the lower 55–97% of the frame that is much
// denser than the rows around it (rejecting uniform texture such as grass or rain) and whose
// edges form a horizontally centred, wide, mostly continuous strip.
func findSubtitleBand(f grayFrame) *subtitleBand {
	if f.W < 64 || f.H < 64 || len(f.Pix) < f.W*f.H {
		return nil
	}
	x0, x1 := f.W*15/100, f.W*85/100
	edge := func(x, y int) bool {
		a, b := int(f.Pix[y*f.W+x]), int(f.Pix[y*f.W+x+1])
		return a-b >= subtitleEdgeThreshold || b-a >= subtitleEdgeThreshold
	}
	counts := make([]int, f.H)
	for y := f.H * 40 / 100; y < f.H; y++ {
		for x := x0; x < x1; x++ {
			if edge(x, y) {
				counts[y]++
			}
		}
	}
	minCount := f.W / 60
	if minCount < 6 {
		minCount = 6
	}
	meanRows := func(a, b int) float64 {
		a, b = max(a, f.H*40/100), min(b, f.H-1)
		if b < a {
			return 0
		}
		sum := 0
		for y := a; y <= b; y++ {
			sum += counts[y]
		}
		return float64(sum) / float64(b-a+1)
	}
	var best *subtitleBand
	bestDensity := 0.0
	top, bottom := f.H*55/100, f.H*97/100
	for y := top; y <= bottom; {
		if counts[y] < minCount {
			y++
			continue
		}
		start, end := y, y
		for next := y + 1; next <= bottom; next++ {
			if counts[next] >= minCount {
				end = next
			} else if next-end > 1 {
				break
			}
		}
		y = end + 1
		height := end - start + 1
		if float64(height) < 0.012*float64(f.H) || float64(height) > 0.10*float64(f.H) {
			continue
		}
		density := meanRows(start, end)
		if surround := math.Max(meanRows(start-height, start-1), meanRows(end+1, end+height)); surround*2.5 > density {
			continue
		}
		cols := make([]int, f.W)
		for yy := start; yy <= end; yy++ {
			for x := x0; x < x1; x++ {
				if edge(x, yy) {
					cols[x]++
				}
			}
		}
		left, right := -1, -1
		for x := x0; x < x1; x++ {
			if cols[x] >= 2 {
				if left < 0 {
					left = x
				}
				right = x
			}
		}
		if left < 0 {
			continue
		}
		width := right - left + 1
		centre := float64(left+right) / 2 / float64(f.W)
		if float64(width) < 0.10*float64(f.W) || float64(width) < 1.8*float64(height) || centre < 0.35 || centre > 0.65 {
			continue
		}
		// Text strips are mostly continuous; two isolated objects at the same height are not.
		covered := 0
		for x := left; x <= right; x++ {
			if cols[x] > 0 {
				covered++
			}
		}
		if float64(covered) < 0.25*float64(width) {
			continue
		}
		if density > bestDensity {
			bestDensity = density
			best = &subtitleBand{Y0: start, Y1: end, X0: left, X1: right, Sig: subtitleSignature(f, start, end, x0, x1, edge)}
		}
	}
	return best
}

// subtitleSignature is a coarse layout fingerprint of the band: for its top and bottom halves,
// whether each of 48 horizontal bins holds edges. Different subtitle lines differ in it.
func subtitleSignature(f grayFrame, y0, y1, x0, x1 int, edge func(x, y int) bool) []bool {
	sig := make([]bool, 2*subtitleSignatureBins)
	mid := (y0 + y1 + 1) / 2
	span := float64(x1 - x0)
	for half, rows := range [][2]int{{y0, mid - 1}, {mid, y1}} {
		if rows[1] < rows[0] {
			rows[1] = rows[0]
		}
		for bin := 0; bin < subtitleSignatureBins; bin++ {
			bx0 := x0 + int(span*float64(bin)/subtitleSignatureBins)
			bx1 := x0 + int(span*float64(bin+1)/subtitleSignatureBins)
			hits, total := 0, 0
			for y := rows[0]; y <= rows[1]; y++ {
				for x := bx0; x < bx1; x++ {
					total++
					if edge(x, y) {
						hits++
					}
				}
			}
			sig[half*subtitleSignatureBins+bin] = total > 0 && float64(hits)/float64(total) > 0.04
		}
	}
	return sig
}

func subtitleSignatureDistance(a, b []bool) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 1
	}
	diff, used := 0, 0
	for i := range a {
		if a[i] || b[i] {
			used++
			if a[i] != b[i] {
				diff++
			}
		}
	}
	if used == 0 {
		return 0
	}
	return float64(diff) / float64(used)
}

// classifySubtitleSamples mirrors the reference rule: a text band at a stable height whose
// content changes across samples is a subtitle; text that never changes (signboard,
// watermark, texture) is uncertain; no band is clean. Hints merge adjacent sample times.
func classifySubtitleSamples(times []float64, step float64, bands []*subtitleBand, frameHeight int) (string, string, []SubtitleHint) {
	groups := map[int][]int{}
	any := false
	for i, band := range bands {
		if band == nil {
			continue
		}
		any = true
		centre := float64(band.Y0+band.Y1) / 2 / float64(frameHeight)
		key := int(math.Round(centre / 0.05))
		for _, k := range []int{key - 1, key, key + 1} {
			groups[k] = append(groups[k], i)
		}
	}
	if !any {
		return subtitleStatusClean, "本地抽帧未在画面下方居中区域检出字幕；短暂、小字或其他位置的字幕仍可能漏检", nil
	}
	hints := []SubtitleHint{}
	for i := 0; i < len(bands); i++ {
		if bands[i] == nil {
			continue
		}
		j := i
		for j+1 < len(bands) && bands[j+1] != nil {
			j++
		}
		hints = append(hints, SubtitleHint{Start: math.Round(times[i]*10) / 10, End: math.Round((times[j]+step)*10) / 10})
		i = j
		if len(hints) == 20 {
			break
		}
	}
	for _, members := range groups {
		if len(members) < 3 {
			continue
		}
		for a := 0; a < len(members); a++ {
			for b := a + 1; b < len(members); b++ {
				if subtitleSignatureDistance(bands[members[a]].Sig, bands[members[b]].Sig) >= 0.25 {
					return subtitleStatusSubtitles, "多帧在画面下方同一高度检出内容变化的文字条带，疑似烧录字幕", hints
				}
			}
		}
	}
	return subtitleStatusUncertain, "检出疑似文字条带，但未见多帧内容变化，可能是招牌、水印或画面纹理，请人工确认", hints
}

func subtitleSourceKey(source string) (string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return "", fmt.Errorf("Scene视频不可读: %w", err)
	}
	return fmt.Sprintf("%s|%d|%d", source, info.Size(), info.ModTime().UnixNano()), nil
}

// detectBurnedSubtitles samples up to 120 grey frames (two per second) with ffmpeg and classifies them.
func detectBurnedSubtitles(source string) (string, string, []SubtitleHint, int, error) {
	media, err := probeLocalVideoWithFFprobe(source, &MediaInfo{})
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("读取视频信息失败: %w", err)
	}
	if media.Duration <= 0 || media.Duration > maxSubtitleVideoSeconds || media.Width <= 0 || media.Height <= 0 {
		return "", "", nil, 0, fmt.Errorf("视频时长无效或超过10分钟")
	}
	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return "", "", nil, 0, err
	}
	total := int(math.Min(maxSubtitleSamples, math.Max(2, math.Ceil(media.Duration*2))))
	w := subtitleSampleWidth
	h := int(math.Round(float64(w)*float64(media.Height)/float64(media.Width)/2)) * 2
	if h < 64 {
		return "", "", nil, 0, fmt.Errorf("视频画面过扁，无法检测字幕")
	}
	var raw []byte
	err = withTempDir(func(dir string) error {
		out := filepath.Join(dir, "frames.gray")
		args := []string{"-hide_banner", "-loglevel", "error", "-y", "-i", source,
			"-vf", fmt.Sprintf("fps=%.6f,scale=%d:%d,format=gray", float64(total)/media.Duration, w, h),
			"-frames:v", fmt.Sprint(total), "-f", "rawvideo", "-pix_fmt", "gray", out}
		if _, runErr := runLocalProgram(ffmpeg, args, 3*time.Minute); runErr != nil {
			return fmt.Errorf("视频抽帧失败: %w", runErr)
		}
		var readErr error
		raw, readErr = os.ReadFile(out)
		return readErr
	})
	if err != nil {
		return "", "", nil, 0, err
	}
	frameSize := w * h
	count := len(raw) / frameSize
	if count < 2 {
		return "", "", nil, 0, fmt.Errorf("视频抽帧不完整，未完成字幕检测")
	}
	step := media.Duration / float64(total)
	times := make([]float64, count)
	bands := make([]*subtitleBand, count)
	for i := 0; i < count; i++ {
		times[i] = float64(i) * step
		bands[i] = findSubtitleBand(grayFrame{W: w, H: h, Pix: raw[i*frameSize : (i+1)*frameSize]})
	}
	status, detail, hints := classifySubtitleSamples(times, step, bands, h)
	return status, detail, hints, count, nil
}

func (s *ProjectService) claimSubtitleCheck(sceneID uint) (func(), error) {
	s.subtitleMu.Lock()
	defer s.subtitleMu.Unlock()
	if s.subtitleBusy == nil {
		s.subtitleBusy = map[uint]bool{}
	}
	if s.subtitleBusy[sceneID] {
		return nil, errSubtitleCheckBusy
	}
	s.subtitleBusy[sceneID] = true
	return func() {
		s.subtitleMu.Lock()
		delete(s.subtitleBusy, sceneID)
		s.subtitleMu.Unlock()
	}, nil
}

func (s *ProjectService) subtitleCheckView(p *models.Project, scene *models.Scene, row *models.SceneSubtitleCheck) *SubtitleCheckView {
	view := &SubtitleCheckView{Status: row.Status, Detail: row.Detail, Hints: []SubtitleHint{}, Samples: row.Samples, Decision: row.Decision, DecidedAt: row.DecidedAt, CheckedAt: row.UpdatedAt}
	_ = json.Unmarshal([]byte(row.HintsJSON), &view.Hints)
	source, err := s.sceneVideoSource(p, scene)
	key := ""
	if err == nil {
		key, err = subtitleSourceKey(source)
	}
	view.Stale = err != nil || key != row.SourceKey
	return view
}

// SceneSubtitleCheck returns the saved result, or nil when the Scene was never checked.
func (s *ProjectService) SceneSubtitleCheck(p *models.Project, scene *models.Scene) (*SubtitleCheckView, error) {
	var row models.SceneSubtitleCheck
	err := s.db.Where("scene_id = ? AND project_id = ?", scene.ID, p.ID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.subtitleCheckView(p, scene, &row), nil
}

// RunSceneSubtitleCheck analyses the current Scene video and replaces the previous result,
// clearing any earlier manual decision because it referred to the earlier analysis.
func (s *ProjectService) RunSceneSubtitleCheck(p *models.Project, scene *models.Scene) (*SubtitleCheckView, error) {
	if s.remote != nil && s.remote.Enabled() {
		return nil, fmt.Errorf("SSH远程模式暂不支持本地字幕检测")
	}
	release, err := s.claimSubtitleCheck(scene.ID)
	if err != nil {
		return nil, err
	}
	defer release()
	source, err := s.sceneVideoSource(p, scene)
	if err != nil {
		return nil, err
	}
	key, err := subtitleSourceKey(source)
	if err != nil {
		return nil, err
	}
	status, detail, hints, samples, err := detectBurnedSubtitles(source)
	if err != nil {
		return nil, err
	}
	if after, keyErr := subtitleSourceKey(source); keyErr != nil || after != key {
		return nil, fmt.Errorf("检测期间Scene视频已被替换，请重新检测")
	}
	hintsJSON, _ := json.Marshal(hints)
	row := models.SceneSubtitleCheck{ProjectID: p.ID, SceneID: scene.ID, SourceKey: key, Status: status, Detail: detail, HintsJSON: string(hintsJSON), Samples: samples}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "scene_id"}},
		DoUpdates: clause.Assignments(map[string]any{"project_id": p.ID, "source_key": key, "status": status, "detail": detail, "hints_json": string(hintsJSON), "samples": samples, "decision": "", "decided_at": nil, "updated_at": time.Now()}),
	}).Create(&row).Error; err != nil {
		return nil, err
	}
	return s.SceneSubtitleCheck(p, scene)
}

// DecideSceneSubtitleCheck records the user's manual verdict for the current analysis.
func (s *ProjectService) DecideSceneSubtitleCheck(p *models.Project, scene *models.Scene, decision string) (*SubtitleCheckView, error) {
	if decision != "has_subtitles" && decision != "no_subtitles" {
		return nil, fmt.Errorf("人工判断只能是 has_subtitles 或 no_subtitles")
	}
	view, err := s.SceneSubtitleCheck(p, scene)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, fmt.Errorf("请先运行字幕检测")
	}
	if view.Stale {
		return nil, fmt.Errorf("Scene视频已变化，请重新检测后再判断")
	}
	now := time.Now()
	if err := s.db.Model(&models.SceneSubtitleCheck{}).Where("scene_id = ? AND project_id = ?", scene.ID, p.ID).
		Updates(map[string]any{"decision": decision, "decided_at": &now}).Error; err != nil {
		return nil, err
	}
	return s.SceneSubtitleCheck(p, scene)
}

func (s *Service) HandleGetSceneSubtitleCheck(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	view, err := s.Projects.SceneSubtitleCheck(p, sc)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"check": view})
}

func (s *Service) HandleRunSceneSubtitleCheck(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	view, err := s.Projects.RunSceneSubtitleCheck(p, sc)
	if errors.Is(err, errSubtitleCheckBusy) {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"check": view})
}

func (s *Service) HandleDecideSceneSubtitleCheck(c *gin.Context) {
	p, ok := s.loadProject(c)
	if !ok {
		return
	}
	sc, ok := s.loadScene(c)
	if !ok {
		return
	}
	var req struct {
		Decision string `json:"decision"`
	}
	if c.ShouldBindJSON(&req) != nil {
		c.JSON(400, gin.H{"error": "参数错误"})
		return
	}
	view, err := s.Projects.DecideSceneSubtitleCheck(p, sc, strings.TrimSpace(req.Decision))
	if err != nil {
		c.JSON(409, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"check": view})
}
