package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFontRegistryRequiresApprovedLicenseFilesAndHash(t *testing.T) {
	root := t.TempDir()
	fonts := filepath.Join(root, "fonts")
	if err := os.MkdirAll(filepath.Join(fonts, "licenses"), 0o755); err != nil {
		t.Fatal(err)
	}
	fontPath := filepath.Join(fonts, "NotoSansSC.ttf")
	if err := os.WriteFile(fontPath, []byte("font fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fonts, "licenses", "OFL.txt"), []byte("SIL Open Font License 1.1"), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := fileSHA256(fontPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := []FontRegistryEntry{{
		Code: "noto-sans-sc", DisplayName: "Noto Sans SC", Family: "Noto Sans SC", File: "NotoSansSC.ttf",
		SHA256: hash, LicenseID: "OFL-1.1", LicenseFile: filepath.Join("licenses", "OFL.txt"),
		CommercialUse: true, Redistribution: true, Embedding: true, SupportsChinese: true, Enabled: true,
	}}
	data, _ := json.Marshal(entries)
	if err := os.WriteFile(filepath.Join(fonts, fontManifestFile), data, 0o644); err != nil {
		t.Fatal(err)
	}
	registry := NewFontRegistry(root)
	listed, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || !listed[0].Verified || listed[0].VerificationError != "" {
		t.Fatalf("font not verified: %+v", listed)
	}
	if _, err := registry.Approved("noto-sans-sc"); err != nil {
		t.Fatal(err)
	}

	entries[0].SHA256 = "deadbeef"
	data, _ = json.Marshal(entries)
	_ = os.WriteFile(filepath.Join(fonts, fontManifestFile), data, 0o644)
	if _, err := registry.Approved("noto-sans-sc"); err == nil {
		t.Fatal("mismatched hash was approved")
	}
}

func TestFontRegistryRejectsUnknownLicenseAndTraversal(t *testing.T) {
	root := t.TempDir()
	fonts := filepath.Join(root, "fonts")
	if err := os.MkdirAll(fonts, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := []FontRegistryEntry{{Code: "unsafe", File: `..\outside.ttf`, LicenseID: "unknown", CommercialUse: true, Redistribution: true, Embedding: true, SupportsChinese: true, Enabled: true}}
	data, _ := json.Marshal(entries)
	_ = os.WriteFile(filepath.Join(fonts, fontManifestFile), data, 0o644)
	registry := NewFontRegistry(root)
	listed, err := registry.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Verified || listed[0].VerificationError == "" {
		t.Fatalf("unsafe font accepted: %+v", listed)
	}
}
