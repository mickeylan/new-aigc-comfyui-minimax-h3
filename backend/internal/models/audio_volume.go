package models

// AudioVolumePoint uses local seconds after a layer starts, not source-file time.
// Gains multiply the layer's base volume; adjacent points interpolate linearly.
type AudioVolumePoint struct {
	Time float64 `json:"time"`
	Gain float64 `json:"gain"`
}
