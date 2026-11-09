package service

import "fmt"

// Transform coordinates are relative to the fixed export frame. Keyframe times
// are output-local seconds AFTER trim/speed, never source or episode timestamps.
type clipTransformPose struct {
	Zoom     float64 `json:"zoom"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Rotation float64 `json:"rotation"`
}
type clipTransformKeyframe struct {
	clipTransformPose
	Time float64 `json:"time"`
}
type clipTransform struct {
	clipTransformPose
	Fit        string                  `json:"fit"`
	CropLeft   float64                 `json:"crop_left"`
	CropRight  float64                 `json:"crop_right"`
	CropTop    float64                 `json:"crop_top"`
	CropBottom float64                 `json:"crop_bottom"`
	Keyframes  []clipTransformKeyframe `json:"keyframes,omitempty"`
}

func validateClipTransform(tr *clipTransform, duration float64) error {
	if tr == nil {
		return nil
	}
	if tr.Fit != "contain" && tr.Fit != "cover" {
		return fmt.Errorf("画幅适配仅支持contain或cover")
	}
	pose := func(p clipTransformPose) bool {
		return finite(p.Zoom) && p.Zoom >= .25 && p.Zoom <= 4 && finite(p.X) && p.X >= -1 && p.X <= 1 && finite(p.Y) && p.Y >= -1 && p.Y <= 1 && finite(p.Rotation) && p.Rotation >= -360 && p.Rotation <= 360
	}
	if !pose(tr.clipTransformPose) {
		return fmt.Errorf("画面变换超限：缩放0.25–4，位置-1–1，旋转-360–360")
	}
	for _, n := range []float64{tr.CropLeft, tr.CropRight, tr.CropTop, tr.CropBottom} {
		if !finite(n) || n < 0 || n > .95 {
			return fmt.Errorf("裁切比例无效")
		}
	}
	if tr.CropLeft+tr.CropRight > .95 || tr.CropTop+tr.CropBottom > .95 {
		return fmt.Errorf("裁切必须至少保留5%%画面")
	}
	if len(tr.Keyframes) > 32 {
		return fmt.Errorf("每段最多32个关键帧")
	}
	previous := -1.0
	for _, k := range tr.Keyframes {
		if !finite(k.Time) || k.Time < 0 || k.Time > duration || k.Time <= previous || (previous >= 0 && k.Time-previous < .001-1e-9) || !pose(k.clipTransformPose) {
			return fmt.Errorf("关键帧须按输出秒数递增且位于片段内，变换值须合法")
		}
		previous = k.Time
	}
	return nil
}
func transformExpression(tr *clipTransform, field func(clipTransformPose) float64) string {
	if len(tr.Keyframes) == 0 {
		return ffnum(field(tr.clipTransformPose))
	}
	keys := tr.Keyframes
	expr := ffnum(field(keys[len(keys)-1].clipTransformPose))
	for i := len(keys) - 2; i >= 0; i-- {
		a, b := keys[i], keys[i+1]
		linear := fmt.Sprintf("(%s+(%s-%s)*(t-%s)/%s)", ffnum(field(a.clipTransformPose)), ffnum(field(b.clipTransformPose)), ffnum(field(a.clipTransformPose)), ffnum(a.Time), ffnum(b.Time-a.Time))
		expr = fmt.Sprintf("if(lt(t,%s),%s,%s)", ffnum(b.Time), linear, expr)
	}
	return fmt.Sprintf("if(lt(t,%s),%s,%s)", ffnum(keys[0].Time), ffnum(field(keys[0].clipTransformPose)), expr)
}

// appendClipTransform uses a constant canvas and a transformed foreground; output
// dimensions and duration stay fixed even when foreground geometry changes.
func appendClipTransform(filters []string, input string, tr *clipTransform, w, h, index int) ([]string, string) {
	prefix := fmt.Sprintf("transform%d", index)
	filters = append(filters, fmt.Sprintf("[%s]split[%sbg][%sfg]", input, prefix, prefix))
	filters = append(filters, fmt.Sprintf("[%sbg]scale=%d:%d,setsar=1,drawbox=c=black:t=fill[%scanvas]", prefix, w, h, prefix))
	fit := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2", w, h, w, h)
	if tr.Fit == "cover" {
		fit = fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d", w, h, w, h)
	}
	zoom := transformExpression(tr, func(p clipTransformPose) float64 { return p.Zoom })
	rotation := transformExpression(tr, func(p clipTransformPose) float64 { return p.Rotation })
	x := transformExpression(tr, func(p clipTransformPose) float64 { return p.X })
	y := transformExpression(tr, func(p clipTransformPose) float64 { return p.Y })
	filters = append(filters, fmt.Sprintf("[%sfg]crop=w=iw*%s:h=ih*%s:x=iw*%s:y=ih*%s,%s,setsar=1,format=rgba,rotate=a='(%s)*PI/180':ow=iw:oh=ih:c=none,scale=w='max(2,trunc(%d*(%s)/2)*2)':h='max(2,trunc(%d*(%s)/2)*2)':eval=frame[%simage]", prefix, ffnum(1-tr.CropLeft-tr.CropRight), ffnum(1-tr.CropTop-tr.CropBottom), ffnum(tr.CropLeft), ffnum(tr.CropTop), fit, rotation, w, zoom, h, zoom, prefix))
	filters = append(filters, fmt.Sprintf("[%scanvas][%simage]overlay=x='(W-w)/2+W*(%s)':y='(H-h)/2+H*(%s)':eval=frame:shortest=1[%sout]", prefix, prefix, x, y, prefix))
	return filters, prefix + "out"
}
