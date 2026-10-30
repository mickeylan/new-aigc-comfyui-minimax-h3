package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

const bilingualRef2VMaster = `subject_definitions:
<Subject 1> 是 <Picture 1> 中的阿青。<Picture 2> 是茶馆场景。<Picture 3> 是 0.00 秒起始帧。

summary:
阿青在茶馆说话。

retention_analysis:
<Subject 1> 与场景和起始帧保持一致。

detailed_description:
[Shot 1] [0.00s] <Subject 1> (S1) says <d>[Chinese] 你好，茶还热吗？</d>，随后看向门口。

overall_soundscape:
茶馆环境声和对白。

non_diegetic_music:
N/A`

const bilingualRef2VEnglish = `subject_definitions:
<Subject 1> is A-Qing in <Picture 1>. <Picture 2> is the teahouse scene. <Picture 3> is the 0.00-second first frame.

summary:
A-Qing speaks in the teahouse.

retention_analysis:
<Subject 1>, the scene, and the first frame remain consistent.

detailed_description:
[Shot 1] [0.00s] <Subject 1> (S1) says <d>[Chinese] 你好，茶还热吗？</d>, then looks toward the door.

overall_soundscape:
Teahouse ambience and dialogue.

non_diegetic_music:
N/A`

func configureDefaultRef2VFixture(t *testing.T, f *translationHTTPFixture) {
	t.Helper()
	character := models.Character{ProjectID: f.p.ID, Name: "阿青", Portrait: "aqing.png"}
	location := models.Asset{ProjectID: f.p.ID, Kind: AssetKindLocation, Name: "茶馆", Image: "teahouse.png"}
	if err := f.db.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Create(&location).Error; err != nil {
		t.Fatal(err)
	}
	refs, _ := json.Marshal([]SceneReferenceSelection{
		{SourceType: "character", SourceID: character.ID, Variant: "portrait", UseH3: true},
		{SourceType: "asset", SourceID: location.ID, Variant: "image", UseH3: true},
	})
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{
		"video_template": "", "video_prompt_master_zh": bilingualRef2VMaster,
		"reference_images_json": string(refs), "image_file": "start.png",
		"characters": "阿青", "visible_characters": "阿青", "location_name": "茶馆",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.First(&f.sc, f.sc.ID).Error; err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRef2VTranslationUsesActualReferenceFileOrder(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	configureDefaultRef2VFixture(t, f)
	files, lines := f.svc.Projects.sceneVideoReferenceFiles(&f.sc, "1")
	if len(files) != 3 || files[0].Name != "aqing.png" || files[1].Name != "teahouse.png" || files[2].Name != "start.png" {
		t.Fatalf("actual ref mapping=%+v", files)
	}
	for i, want := range []string{"角色", "场景", "当前分镜画面"} {
		if !strings.Contains(lines[i], want) || !strings.Contains(lines[i], "<Picture "+string(rune('1'+i))+">") {
			t.Fatalf("line %d=%q", i, lines[i])
		}
	}

	f.response = bilingualRef2VEnglish
	w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/translate", `{"language":"en"}`)
	if w.Code != 200 {
		t.Fatalf("translate status=%d %s", w.Code, w.Body.String())
	}
	got := decodeTranslationBody(t, w)
	if got["prompt"] != bilingualRef2VEnglish {
		t.Fatalf("prompt changed bytes: %q", got["prompt"])
	}
	if err := validateH3Translation(bilingualRef2VMaster, bilingualRef2VEnglish); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultRef2VSourceChangesDoNotBlockReviewedEnglishSave(t *testing.T) {
	mutations := map[string]func(*translationHTTPFixture){
		"reference order": func(f *translationHTTPFixture) {
			var refs []SceneReferenceSelection
			_ = json.Unmarshal([]byte(f.sc.ReferenceImagesJSON), &refs)
			refs[0], refs[1] = refs[1], refs[0]
			b, _ := json.Marshal(refs)
			f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("reference_images_json", string(b))
		},
		"scene image": func(f *translationHTTPFixture) {
			f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("image_file", "new-start.png")
		},
		"character classification": func(f *translationHTTPFixture) {
			f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Update("visible_characters", "")
		},
		"dialogue": func(f *translationHTTPFixture) {
			f.db.Create(&models.Dialogue{ProjectID: f.p.ID, SceneID: f.sc.ID, Order: 1, Character: "阿青", Text: "变化"})
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			f := newTranslationHTTPFixture(t)
			configureDefaultRef2VFixture(t, f)
			oldHash, err := f.svc.sceneVideoPromptSourceHash(&f.sc)
			if err != nil {
				t.Fatal(err)
			}
			mutate(f)
			body, _ := json.Marshal(map[string]any{"exec_prompt": bilingualRef2VEnglish, "source_hash": oldHash})
			w := translationRequest(f.svc, http.MethodPost, f.p.ID, f.sc.ID, "/confirm", string(body))
			if w.Code != http.StatusOK {
				t.Fatalf("source change blocked reviewed English save status=%d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestUnconfirmedMasterFailsBeforeTaskServiceAndLegacyEnglishStillPassesGate(t *testing.T) {
	f := newTranslationHTTPFixture(t)
	configureDefaultRef2VFixture(t, f)
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &f.sc); err == nil || !strings.Contains(err.Error(), "尚未翻译并确认") {
		t.Fatalf("unconfirmed generation reached later services: %v", err)
	}
	if err := f.db.Model(&models.Scene{}).Where("id = ?", f.sc.ID).Updates(map[string]any{"video_prompt_master_zh": "", "video_full_prompt": "legacy English prompt", "prompt_stale": false}).Error; err != nil {
		t.Fatal(err)
	}
	var legacy models.Scene
	f.db.First(&legacy, f.sc.ID)
	// A later configuration error is expected; the bilingual confirmation gate must not reject legacy rows.
	if err := f.svc.Projects.GenerateSceneVideo(&f.p, &legacy); err != nil && strings.Contains(err.Error(), "尚未翻译并确认") {
		t.Fatalf("legacy row hit new gate: %v", err)
	}
}
