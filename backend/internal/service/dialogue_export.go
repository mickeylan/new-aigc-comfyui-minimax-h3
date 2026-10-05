package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"comfyui-console/internal/models"
)

var (
	ErrDialogueExportEpisodeNotFound = errors.New("episode has no scenes")
	ErrDialogueExportNotReady        = errors.New("episode dialogue audio is not ready")
)

type DialogueWAVExport struct {
	Character string `json:"character,omitempty"`
	URL       string `json:"url"`
}

type EpisodeDialogueExport struct {
	EpisodeN int                 `json:"episode_n"`
	Duration float64             `json:"duration"`
	Dialogue *DialogueWAVExport  `json:"dialogue,omitempty"`
	Stems    []DialogueWAVExport `json:"stems,omitempty"`
}

type dialogueExportSelection struct {
	Character string
	Segments  []dubSegment
}

// selectDialogueStems groups timeline segments by their trimmed character name. Sorting makes
// output stable without putting user-controlled character names into filesystem paths.
func selectDialogueStems(segments []dubSegment) []dialogueExportSelection {
	byCharacter := make(map[string][]dubSegment)
	for _, segment := range segments {
		character := strings.TrimSpace(segment.Character)
		byCharacter[character] = append(byCharacter[character], segment)
	}
	characters := make([]string, 0, len(byCharacter))
	for character := range byCharacter {
		characters = append(characters, character)
	}
	sort.Strings(characters)
	out := make([]dialogueExportSelection, 0, len(characters))
	for _, character := range characters {
		out = append(out, dialogueExportSelection{Character: character, Segments: byCharacter[character]})
	}
	return out
}

func buildDialogueExportGraph(segments []dubSegment, totalDuration float64) mergeAudioGraph {
	extras := make([]mergeAudioInput, 0, len(segments))
	for index, segment := range segments {
		extras = append(extras, mergeAudioInput{
			Index: index, Start: segment.Start, Duration: segment.End - segment.Start,
			SourceDuration: segment.SourceDur, Volume: segment.Volume,
			Speed: segment.Speed, Pitch: segment.Pitch, Kind: "dialogue",
		})
	}
	return buildMergeAudioGraph(0, false, totalDuration, 0, extras)
}

func pathInside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// ExportEpisodeDialogueWAV exports either the pure-dialogue episode mix or one full-length stem
// per character. It intentionally uses local FFmpeg because all dialogue assets and served output
// paths are local workspace files.
func (s *ProjectService) ExportEpisodeDialogueWAV(p *models.Project, episodeN int, stems bool) (*EpisodeDialogueExport, error) {
	generation, err := latestEpisodeGeneration(s.db, p.ID, episodeN)
	if err != nil {
		return nil, err
	}
	var scenes []models.Scene
	if err := s.db.Where("project_id = ? AND episode_n = ? AND generation = ?", p.ID, episodeN, generation).Order("`order`, id").Find(&scenes).Error; err != nil {
		return nil, err
	}
	if len(scenes) == 0 {
		return nil, ErrDialogueExportEpisodeNotFound
	}
	sceneIDs := make([]uint, len(scenes))
	videoDurs := make([]sceneVideo, len(scenes))
	for i, scene := range scenes {
		sceneIDs[i] = scene.ID
		videoDurs[i].dur = normalizeSceneDuration(scene.Duration)
	}
	segments, duration, ready, err := s.buildDubTimeline(p, episodeN, sceneIDs, videoDurs)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDialogueExportNotReady, err)
	}
	if !ready || len(segments) == 0 || duration <= 0 {
		return nil, ErrDialogueExportNotReady
	}

	inputRoot := filepath.Join(s.cfg.Comfy.ComfyDir, "input", fmt.Sprint(p.ID))
	for _, segment := range segments {
		if segment.AudioAbs == "" || !pathInside(inputRoot, segment.AudioAbs) {
			return nil, fmt.Errorf("%w: invalid dialogue audio path", ErrDialogueExportNotReady)
		}
		info, err := os.Stat(segment.AudioAbs)
		if err != nil || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%w: dialogue audio file is missing", ErrDialogueExportNotReady)
		}
	}

	ffmpeg, err := localFFmpegPath()
	if err != nil {
		return nil, err
	}
	exportRoot := s.mediaPath("output_workers", "gpu0", "dialogue_exports", fmt.Sprintf("project-%d", p.ID))
	if err := os.MkdirAll(exportRoot, 0o755); err != nil {
		return nil, err
	}
	outputDir, err := os.MkdirTemp(exportRoot, fmt.Sprintf("episode-%d-", episodeN))
	if err != nil {
		return nil, err
	}
	succeeded := false
	defer func() {
		if !succeeded {
			_ = os.RemoveAll(outputDir)
		}
	}()

	render := func(selected []dubSegment, filename string) (DialogueWAVExport, error) {
		graph := buildDialogueExportGraph(selected, duration)
		if graph.Label == "" {
			return DialogueWAVExport{}, ErrDialogueExportNotReady
		}
		outputAbs := filepath.Join(outputDir, filename)
		tempAbs := outputAbs + ".tmp.wav"
		args := []string{"-y"}
		for _, segment := range selected {
			args = append(args, "-i", segment.AudioAbs)
		}
		args = append(args, "-filter_complex", graph.Filters, "-map", graph.Label, "-vn", "-c:a", "pcm_s16le", "-ar", "48000", "-ac", "2", tempAbs)
		if _, err := runLocalProgram(ffmpeg, args, 10*time.Minute); err != nil {
			return DialogueWAVExport{}, err
		}
		if err := os.Rename(tempAbs, outputAbs); err != nil {
			return DialogueWAVExport{}, err
		}
		rel, err := filepath.Rel(s.mediaPath("output_workers", "gpu0"), outputAbs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return DialogueWAVExport{}, fmt.Errorf("invalid dialogue export output path")
		}
		return DialogueWAVExport{URL: "/api/output/0/" + filepath.ToSlash(rel)}, nil
	}

	result := &EpisodeDialogueExport{EpisodeN: episodeN, Duration: duration}
	if stems {
		selections := selectDialogueStems(segments)
		result.Stems = make([]DialogueWAVExport, 0, len(selections))
		for i, selection := range selections {
			item, err := render(selection.Segments, fmt.Sprintf("stem-%03d.wav", i+1))
			if err != nil {
				return nil, err
			}
			item.Character = selection.Character
			result.Stems = append(result.Stems, item)
		}
	} else {
		item, err := render(segments, "dialogue.wav")
		if err != nil {
			return nil, err
		}
		result.Dialogue = &item
	}
	succeeded = true
	return result, nil
}
