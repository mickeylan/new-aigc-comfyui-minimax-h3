package service

import (
	"os"
	"path/filepath"
	"testing"

	"comfyui-console/internal/models"
)

// 同一份文件被存了两次（素材库里常见的重复行）：删掉其中一行时，
// 文件还有人引用就不能删，否则另一行的缩略图/模型当场变空白。
//
// 注意：真删文件走 remote.Run("rm -f ...")，Windows 上没有 rm 可执行文件，
// 所以这里只断言"删掉共享行之后文件仍在"，不断言"删掉最后一行后文件消失"。
func TestDeleteMaterialKeepsFileStillReferencedByDuplicateRow(t *testing.T) {
	svc, _, _ := proxyTestService(t)
	dir := svc.Upload.InputDir()
	if err := os.MkdirAll(filepath.Join(dir, "materials"), 0o755); err != nil {
		t.Fatal(err)
	}
	modelRel := "materials/dup.spz"
	thumbRel := "materials/dup_thumb.png"
	for _, rel := range []string{modelRel, thumbRel} {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	first := models.Material{Name: "dup.spz", Type: "3d", Source: "studio_3d", Path: modelRel, ThumbPath: thumbRel, Size: 1}
	second := models.Material{Name: "dup.spz", Type: "3d", Source: "studio_3d", Path: modelRel, ThumbPath: thumbRel, Size: 1}
	if err := svc.DB.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.DB.Create(&second).Error; err != nil {
		t.Fatal(err)
	}

	if err := svc.Materials.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	// 还有第二行引用，文件必须留着
	for _, rel := range []string{modelRel, thumbRel} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Fatalf("文件 %s 被别的素材行引用，不该被删：%v", rel, err)
		}
	}
	var left models.Material
	if err := svc.DB.First(&left, second.ID).Error; err != nil {
		t.Fatalf("第二行不该被一起删掉：%v", err)
	}
	// 第一行记录确实没了
	if err := svc.DB.First(&models.Material{}, first.ID).Error; err == nil {
		t.Fatal("第一行记录应已删除")
	}
}

// 引用计数只看自己这一行之外的行，并且把 path / thumb_path 都算上。
func TestOtherMaterialUsesPathCountsBothModelAndThumb(t *testing.T) {
	svc, _, _ := proxyTestService(t)
	sameThumb := models.Material{Name: "a.png", Type: "3d", Path: "materials/a.glb", ThumbPath: "materials/shared_thumb.png"}
	other := models.Material{Name: "b.png", Type: "3d", Path: "materials/b.glb", ThumbPath: "materials/b_thumb.png"}
	for _, mat := range []*models.Material{&sameThumb, &other} {
		if err := svc.DB.Create(mat).Error; err != nil {
			t.Fatal(err)
		}
	}

	// 自己的模型路径没有别人用
	used, err := svc.Materials.otherMaterialUsesPath("materials/a.glb", sameThumb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if used {
		t.Fatal("a.glb 只有自己引用，不该算共享")
	}

	// 另一个素材的缩略图被别人用着（这里用 a 的 id 去查 b 的缩略图）
	used, err = svc.Materials.otherMaterialUsesPath("materials/b_thumb.png", sameThumb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !used {
		t.Fatal("b_thumb.png 被 b 引用，对 a 来说就是共享文件")
	}

	// 完全没人用的路径
	used, err = svc.Materials.otherMaterialUsesPath("materials/none.png", sameThumb.ID)
	if err != nil {
		t.Fatal(err)
	}
	if used {
		t.Fatal("没人引用的路径不该算共享")
	}
}