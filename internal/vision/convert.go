package vision

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // register GIF decoder (first frame only)
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"io"
	"os"

	_ "golang.org/x/image/bmp" // register BMP decoder
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff" // register TIFF decoder
	_ "golang.org/x/image/webp" // register WebP decoder
)

// HookJPEGQuality is the JPEG quality for the drive-by hook path. q70
// is a good compression-vs-fidelity tradeoff at the 800px hook target;
// any artifacts are dominated by the resize lossiness at that scale.
const HookJPEGQuality = 70

// DeliberateJPEGQuality is the JPEG quality for the MCP / describe
// path. q85 reduces chroma-subsampling artifacts that matter at the
// 2576px deliberate target — particularly around small text (chart
// axis labels at 8-9pt), single-pixel diagram strokes, and color
// swatches that distinguish series. Payload is ~30-40% larger than
// q70 but pro tokens scale with content complexity not bytes, so the
// per-call cost increment is sub-cent.
const DeliberateJPEGQuality = 85

// JPEGQuality is retained for callers that don't differentiate by
// path. Resolves to the hook quality (the historical default).
// Deprecated: pass quality explicitly via ConvertForVision's third
// argument; this const will become unexported in v0.2.
const JPEGQuality = HookJPEGQuality

// MaxImageBytes caps the on-disk file size bmg will load into memory.
// 50 MB is generous for any legitimate screenshot or document scan
// while keeping pathological inputs (uncompressed BMP at gigabyte
// scale; multi-page TIFF; deliberately malformed file claiming a huge
// payload) from OOM-ing the hook process.
const MaxImageBytes = 50 * 1024 * 1024

// MaxImagePixels caps the decoded resolution. 32 megapixels (e.g.
// 8000x4000) is large enough for any realistic camera or screenshot
// while bounding worst-case framebuffer at ~128 MB (W*H*4 bytes for
// RGBA). Defends against the decompression-bomb pattern (PNG header
// declares 65000x65000 pixels with no actual content) where the bytes
// pass the file-size check but Decode would allocate a giant
// framebuffer. DecodeConfig reads only the header, so the check is
// O(microseconds).
const MaxImagePixels = 32 * 1024 * 1024

// ReadImageBytesBounded opens path and reads up to MaxImageBytes. If
// the file is larger, returns an error naming the cap so the caller
// can surface a clear message. Use this in place of os.ReadFile at any
// site where path is attacker-influenceable (anywhere bmg is asked to
// describe a file Claude can name).
func ReadImageBytesBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Read up to MaxImageBytes+1 so we can distinguish "exactly at cap"
	// from "exceeds cap" via the returned length.
	data, err := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxImageBytes {
		return nil, fmt.Errorf("image exceeds cap: %d bytes > %d (%d MB); refuse to load to protect against decompression-bomb / OOM patterns",
			len(data), MaxImageBytes, MaxImageBytes/(1024*1024))
	}
	return data, nil
}

// ConvertForVision decodes raw image bytes (any format that Go's
// image package can read), resizes the longest edge to maxDim if
// larger, flattens any alpha channel against white, and re-encodes
// as JPEG at JPEGQuality.
//
// maxDim differentiates the drive-by hook path (cheap, 800px) from
// the deliberate MCP/describe path (quality-optimized, 2576px). 0 is
// treated as no resize cap — useful for tests but production callers
// should pass an explicit cap.
//
// This addresses two problems at once:
//   - The Claude Code "Could not process image" 400 error is most
//     reliably triggered by transparent PNGs and oversized images;
//     re-encoding to opaque JPEG sidesteps both.
//   - Smaller images cost fewer Gemini tokens per call; the hook path
//     uses the small cap to minimize drive-by cost, while the MCP
//     path uses the larger cap to preserve OCR / fine-detail signal.
//
// The returned bytes are always image/jpeg. JPEG quality is selected
// from MaxDim: anything > HookMaxDim gets DeliberateJPEGQuality (q85);
// HookMaxDim or smaller gets HookJPEGQuality (q70). This couples the
// quality decision to the path that's already selected by MaxDim, so
// callers don't have to thread a separate quality argument.
func ConvertForVision(raw []byte, maxDim int) ([]byte, error) {
	// Header-only pre-check: a malformed image declaring huge dimensions
	// would otherwise allocate an absurd framebuffer in image.Decode.
	// DecodeConfig reads the header only (microseconds, ~constant bytes
	// regardless of declared size).
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode header (format=%q): %w", format, err)
	}
	if int64(cfg.Width)*int64(cfg.Height) > int64(MaxImagePixels) {
		return nil, fmt.Errorf("image dimensions %dx%d exceed cap of %d pixels (decode would allocate ~%d MB framebuffer); refuse to load",
			cfg.Width, cfg.Height, MaxImagePixels, (int64(cfg.Width)*int64(cfg.Height)*4)/(1024*1024))
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode (format=%q): %w", format, err)
	}

	// Compute target size, preserving aspect ratio.
	srcBounds := src.Bounds()
	w, h := srcBounds.Dx(), srcBounds.Dy()
	scaledW, scaledH := w, h
	if maxDim > 0 && (w > maxDim || h > maxDim) {
		if w >= h {
			scaledW = maxDim
			scaledH = int(float64(h) * float64(maxDim) / float64(w))
		} else {
			scaledH = maxDim
			scaledW = int(float64(w) * float64(maxDim) / float64(h))
		}
	}

	// Always flatten to RGB on a white background. This handles alpha
	// (PNG transparency) and also normalizes color models so the JPEG
	// encoder doesn't have to negotiate.
	dst := image.NewRGBA(image.Rect(0, 0, scaledW, scaledH))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)

	if scaledW == w && scaledH == h {
		// Just copy + flatten — no resize needed.
		draw.Draw(dst, dst.Bounds(), src, srcBounds.Min, draw.Over)
	} else {
		// CatmullRom is high-quality and adequately fast for our sizes;
		// avoid ApproxBiLinear for smaller artifacts.
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, srcBounds, xdraw.Over, nil)
	}

	quality := HookJPEGQuality
	if maxDim > HookMaxDim {
		quality = DeliberateJPEGQuality
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("jpeg encode: %w", err)
	}
	return buf.Bytes(), nil
}
