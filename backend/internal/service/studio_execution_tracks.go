package service

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"comfyui-console/internal/models"
)

// Optional, explicitly versioned extension; legacy plans do not enable it.
type studioExecutionKey struct {
	At       float64   `json:"at"`
	Position []float64 `json:"position"`
	Target   []float64 `json:"target"`
	FOV      float64   `json:"fov"`
}
type studioExecutionCameraTrack struct {
	Keys []studioExecutionKey `json:"keys"`
}
type studioExecutionClipRef struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Duration float64 `json:"duration"`
}
type studioExecutionPoint struct {
	X float64 `json:"x"`
	Z float64 `json:"z"`
}
type studioExecutionBeat struct {
	Start       float64                `json:"start"`
	End         float64                `json:"end"`
	From        studioExecutionPoint   `json:"from"`
	To          studioExecutionPoint   `json:"to"`
	Yaw         float64                `json:"yaw"`
	Animation   studioExecutionClipRef `json:"animation"`
	SourceIndex *int                   `json:"sourceIndex,omitempty"`
}
type studioExecutionActorTrack struct {
	TargetID   string                `json:"targetId"`
	TargetName string                `json:"targetName"`
	Beats      []studioExecutionBeat `json:"beats"`
}
type studioWristArm struct {
	Shoulder string `json:"shoulder"`
	Elbow    string `json:"elbow"`
	Wrist    string `json:"wrist"`
}
type studioWristContactBinding struct {
	Version      int            `json:"version"`
	Arm          studioWristArm `json:"arm"`
	TargetBone   string         `json:"targetBone"`
	SourceAnchor []float64      `json:"sourceAnchor"`
	TargetAnchor []float64      `json:"targetAnchor"`
	Rotation     []float64      `json:"rotation"`
	Pole         []float64      `json:"pole"`
}
type studioWristContact struct {
	Version  int                       `json:"version"`
	Policy   string                    `json:"policy"`
	SourceID string                    `json:"sourceId"`
	TargetID string                    `json:"targetId"`
	Start    *float64                  `json:"start"`
	End      *float64                  `json:"end"`
	Binding  studioWristContactBinding `json:"binding"`
	Normal   []float64                 `json:"normal"`
}
type studioExecutionTracks struct {
	Version  int                         `json:"version"`
	Camera   *studioExecutionCameraTrack `json:"camera,omitempty"`
	Actors   []studioExecutionActorTrack `json:"actors,omitempty"`
	Contacts []studioWristContact        `json:"contacts,omitempty"`
}

func studioMovingCamera(e *studioExecutionTracks) bool {
	if e == nil || e.Camera == nil || len(e.Camera.Keys) < 2 {
		return false
	}
	a := e.Camera.Keys[0]
	for _, b := range e.Camera.Keys[1:] {
		if a.FOV != b.FOV {
			return true
		}
		for i := range a.Position {
			if a.Position[i] != b.Position[i] || a.Target[i] != b.Target[i] {
				return true
			}
		}
	}
	return false
}
func validateStudioExecutionTracks(e *studioExecutionTracks, original models.Shot, plan *studioExecutionPlan, objects map[string]studioExecutionObject, assets map[string]studioExecutionAsset) error {
	if e == nil {
		return nil
	}
	if e.Version != 1 || (e.Camera == nil && len(e.Actors) == 0) || len(e.Actors) > 32 {
		return fmt.Errorf("镜内执行版本或规模无效")
	}
	if e.Camera != nil {
		keys := e.Camera.Keys
		if len(keys) < 2 || len(keys) > 64 || keys[0].At != 0 || keys[len(keys)-1].At != original.Duration {
			return fmt.Errorf("机位轨道必须覆盖本镜完整时间")
		}
		for i, k := range keys {
			if !previsFinite(k.At, 0, original.Duration) || !studioExecutionVector(k.Position, -1000, 1000) || !studioExecutionVector(k.Target, -1000, 1000) || !previsFinite(k.FOV, 15, 90) {
				return fmt.Errorf("机位键无效")
			}
			if i == 0 {
				continue
			}
			a := keys[i-1]
			if k.At <= a.At {
				return fmt.Errorf("机位键时间不递增")
			}
			dot, denom := 0.0, 0.0
			for j := 0; j < 3; j++ {
				v := a.Position[j] - a.Target[j]
				d := (k.Position[j] - k.Target[j]) - v
				dot += v * d
				denom += d * d
			}
			t := 0.0
			if denom > 0 {
				t = math.Max(0, math.Min(1, -dot/denom))
			}
			dist := 0.0
			for j := 0; j < 3; j++ {
				v := a.Position[j] - a.Target[j]
				d := (k.Position[j] - k.Target[j]) - v
				dist += math.Pow(v+t*d, 2)
			}
			if dist <= 1e-12 {
				return fmt.Errorf("机位轨道内部视点与焦点重合")
			}
		}
	}
	seen := map[string]bool{}
	covered := map[string]bool{}
	count := 0
	for _, a := range e.Actors {
		o, ok := objects[a.TargetID]
		if !ok || seen[a.TargetID] || a.TargetName != o.Name || o.AssetID != "object:"+o.ID || len(a.Beats) == 0 {
			return fmt.Errorf("执行角色不是实际可用对象")
		}
		seen[a.TargetID] = true
		character := ""
		for _, b := range plan.Bindings {
			if b.ObjectID == a.TargetID {
				character = b.Character
			}
		}
		if character == "" {
			return fmt.Errorf("执行角色未绑定可见人物")
		}
		count += len(a.Beats)
		if count > 128 {
			return fmt.Errorf("执行动作段超过128")
		}
		for i, b := range a.Beats {
			if !previsFinite(b.Start, 0, original.Duration) || !previsFinite(b.End, 0, original.Duration) || b.End <= b.Start || !previsFinite(b.Yaw, -2*math.Pi, 2*math.Pi) || !previsFinite(b.From.X, -1000, 1000) || !previsFinite(b.From.Z, -1000, 1000) || !previsFinite(b.To.X, -1000, 1000) || !previsFinite(b.To.Z, -1000, 1000) {
				return fmt.Errorf("执行动作段区间或位置无效")
			}
			if i > 0 {
				prev := a.Beats[i-1]
				if b.Start < prev.End || b.From != prev.To {
					return fmt.Errorf("同角色动作段重叠或位置不连续")
				}
			}
			clips := assets[o.AssetID].Clips
			r := b.Animation
			if r.Index < 0 || r.Index >= len(clips) || !strings.HasPrefix(clips[r.Index].ID, strconv.Itoa(r.Index)+":") || clips[r.Index].Name != r.Name || clips[r.Index].Duration != r.Duration || !previsFinite(r.Duration, 0.000001, 86400) {
				return fmt.Errorf("执行动作不属于当前对象真实clip")
			}
			if len(original.ActionTimeline) > 0 {
				if b.SourceIndex == nil || *b.SourceIndex < 0 || *b.SourceIndex >= len(original.ActionTimeline) {
					return fmt.Errorf("缺少原动作段索引")
				}
				step := original.ActionTimeline[*b.SourceIndex]
				matched := false
				for _, name := range strings.FieldsFunc(step.Subject, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
					if strings.TrimSpace(name) == character {
						matched = true
					}
				}
				if !matched || step.Start != b.Start || step.End != b.End {
					return fmt.Errorf("执行段改变了原动作主体或区间")
				}
				covered[fmt.Sprintf("%d:%s", *b.SourceIndex, character)] = true
			} else if b.SourceIndex != nil {
				return fmt.Errorf("原镜头无动作段，不能虚构来源索引")
			}
		}
	}
	if len(e.Actors) > 0 {
		for i, step := range original.ActionTimeline {
			for _, name := range strings.FieldsFunc(step.Subject, func(r rune) bool { return strings.ContainsRune(",，、;；\n", r) }) {
				if !covered[fmt.Sprintf("%d:%s", i, strings.TrimSpace(name))] {
					return fmt.Errorf("动作时间轴的主体或区间尚未完整绑定")
				}
			}
		}
	}
	return validateStudioWristContacts(e, original.Duration)
}

// Structural/resource checks only. Browser application/playback/export must also
// solve the actual rig and prove every posed skin against the separating plane.
// Version 1 is an ENTIRE-shot surface hold, not approach/release or a closed grip.
func validateStudioWristContacts(e *studioExecutionTracks, duration float64) error {
	if e.Contacts == nil {
		return nil
	}
	if len(e.Contacts) == 0 || len(e.Contacts) > 16 {
		return fmt.Errorf("手腕接触数量无效")
	}
	actors := map[string]studioExecutionActorTrack{}
	for _, actor := range e.Actors {
		actors[actor.TargetID] = actor
	}
	used := map[string]bool{}
	normSquared := func(v []float64) float64 {
		sum := 0.0
		for _, n := range v {
			sum += n * n
		}
		return sum
	}
	for _, c := range e.Contacts {
		a, aok := actors[c.SourceID]
		b, bok := actors[c.TargetID]
		if c.Version != 1 || c.Policy != "whole-skin-separating-plane-v1" || !aok || !bok || c.SourceID == c.TargetID || used[c.SourceID] || used[c.TargetID] || c.Start == nil || c.End == nil || !previsFinite(*c.Start, 0, duration) || *c.Start != 0 || !previsFinite(*c.End, 0, duration+1e-6) || math.Abs(*c.End-duration) > 1e-6 || !studioExecutionVector(c.Normal, -1, 1) || math.Abs(normSquared(c.Normal)-1) > 1e-6 {
			return fmt.Errorf("手腕接触版本、双方、整镜时间窗或法线无效")
		}
		used[c.SourceID], used[c.TargetID] = true, true
		v := c.Binding
		names := []string{v.Arm.Shoulder, v.Arm.Elbow, v.Arm.Wrist, v.TargetBone}
		for _, name := range names {
			if strings.TrimSpace(name) == "" || len(name) > 160 {
				return fmt.Errorf("手腕接触必须指定实际骨名")
			}
		}
		if v.Version != 1 || v.Arm.Shoulder == v.Arm.Elbow || v.Arm.Shoulder == v.Arm.Wrist || v.Arm.Elbow == v.Arm.Wrist || !studioExecutionVector(v.SourceAnchor, -.25, .25) || !studioExecutionVector(v.TargetAnchor, -.25, .25) || len(v.Rotation) != 4 || !studioExecutionVector(v.Pole, -1, 1) || normSquared(v.Pole) < 1e-12 {
			return fmt.Errorf("手腕接触骨链、锚点或肘向无效")
		}
		for _, n := range v.Rotation {
			if !previsFinite(n, -1, 1) {
				return fmt.Errorf("手腕接触旋转无效")
			}
		}
		if math.Abs(normSquared(v.Rotation)-1) > 1e-6 {
			return fmt.Errorf("手腕接触旋转必须单位化")
		}
		// Exact minimum of piecewise linear relative roots, including held gaps.
		// The global .7m floor remains strict; no contact field can turn it off.
		times := []float64{0, duration}
		for _, actor := range []studioExecutionActorTrack{a, b} {
			for _, beat := range actor.Beats {
				times = append(times, beat.Start, beat.End)
			}
		}
		sort.Float64s(times)
		position := func(actor studioExecutionActorTrack, seconds float64) studioExecutionPoint {
			beat := actor.Beats[0]
			for _, next := range actor.Beats {
				if next.Start <= seconds {
					beat = next
				}
			}
			t := math.Max(0, math.Min(1, (seconds-beat.Start)/(beat.End-beat.Start)))
			return studioExecutionPoint{X: beat.From.X + (beat.To.X-beat.From.X)*t, Z: beat.From.Z + (beat.To.Z-beat.From.Z)*t}
		}
		for i := 1; i < len(times); i++ {
			ap, bp := position(a, times[i-1]), position(b, times[i-1])
			aq, bq := position(a, times[i]), position(b, times[i])
			x, z := ap.X-bp.X, ap.Z-bp.Z
			dx, dz := aq.X-bq.X-x, aq.Z-bq.Z-z
			t := 0.0
			if denom := dx*dx + dz*dz; denom > 0 {
				t = math.Max(0, math.Min(1, -(x*dx+z*dz)/denom))
			}
			floor := float64(.7)
			if (x+dx*t)*(x+dx*t)+(z+dz*t)*(z+dz*t) <= floor*floor {
				return fmt.Errorf("手腕接触仍须通过严格0.7米角色根间距")
			}
		}
	}
	return nil
}
