package service

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"comfyui-console/internal/models"
)

// These three templates back the 2D canvas buttons (inpaint, background
// removal) and the 3D scene's "generate mesh". They only use nodes present
// in the ComfyUI tree vendored in this repository, so a missing custom-node
// pack cannot make them fail at render time.
func TestNewImageWorkflowsAreEmbedded(t *testing.T) {
	files, _, err := loadSystemTemplateFiles("")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"image_inpaint.json",
		"image_remove_background.json",
		"image_to_3d_hunyuan3d.json",
		"image_inpaint_krea2.json",
		"image_segment_sam3.json",
		"image_segment_sam3_matte.json",
		"image_to_3d_triposplat.json",
	} {
		if _, ok := files[name]; !ok {
			t.Fatalf("%s is not embedded in the binary", name)
		}
	}
}

func TestNewImageWorkflowsRender(t *testing.T) {
	fileParam := func(name string) map[string]any {
		return map[string]any{"task_id": "task-1", "name": name}
	}

	cases := []struct {
		file   string
		code   string
		params map[string]any
	}{
		{
			file: "image_inpaint.json",
			code: "image_inpaint",
			params: map[string]any{
				"image":    fileParam("source.png"),
				"mask":     fileParam("mask.png"),
				"prompt":   "a red hat",
				"_task_id": "task-1",
			},
		},
		{
			file: "image_remove_background.json",
			code: "image_remove_background",
			params: map[string]any{
				"image":    fileParam("photo.jpg"),
				"_task_id": "task-1",
			},
		},
		{
			file: "image_to_3d_hunyuan3d.json",
			code: "image_to_3d_hunyuan3d",
			params: map[string]any{
				"image":    fileParam("character.png"),
				"_task_id": "task-1",
			},
		},
		{
			file: "image_inpaint_krea2.json",
			code: "image_inpaint_krea2",
			params: map[string]any{
				"image":    fileParam("source.png"),
				"mask":     fileParam("mask.png"),
				"prompt":   "a red hat",
				"_task_id": "task-1",
			},
		},
		{
			file: "image_segment_sam3.json",
			code: "image_segment_sam3",
			params: map[string]any{
				"image":    fileParam("photo.png"),
				"prompt":   "person",
				"_task_id": "task-1",
			},
		},
		{
			file: "image_segment_sam3_matte.json",
			code: "image_segment_sam3_matte",
			params: map[string]any{
				"image":    fileParam("photo.png"),
				"prompt":   "person",
				"_task_id": "task-1",
			},
		},
		{
			file: "image_to_3d_triposplat.json",
			code: "image_to_3d_triposplat",
			params: map[string]any{
				"image":    fileParam("character.png"),
				"_task_id": "task-1",
			},
		},
	}

	placeholderRe := regexp.MustCompile(`\{\{([^}]+)\}\}`)

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			data, err := os.ReadFile("templates/" + tc.file)
			if err != nil {
				t.Fatal(err)
			}
			var tf templateFile
			if err := json.Unmarshal(data, &tf); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if tf.Code != tc.code {
				t.Fatalf("code=%q want %q", tf.Code, tc.code)
			}
			if len(tf.Inputs) == 0 || len(tf.Workflow) == 0 {
				t.Fatal("template has no inputs or no workflow")
			}

			declared := map[string]string{}
			for _, input := range tf.Inputs {
				key, _ := input["key"].(string)
				typ, _ := input["type"].(string)
				if key == "" || typ == "" {
					t.Fatalf("input without key/type: %v", input)
				}
				declared[key] = typ
			}

			// Every placeholder must be either declared, task_id, or an
			// image placeholder whose key is declared as an image input.
			for nodeID, node := range tf.Workflow {
				inputs, ok := node["inputs"].(map[string]any)
				if !ok {
					continue
				}
				for inputKey, value := range inputs {
					text, ok := value.(string)
					if !ok {
						continue
					}
					for _, match := range placeholderRe.FindAllStringSubmatch(text, -1) {
						inner := match[1]
						if inner == "task_id" {
							continue
						}
						if strings.HasPrefix(inner, "img:") {
							key := strings.TrimPrefix(inner, "img:")
							if declared[key] != "image" {
								t.Errorf("node %s input %s references undeclared image %q", nodeID, inputKey, key)
							}
							continue
						}
						if _, ok := declared[inner]; !ok {
							t.Errorf("node %s input %s references undeclared param %q", nodeID, inputKey, inner)
						}
					}
				}
			}

			inputsJSON, _ := json.Marshal(tf.Inputs)
			workflowJSON, _ := json.Marshal(tf.Workflow)
			tpl := &models.Template{
				Code:         tf.Code,
				InputsJSON:   string(inputsJSON),
				WorkflowJSON: string(workflowJSON),
			}
			rendered, err := (&TaskService{}).RenderWorkflow(tpl, tc.params)
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			encoded, err := json.Marshal(rendered)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "{{") {
				t.Fatalf("unrendered placeholder: %s", encoded)
			}
		})
	}
}
