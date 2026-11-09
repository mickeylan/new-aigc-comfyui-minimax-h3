package service

import (
	"comfyui-console/internal/models"
	"encoding/json"
	"strings"
	"testing"
)

func TestAudioLayerVolumeEnvelopePersistenceAndValidation(t *testing.T) {
	db := newTestDB(t)
	if err := db.AutoMigrate(&models.AudioLayer{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	service := NewAudioLayerService(db)
	points := []models.AudioVolumePoint{{Time: 0, Gain: 0}, {Time: 2, Gain: 1}}
	input := AudioLayerInput{EpisodeN: 1, Kind: "bgm", StartTime: 0, EndTime: 4, File: "music.wav", VolumeEnvelope: &points}
	layer, err := service.Create(1, input)
	if err != nil {
		t.Fatal(err)
	}
	list, err := service.List(1, 1)
	if err != nil || len(list) != 1 || len(list[0].VolumeEnvelope) != 2 {
		t.Fatalf("roundtrip %+v %v", list, err)
	}
	input.VolumeEnvelope = nil
	input.Name = "retitled"
	layer, err = service.Update(1, layer.ID, input)
	if err != nil || len(layer.VolumeEnvelope) != 2 {
		t.Fatalf("omitted update lost envelope: %+v %v", layer, err)
	}
	invalid := []models.AudioVolumePoint{{Time: 5, Gain: 1}}
	input.VolumeEnvelope = &invalid
	if _, err = service.Update(1, layer.ID, input); err == nil {
		t.Fatal("overflow accepted")
	}
	db.First(layer, layer.ID)
	if len(layer.VolumeEnvelope) != 2 {
		t.Fatal("invalid update mutated record")
	}
	empty := []models.AudioVolumePoint{}
	input.VolumeEnvelope = &empty
	layer, err = service.Update(1, layer.ID, input)
	if err != nil || len(layer.VolumeEnvelope) != 0 {
		t.Fatalf("explicit clear failed %v", err)
	}
}
func TestAudioEnvelopeSliceAndOutputLocalGraph(t *testing.T) {
	points := []clipVolumePoint{{Time: 0, Gain: 0}, {Time: 4, Gain: 1}, {Time: 8, Gain: 0}}
	sliced := sliceVolumeEnvelope(points, 2, 6, 2)
	if len(sliced) != 3 || sliced[0].Gain != .5 || sliced[1].Time != 1 || sliced[1].Gain != 1 || sliced[2].Time != 2 || sliced[2].Gain != .5 {
		t.Fatalf("bad clipped/sped envelope: %+v", sliced)
	}
	graph := buildMergeAudioGraph(1, false, 5, 1, []mergeAudioInput{{Index: 1, Start: 2, Duration: 2, Volume: 1, VolumeEnvelope: sliced}})
	if strings.Index(graph.Filters, ":eval=frame") > strings.Index(graph.Filters, "adelay=2000") || !strings.Contains(graph.Filters, "volume='if(") {
		t.Fatalf("envelope must run before output delay: %s", graph.Filters)
	}
	before, _ := json.Marshal(mergeAudioLayerFingerprint{VolumeEnvelope: points})
	after, _ := json.Marshal(mergeAudioLayerFingerprint{VolumeEnvelope: sliced})
	if string(before) == string(after) {
		t.Fatal("snapshot ignores envelope")
	}
}
