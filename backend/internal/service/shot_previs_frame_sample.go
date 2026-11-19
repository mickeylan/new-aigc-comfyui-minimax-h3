package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// sampleShotPrevisFrame mirrors sampleTimeline; input is copied before sampling.
func sampleShotPrevisFrame(doc previsDocument, timeMS int) (previsDocument, error) {
	if err := validatePrevisDocument(doc); err != nil {
		return doc, err
	}
	if timeMS < 0 || timeMS > doc.DurationMS {
		return doc, fmt.Errorf("选帧时间超出预演范围")
	}
	raw, _ := json.Marshal(doc)
	var frame previsDocument
	_ = json.Unmarshal(raw, &frame)
	if doc.SchemaVersion == 1 {
		return frame, nil
	}
	mix := func(a, b, t float64) float64 { return a + (b-a)*t }
	vector := func(a, b previsVector, t float64) previsVector {
		return previsVector{X: mix(a.X, b.X, t), Y: mix(a.Y, b.Y, t), Z: mix(a.Z, b.Z, t)}
	}
	for i, o := range frame.Objects {
		keys := []previsKeyframe{}
		for _, k := range doc.Keyframes {
			if k.ObjectID == o.ID {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i].TimeMS < keys[j].TimeMS })
		var a, b *previsKeyframe
		for j := range keys {
			if keys[j].TimeMS > timeMS {
				b = &keys[j]
				break
			}
			a = &keys[j]
		}
		if a == nil {
			continue
		}
		frame.Objects[i].Position = a.Position
		frame.Objects[i].Yaw = a.Yaw
		if b != nil {
			t := float64(timeMS-a.TimeMS) / float64(b.TimeMS-a.TimeMS)
			frame.Objects[i].Position = vector(a.Position, b.Position, t)
			delta := math.Mod(math.Mod(b.Yaw-a.Yaw+540, 360)+360, 360) - 180
			frame.Objects[i].Yaw = a.Yaw + delta*t
		}
		frame.Objects[i].Yaw = math.Mod(frame.Objects[i].Yaw+540, 360) - 180
	}
	keys := append([]previsCameraKeyframe{}, doc.CameraKeyframes...)
	sort.Slice(keys, func(i, j int) bool { return keys[i].TimeMS < keys[j].TimeMS })
	var a, b *previsCameraKeyframe
	for j := range keys {
		if keys[j].TimeMS > timeMS {
			b = &keys[j]
			break
		}
		a = &keys[j]
	}
	if a != nil {
		frame.Camera = a.Camera
		if b != nil && !b.Cut {
			t := float64(timeMS-a.TimeMS) / float64(b.TimeMS-a.TimeMS)
			candidate := previsCamera{Position: vector(a.Camera.Position, b.Camera.Position, t), Target: vector(a.Camera.Target, b.Camera.Target, t), FOV: mix(a.Camera.FOV, b.Camera.FOV, t)}
			if validatePrevisCamera(candidate) {
				frame.Camera = candidate
			}
		}
	}
	return frame, nil
}
