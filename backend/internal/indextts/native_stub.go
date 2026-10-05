//go:build !windows || !cgo || !indextts

package indextts

import "fmt"

func LoadCUDA(modelDir string, deviceIndex int) (Model, error) {
	return nil, fmt.Errorf("本地 IndexTTS CUDA 后端不可用；请在 Windows 上启用 cgo 并使用 -tags indextts 构建")
}
