package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadBoundedUploadRejectsOversize(t *testing.T) {
	_, err := readBoundedUpload(bytes.NewReader(make([]byte, 9)), 8)
	if err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("expected oversize error, got %v", err)
	}
	data, err := readBoundedUpload(bytes.NewReader(make([]byte, 8)), 8)
	if err != nil || len(data) != 8 {
		t.Fatalf("exact limit should pass: len=%d err=%v", len(data), err)
	}
}

func TestValidateUploadContentRejectsSpoofedExtensionAndType(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...)
	if err := validateUploadContent("image", "frame.png", png); err != nil {
		t.Fatalf("valid PNG signature rejected: %v", err)
	}
	for _, tc := range []struct {
		kind, name string
		data       []byte
	}{
		{"image", "frame.png", []byte("plain text")},
		{"image", "frame.jpg", png},
		{"audio", "voice.mp3", png},
		{"video", "clip.mp4", []byte("plain text")},
		{"document", "notes.pdf", []byte("plain text")},
	} {
		if err := validateUploadContent(tc.kind, tc.name, tc.data); err == nil {
			t.Errorf("accepted spoofed %s upload %s", tc.kind, tc.name)
		}
	}
}
