package vision

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makePNG produces a w x h opaque PNG suitable for piping into Convert
// or ReadImageBytesBounded.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode synthetic PNG: %v", err)
	}
	return buf.Bytes()
}

func TestReadImageBytesBounded_UnderCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.png")
	if err := os.WriteFile(path, makePNG(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadImageBytesBounded(path)
	if err != nil {
		t.Fatalf("ReadImageBytesBounded: %v", err)
	}
	if len(got) == 0 {
		t.Error("returned zero bytes for a 4x4 PNG")
	}
}

func TestReadImageBytesBounded_OverCap(t *testing.T) {
	// Build a file larger than MaxImageBytes by writing zeros — content
	// validity doesn't matter, only size.
	dir := t.TempDir()
	path := filepath.Join(dir, "bomb.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Seek + write 1 byte at MaxImageBytes+10 creates a sparse file of
	// the right size on the local filesystem.
	if _, err := f.Seek(int64(MaxImageBytes+10), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, err = ReadImageBytesBounded(path)
	if err == nil {
		t.Fatal("expected error for file over MaxImageBytes; got nil")
	}
	if !strings.Contains(err.Error(), "exceeds cap") {
		t.Errorf("error %q should mention 'exceeds cap'", err.Error())
	}
}

func TestReadImageBytesBounded_NotFound(t *testing.T) {
	_, err := ReadImageBytesBounded(filepath.Join(t.TempDir(), "nonexistent.png"))
	if err == nil {
		t.Fatal("expected error for missing file; got nil")
	}
}

func TestConvertForVision_RejectsBombDimensions(t *testing.T) {
	// Forge a PNG header claiming 65535 x 65535. We can't actually
	// allocate that, but image.DecodeConfig will read the header and
	// our pre-check should reject before image.Decode runs.
	//
	// The simplest forgery: a real small PNG with the IHDR width/height
	// bytes patched. PNG signature is 8 bytes, then IHDR chunk: 4 bytes
	// length + "IHDR" (4 bytes) + 4 bytes width + 4 bytes height + ...
	// So width starts at offset 16, height at offset 20.
	raw := makePNG(t, 4, 4)
	if len(raw) < 33 {
		t.Fatalf("synthetic PNG unexpectedly short: %d bytes", len(raw))
	}
	// PNG layout: 8-byte signature, then IHDR chunk = 4-byte length +
	// 4-byte "IHDR" + 13 bytes of data + 4-byte CRC32. Width is at
	// offset 16, height at 20, CRC at 29.
	binary.BigEndian.PutUint32(raw[16:20], 65535)
	binary.BigEndian.PutUint32(raw[20:24], 65535)
	// Recompute CRC over the IHDR chunk type + data (17 bytes starting
	// at offset 12). Go's png decoder validates this; without the
	// recomputation our pre-check never gets to run.
	crc := crc32.ChecksumIEEE(raw[12:29])
	binary.BigEndian.PutUint32(raw[29:33], crc)
	_, err := ConvertForVision(raw, 800)
	if err == nil {
		t.Fatal("expected ConvertForVision to reject 65535x65535-declared PNG; got nil")
	}
	if !strings.Contains(err.Error(), "exceed cap") && !strings.Contains(err.Error(), "exceed") {
		t.Errorf("error %q should mention dimension cap; got %q", "exceed", err.Error())
	}
}

func TestConvertForVision_HappyPath(t *testing.T) {
	raw := makePNG(t, 100, 100)
	out, err := ConvertForVision(raw, 800)
	if err != nil {
		t.Fatalf("ConvertForVision: %v", err)
	}
	if len(out) == 0 {
		t.Error("ConvertForVision returned empty bytes for valid PNG")
	}
	// Output should be JPEG — first 2 bytes should be FF D8.
	if len(out) < 2 || out[0] != 0xFF || out[1] != 0xD8 {
		t.Errorf("output is not JPEG-shaped; first bytes %x %x", out[0], out[1])
	}
}

func TestConvertForVision_DeliberateUsesHigherQuality(t *testing.T) {
	// Make a 100x100 image where the JPEG encoder will measurably
	// differ between q70 and q85. A noise pattern gives more entropy
	// than a flat color, so the quality factor matters.
	const N = 100
	img := image.NewRGBA(image.Rect(0, 0, N, N))
	for y := 0; y < N; y++ {
		for x := 0; x < N; x++ {
			img.Set(x, y, image.NewUniform(image.Black).At(0, 0))
			if (x*y)%7 == 0 {
				img.Set(x, y, image.NewUniform(image.White).At(0, 0))
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()

	// Hook path (MaxDim == HookMaxDim): q70, smaller output.
	hookOut, err := ConvertForVision(raw, HookMaxDim)
	if err != nil {
		t.Fatalf("hook path: %v", err)
	}
	// Deliberate path (MaxDim > HookMaxDim): q85, larger output.
	delibOut, err := ConvertForVision(raw, DeliberateMaxDim)
	if err != nil {
		t.Fatalf("deliberate path: %v", err)
	}
	if len(delibOut) <= len(hookOut) {
		t.Errorf("expected deliberate-path output to be larger (q85 vs q70); got deliberate=%d hook=%d",
			len(delibOut), len(hookOut))
	}
}
