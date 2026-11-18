package service

import "fmt"

type previsRoute struct {
	ID       string       `json:"id"`
	ObjectID string       `json:"object_id"`
	A        previsVector `json:"a"`
	B        previsVector `json:"b"`
}
type previsKeyframe struct {
	ObjectID string       `json:"object_id"`
	TimeMS   int          `json:"time_ms"`
	Position previsVector `json:"position"`
	Yaw      float64      `json:"yaw"`
}
type previsCameraKeyframe struct {
	TimeMS int          `json:"time_ms"`
	Camera previsCamera `json:"camera"`
	Cut    bool         `json:"cut"`
}

func validatePrevisTimeline(doc previsDocument) error {
	if doc.SchemaVersion == 1 {
		if doc.DurationMS != 0 || len(doc.Routes) > 0 || len(doc.Keyframes) > 0 || len(doc.CameraKeyframes) > 0 {
			return fmt.Errorf("旧版本不支持时间轴")
		}
		return nil
	}
	if doc.DurationMS < 1000 || doc.DurationMS > 120000 || len(doc.Routes) > 32 || len(doc.Keyframes) > 256 || len(doc.CameraKeyframes) > 128 {
		return fmt.Errorf("时间轴时长或条目数超限")
	}
	ids := map[string]bool{}
	for _, o := range doc.Objects {
		ids[o.ID] = true
	}
	routes := map[string]bool{}
	for _, r := range doc.Routes {
		if r.ID == "" || len(r.ID) > 80 || routes[r.ID] || !ids[r.ObjectID] || !validPrevisVector(r.A) || !validPrevisVector(r.B) {
			return fmt.Errorf("路线身份或坐标无效")
		}
		routes[r.ID] = true
	}
	frames := map[string]bool{}
	for _, k := range doc.Keyframes {
		id := fmt.Sprintf("%s/%d", k.ObjectID, k.TimeMS)
		if !ids[k.ObjectID] || k.TimeMS < 0 || k.TimeMS > doc.DurationMS || !validPrevisVector(k.Position) || !previsFinite(k.Yaw, -360, 360) || frames[id] {
			return fmt.Errorf("对象关键帧重复或无效")
		}
		frames[id] = true
	}
	cameras := map[int]bool{}
	for _, k := range doc.CameraKeyframes {
		if k.TimeMS < 0 || k.TimeMS > doc.DurationMS || !validatePrevisCamera(k.Camera) || cameras[k.TimeMS] {
			return fmt.Errorf("相机关键帧重复或无效")
		}
		cameras[k.TimeMS] = true
	}
	return nil
}
