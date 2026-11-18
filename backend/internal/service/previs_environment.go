package service

import (
	"fmt"
	"regexp"
	"strings"
)

type previsWeather struct {
	SunElevation float64 `json:"sun_elevation"`
	FogDensity   float64 `json:"fog_density"`
	Preset       string  `json:"preset"`
	Intensity    float64 `json:"intensity"`
	Seed         int64   `json:"seed"`
}
type previsModelBinding struct {
	ObjectID  string `json:"object_id"`
	AssetHash string `json:"asset_hash"`
	FileName  string `json:"file_name"`
	ClipIndex int    `json:"clip_index"`
	StartMS   int    `json:"start_ms"`
	EndMS     int    `json:"end_ms"`
	Loop      bool   `json:"loop"`
}

var previsAssetHash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func validatePrevisEnvironment(doc previsDocument) error {
	if doc.SchemaVersion < 3 && (doc.Weather != nil || len(doc.Models) > 0) {
		return fmt.Errorf("天气与模型绑定需要预演v3")
	}
	if w := doc.Weather; w != nil {
		valid := map[string]bool{"clear": true, "overcast": true, "rain": true, "snow": true, "fog": true}
		if !valid[w.Preset] || !previsFinite(w.SunElevation, 0, 90) || !previsFinite(w.FogDensity, 0, .1) || !previsFinite(w.Intensity, 0, 1) || w.Seed < 0 || w.Seed > 2147483647 {
			return fmt.Errorf("天气参数无效")
		}
	}
	if len(doc.Models) > 32 {
		return fmt.Errorf("模型绑定超过32个")
	}
	ids := map[string]bool{}
	for _, o := range doc.Objects {
		ids[o.ID] = true
	}
	seen := map[string]bool{}
	for _, m := range doc.Models {
		if !ids[m.ObjectID] || seen[m.ObjectID] || !previsAssetHash.MatchString(m.AssetHash) || strings.TrimSpace(m.FileName) == "" || len(m.FileName) > 640 || len([]rune(m.FileName)) > 160 || m.ClipIndex < -1 || m.ClipIndex > 255 || m.StartMS < 0 || m.EndMS <= m.StartMS || m.EndMS > doc.DurationMS {
			return fmt.Errorf("模型或动画片段绑定无效")
		}
		seen[m.ObjectID] = true
	}
	return nil
}
