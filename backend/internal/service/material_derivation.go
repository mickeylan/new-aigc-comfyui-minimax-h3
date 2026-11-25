package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"regexp"
	"strings"
)

// Audit-only, client-declared snapshot. Never interpreted as a workflow or trusted proof.
const maxMaterialDerivationBytes = 1 << 20

var derivationArtifactID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validateMaterialDerivation(raw, ftype string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil // Existing uploads and old material rows have no provenance.
	}
	invalid := errors.New("派生来源清单无效：仅支持1MiB以内的v1 Studio逐镜WebM清单，请重新导出")
	if ftype != "video" || len(raw) > maxMaterialDerivationBytes {
		return "", invalid
	}
	var doc struct {
		Version           int             `json:"version"`
		Kind              string          `json:"kind"`
		ArtifactID        string          `json:"artifactId"`
		Container         string          `json:"container"`
		Codec             string          `json:"codec"`
		Width             int             `json:"width"`
		Height            int             `json:"height"`
		FPS               int             `json:"fps"`
		FrameCount        int             `json:"frameCount"`
		DurationMS        float64         `json:"durationMs"`
		SourceDurationMS  float64         `json:"sourceDurationMs"`
		TimesMS           []float64       `json:"timesMs"`
		SourceFingerprint string          `json:"sourceFingerprint"`
		Mode              string          `json:"mode"`
		SamplerVersion    string          `json:"samplerVersion"`
		CatalogRevision   int             `json:"characterAnimationCatalogRevision"`
		WalkProof         json.RawMessage `json:"walkProof"`
		Environment       json.RawMessage `json:"environment"`
		Notes             []string        `json:"notes"`
		Shots             struct {
			Version int               `json:"version"`
			Shots   []json.RawMessage `json:"shots"`
		} `json:"shots"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	// Additional fields are preserved as opaque audit data, never executed or silently dropped.
	// Shots/environment/proofs are not accepted as server-side movement or generation validation.
	if err := decoder.Decode(&doc); err != nil {
		return "", invalid
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return "", invalid
	}
	// Exact scene-frame presets only; each remains within the original 1280x720 pixel budget.
	validSize := (doc.Width == 1280 && doc.Height == 720) || (doc.Width == 720 && doc.Height == 1280) || (doc.Width == 960 && doc.Height == 960) || (doc.Width == 1152 && doc.Height == 768) || (doc.Width == 768 && doc.Height == 1152)
	if doc.Version != 1 || doc.Kind != "studio-script-shot-video" || !derivationArtifactID.MatchString(doc.ArtifactID) || doc.Container != "webm" || (doc.Codec != "VP9" && doc.Codec != "VP8") || !validSize {
		return "", invalid
	}
	if doc.FPS != 1 && doc.FPS != 6 && doc.FPS != 12 && doc.FPS != 24 {
		return "", invalid
	}
	if doc.FrameCount < 1 || doc.FrameCount > 2880 || len(doc.TimesMS) != doc.FrameCount || doc.SourceDurationMS <= 0 || doc.SourceDurationMS > doc.DurationMS || math.Abs(doc.DurationMS-float64(doc.FrameCount)*1000/float64(doc.FPS)) > 0.1 {
		return "", invalid
	}
	if len(doc.SourceFingerprint) == 0 || len(doc.SourceFingerprint) > 1024 || doc.SamplerVersion != "script-shot-timeline-v1" || doc.CatalogRevision < 1 || doc.Shots.Version != 1 || len(doc.Shots.Shots) < 1 || len(doc.Shots.Shots) > 64 || len(doc.WalkProof) == 0 || bytes.Equal(doc.WalkProof, []byte("null")) {
		return "", invalid
	}
	switch doc.Mode {
	case "viewport", "white", "normal", "depth":
	default:
		return "", invalid
	}
	for i, milliseconds := range doc.TimesMS {
		if math.Abs(milliseconds-float64(i)*1000/float64(doc.FPS)) > 1 || milliseconds >= doc.SourceDurationMS {
			return "", invalid
		}
	}
	// Preserve the complete original snapshot (including future optional audit fields).
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(raw)) != nil {
		return "", invalid
	}
	return compact.String(), nil
}
