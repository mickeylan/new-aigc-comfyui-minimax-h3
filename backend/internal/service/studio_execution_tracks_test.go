package service

import (
	"bytes"
	"comfyui-console/internal/models"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
)

func executionTracksFixture() (studioExecutionSource, studioExecutionContext, studioExecutionPlan) {
	source := studioExecutionSource{Scene: models.Scene{Content: "甲停下后挥手。", CharacterRolesSet: true, VisibleCharacters: "甲"}, Shots: []models.Shot{{ID: 1, Duration: 4, CameraMovement: "推近", Performance: "停下后挥手", ActionTimeline: []models.ShotActionTimelineEntry{{Start: 0, End: 2, Subject: "甲"}, {Start: 2, End: 4, Subject: "甲"}}}}}
	ctx := executionContextFixture()
	ctx.Assets[0].ID = "object:existing"
	ctx.Assets[0].Clips[0].ID = "0:uuid"
	ctx.Objects[0].AssetID = "object:existing"
	p := executionPlanFixture(source, "hash")
	p.Bindings = []studioExecutionBinding{{Character: "甲", ObjectID: "existing"}}
	zero, one := 0, 1
	p.Shots[0].Camera = nil
	p.Shots[0].Execution = &studioExecutionTracks{Version: 1, Camera: &studioExecutionCameraTrack{Keys: []studioExecutionKey{{At: 0, Position: []float64{0, 2, 5}, Target: []float64{0, 1, 0}, FOV: 45}, {At: 4, Position: []float64{0, 2, 3}, Target: []float64{0, 1, 0}, FOV: 45}}}, Actors: []studioExecutionActorTrack{{TargetID: "existing", TargetName: "韩立", Beats: []studioExecutionBeat{{Start: 0, End: 2, From: studioExecutionPoint{}, To: studioExecutionPoint{}, Animation: studioExecutionClipRef{Index: 0, Name: "walk", Duration: 1}, SourceIndex: &zero}, {Start: 2, End: 4, From: studioExecutionPoint{}, To: studioExecutionPoint{}, Animation: studioExecutionClipRef{Index: 0, Name: "walk", Duration: 1}, SourceIndex: &one}}}}}
	return source, ctx, p
}
func wristTracksFixture() (*studioExecutionTracks, models.Shot, studioExecutionPlan, map[string]studioExecutionObject, map[string]studioExecutionAsset) {
	source, ctx, p := executionTracksFixture()
	e := p.Shots[0].Execution
	other := e.Actors[0]
	other.TargetID, other.TargetName = "other", "乙"
	other.Beats = append([]studioExecutionBeat(nil), other.Beats...)
	for i := range other.Beats {
		other.Beats[i].From.X, other.Beats[i].To.X = .85, .85
	}
	e.Actors = append(e.Actors, other)
	p.Bindings = append(p.Bindings, studioExecutionBinding{Character: "乙", ObjectID: "other"})
	for i := range source.Shots[0].ActionTimeline {
		source.Shots[0].ActionTimeline[i].Subject = "甲，乙"
	}
	objects := map[string]studioExecutionObject{ctx.Objects[0].ID: ctx.Objects[0]}
	o := ctx.Objects[0]
	o.ID, o.Name, o.AssetID = "other", "乙", "object:other"
	objects[o.ID] = o
	asset := ctx.Assets[0]
	assets := map[string]studioExecutionAsset{asset.ID: asset}
	asset.ID = o.AssetID
	assets[asset.ID] = asset
	start, end := 0.0, 4.0
	e.Contacts = []studioWristContact{{Version: 1, Policy: "whole-skin-separating-plane-v1", SourceID: "existing", TargetID: "other", Start: &start, End: &end, Normal: []float64{-1, 0, 0},
		Binding: studioWristContactBinding{Version: 1, Arm: studioWristArm{Shoulder: "leftUpperArm", Elbow: "leftForeArm", Wrist: "leftHand"}, TargetBone: "rightHand", SourceAnchor: []float64{.066, -.02, .0047}, TargetAnchor: []float64{-.066, -.02, .0047}, Rotation: []float64{0, 0, 0, 1}, Pole: []float64{0, 0, 1}}}}
	return e, source.Shots[0], p, objects, assets
}

func TestStudioWristContactContractRoundtrip(t *testing.T) {
	e, original, p, objects, assets := wristTracksFixture()
	if err := validateStudioExecutionTracks(e, original, &p, objects, assets); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var restored studioExecutionTracks
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Contacts) != 1 || restored.Contacts[0].Binding.TargetAnchor[0] != -.066 {
		t.Fatal("contact contract was lost")
	}
	if err := validateStudioExecutionTracks(&restored, original, &p, objects, assets); err != nil {
		t.Fatal(err)
	}
	unknown := strings.Replace(string(data), `"policy":`, `"typo":`, 1)
	decoder = json.NewDecoder(strings.NewReader(unknown))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&restored); err == nil {
		t.Fatal("unknown contact field accepted")
	}
}

func TestStudioWristContactRejectsMalformedOrUnboundContracts(t *testing.T) {
	cases := map[string]func(*studioExecutionTracks){
		"version":          func(e *studioExecutionTracks) { e.Contacts[0].Version = 2 },
		"policy":           func(e *studioExecutionTracks) { e.Contacts[0].Policy = "ignore-pair" },
		"unbound":          func(e *studioExecutionTracks) { e.Contacts[0].TargetID = "missing" },
		"self":             func(e *studioExecutionTracks) { e.Contacts[0].TargetID = "existing" },
		"duplicate":        func(e *studioExecutionTracks) { e.Contacts = append(e.Contacts, e.Contacts[0]) },
		"missing-start":    func(e *studioExecutionTracks) { e.Contacts[0].Start = nil },
		"partial-window":   func(e *studioExecutionTracks) { *e.Contacts[0].End = 3 },
		"zero-normal":      func(e *studioExecutionTracks) { e.Contacts[0].Normal = []float64{0, 0, 0} },
		"nan-normal":       func(e *studioExecutionTracks) { e.Contacts[0].Normal[0] = math.NaN() },
		"rotation":         func(e *studioExecutionTracks) { e.Contacts[0].Binding.Rotation[3] = 2 },
		"oversized-anchor": func(e *studioExecutionTracks) { e.Contacts[0].Binding.SourceAnchor[0] = .26 },
		"pole":             func(e *studioExecutionTracks) { e.Contacts[0].Binding.Pole = []float64{0, 0, 0} },
		"arm-chain":        func(e *studioExecutionTracks) { e.Contacts[0].Binding.Arm.Elbow = "leftHand" },
		"missing-binding":  func(e *studioExecutionTracks) { e.Contacts[0].Binding.Version = 0 },
		"root-floor": func(e *studioExecutionTracks) {
			for i := range e.Actors[1].Beats {
				e.Actors[1].Beats[i].From.X, e.Actors[1].Beats[i].To.X = .7, .7
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			e, original, p, objects, assets := wristTracksFixture()
			change(e)
			if err := validateStudioExecutionTracks(e, original, &p, objects, assets); err == nil {
				t.Fatal("unsafe contact accepted")
			}
		})
	}
}

func TestStudioExecutionTracksAcceptAuditedInPlaceMultipleBeatsAndCamera(t *testing.T) {
	source, ctx, p := executionTracksFixture()
	if err := validateStudioExecutionPlan(&p, source, ctx, "hash"); err != nil {
		t.Fatal(err)
	}
	if len(p.Blockers) != 0 || len(p.Shots[0].Blockers) != 0 {
		t.Fatalf("fully covered tracks blocked: %+v", p)
	}
}
func TestStudioExecutionTracksPlanWideUnionLimit(t *testing.T) {
	for _, count := range []int{32, 34} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source, ctx, p := executionTracksFixture()
			firstSource, firstPlan, firstAsset := source.Shots[0], p.Shots[0], ctx.Assets[0]
			ctx.Assets = nil
			ctx.Objects, p.Objects, p.Bindings = nil, []studioExecutionObject{}, []studioExecutionBinding{}
			names := make([]string, count)
			for i := range names {
				names[i] = fmt.Sprintf("角色%d", i)
				asset := firstAsset
				asset.ID = fmt.Sprintf("object:actor%d", i)
				ctx.Assets = append(ctx.Assets, asset)
				o := studioExecutionObject{ID: fmt.Sprintf("actor%d", i), AssetID: asset.ID, Name: names[i], Position: []float64{float64(i * 3), 0, 0}, Rotation: []float64{0, 0, 0}, Scale: []float64{1, 1, 1}}
				ctx.Objects = append(ctx.Objects, o)
				p.Bindings = append(p.Bindings, studioExecutionBinding{Character: names[i], ObjectID: o.ID})
			}
			source.Scene.VisibleCharacters = strings.Join(names, "，")
			source.Shots, p.Shots = nil, nil
			for cut := 0; cut < 2; cut++ {
				s := firstSource
				s.ID = uint(cut + 1)
				start, end := cut*count/2, (cut+1)*count/2
				s.ActionTimeline = []models.ShotActionTimelineEntry{{Start: 0, End: 4, Subject: strings.Join(names[start:end], "，")}}
				shot := firstPlan
				shot.ShotID = s.ID
				shot.Execution = &studioExecutionTracks{Version: 1, Camera: firstPlan.Execution.Camera}
				for i := start; i < end; i++ {
					zero := 0
					point := studioExecutionPoint{X: float64(i * 3)}
					shot.Execution.Actors = append(shot.Execution.Actors, studioExecutionActorTrack{TargetID: ctx.Objects[i].ID, TargetName: names[i], Beats: []studioExecutionBeat{{Start: 0, End: 4, From: point, To: point, Animation: firstPlan.Execution.Actors[0].Beats[0].Animation, SourceIndex: &zero}}})
				}
				source.Shots = append(source.Shots, s)
				p.Shots = append(p.Shots, shot)
			}
			// Both individual cuts pass the complete mirrored validator (17 each).
			for cut := range p.Shots {
				singleSource, singlePlan := source, p
				singleSource.Shots = source.Shots[cut : cut+1]
				singlePlan.Shots = p.Shots[cut : cut+1]
				if err := validateStudioExecutionPlan(&singlePlan, singleSource, ctx, "hash"); err != nil {
					t.Fatalf("individual cut rejected: %v", err)
				}
			}
			err := validateStudioExecutionPlan(&p, source, ctx, "hash")
			if count == 32 && err != nil {
				t.Fatal(err)
			}
			if count == 34 && (err == nil || err.Error() != "整份执行计划超过32个角色，请拆分场次") {
				t.Fatalf("expected plan-wide union rejection, got %v", err)
			}
		})
	}
}

func TestStudioExecutionTracksRejectInvalidCoverageAndResources(t *testing.T) {
	cases := []struct {
		name   string
		change func(*studioExecutionSource, *studioExecutionContext, *studioExecutionPlan)
	}{
		{"version", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Version = 2
		}},
		{"incomplete-camera", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Camera.Keys[1].At = 3
		}},
		{"interior-eye-crossing", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			k := p.Shots[0].Execution.Camera.Keys
			k[0].Target = []float64{0, 2, 0}
			k[1].Target = []float64{0, 2, 0}
			k[1].Position = []float64{0, 2, -5}
		}},
		{"unbound", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) { p.Bindings = nil }},
		{"wrong-object", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Actors[0].TargetID = "missing"
		}},
		{"wrong-clip", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Actors[0].Beats[0].Animation.Name = "invented"
		}},
		{"wrong-clip-index", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			c.Assets[0].Clips[0].ID = "1:uuid"
		}},
		{"changed-interval", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Actors[0].Beats[0].End = 1
		}},
		{"changed-subject", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			s.Shots[0].ActionTimeline[0].Subject = "乙"
		}},
		{"missing-source", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Actors[0].Beats[0].SourceIndex = nil
		}},
		{"uncovered-beat", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Execution.Actors[0].Beats = p.Shots[0].Execution.Actors[0].Beats[:1]
		}},
		{"uncovered-multiple-subject", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			s.Shots[0].ActionTimeline[0].Subject = "甲，乙"
		}},
		{"legacy-mixed", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			p.Shots[0].Motion = &studioExecutionMotion{}
		}},
		{"legacy-mixed-across-cuts", func(s *studioExecutionSource, c *studioExecutionContext, p *studioExecutionPlan) {
			s.Shots = append(s.Shots, models.Shot{ID: 2, Duration: 2})
			p.Shots = append(p.Shots, studioExecutionShot{ShotID: 2, Duration: 2, Camera: &studioExecutionCamera{Position: []float64{0, 2, 5}, Target: []float64{0, 1, 0}, FOV: 45}, Motion: &studioExecutionMotion{ObjectID: "existing", ClipID: "0:uuid", Start: []float64{0, 0, 0}, End: []float64{1, 0, 0}, Duration: 1}, Evidence: []string{s.Scene.Content}, Assumptions: []string{}, Blockers: []string{}})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c, p := executionTracksFixture()
			tc.change(&s, &c, &p)
			if validateStudioExecutionPlan(&p, s, c, "hash") == nil {
				t.Fatal("invalid execution accepted")
			}
		})
	}
}
