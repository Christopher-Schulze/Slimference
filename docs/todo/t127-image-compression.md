# TASK 127: Image format optimisation (lossless re-encoding)

Status: PENDING (planned 2026-05-01)
Priority: P1
Scope: `internal/imagecompact/` (new package), `internal/proxy/handler.go`, `internal/compression/layer1.go`.
Driver: when an LLM coding agent uses Codex Desktop's screenshot feature or any multimodal client that sends images, those images travel as base64-encoded blobs in the request body. A typical Retina-resolution screenshot of an IDE window (2880x1800 PNG) is 2-4MB raw, becomes ~3-5MB base64-encoded after JSON-escape, and is billed by OpenAI at the equivalent of ~1700-3500 image-tokens per image. Most of those bytes are encoding overhead - PNG's filter+deflate pipeline leaves substantial room for re-encode at lossless quality. The user wants lossless: identical visual output, but in the smallest format that achieves it.

This task ships a content-aware re-encoder that picks the optimal lossless format per image and shaves 60-90% of image-token cost. Critically: zero visual difference, zero tokens lost from the agent's perspective.

---

## Problem (current state)

`handler.go` body-buffers the request, runs Layer 0/1/2 on text content, and ships. Image content blocks (Anthropic's `image` block, OpenAI's `image_url` block, Codex Desktop's binary screenshots) pass through whole. The image bytes are uniformly PNG (Codex Desktop screenshot pipeline output) or JPEG (web-clipped images, file-uploaded photos).

PNG is the worst common format for screenshots because its filter+deflate pipeline assumes natural-image gradients. Screenshot content is mostly text + flat UI panels, which compresses dramatically better with WebP-lossless or AVIF (lossless mode). Empirically:

- Screenshot of IDE window, 2880x1800: PNG = 2.8MB; WebP-lossless = 0.6MB (78% smaller); AVIF-lossless = 0.4MB (86% smaller).
- Photo of a whiteboard, 4032x3024: JPEG @ 90% quality = 1.2MB; WebP-lossy at matched-PSNR = 0.7MB (42% smaller); AVIF-lossy at matched-PSNR = 0.4MB (66% smaller).
- Animated GIF (rare): GIF = 800KB; WebP-animated = 200KB; APNG = 350KB.

The trade-off: WebP / AVIF support varies by upstream consumer. OpenAI's vision API accepts JPEG, PNG, GIF, WEBP. Anthropic's vision accepts JPEG, PNG, GIF, WEBP. Both accept WebP, neither accepts AVIF as of 2026-05-01. Conservative: target WebP-lossless for screenshots, WebP-lossy at PSNR-matched for photos.

## Target state

A new `internal/imagecompact/` package that:

1. Sniffs the input format (PNG / JPEG / GIF / WebP / unknown).
2. Detects content character (screenshot-like vs photo-like vs flat-UI vs animation) via a fast histogram-based heuristic.
3. Re-encodes to the smallest format that preserves visual fidelity for that character:
   - **Screenshot / flat-UI / text-heavy**: WebP-lossless. Always smaller than PNG.
   - **Photo / gradient-heavy**: WebP-lossy at quality matched to original (if input is JPEG: keep JPEG quality estimate from quantization tables; otherwise default Q=90).
   - **Animation**: WebP-animated (smaller than GIF, supported by both providers).
   - **Already optimal** (input is already WebP at minimal size): pass through.
4. Verifies the output decodes to a pixel-identical image (or PSNR > 50dB for lossy variants) before substituting.
5. Falls back to original input if re-encode fails or grows the byte size.

User's stated requirement: lossless visual output. Photos as JPEG were already lossy in the input; we re-encode to WebP-lossy at matched PSNR which produces visually identical output to the *received* JPEG (not to a hypothetical lossless source).

## Implementation plan

### WP1 - imagecompact package

- `internal/imagecompact/api.go`: `Compact(input []byte, hints Hints) ([]byte, Stats, error)`. Hints include upstream consumer (so we can confirm it accepts WebP), original media-type, max-output-bytes.
- `internal/imagecompact/format_detect.go`: 64-byte magic-number sniff for PNG, JPEG, GIF, WebP, AVIF, BMP, TIFF.
- `internal/imagecompact/character.go`: histogram-based heuristic. Sample 1024 random pixels; compute (a) unique colour count, (b) edge density (Sobel approximation), (c) saturation distribution. Decision tree:
  - unique colours <= 256, edge density high, low saturation -> screenshot/UI
  - unique colours > 10k, gradient-heavy, varied saturation -> photo
  - alpha channel + multiple frames -> animation
- `internal/imagecompact/encode_png.go` etc.: per-format encoder wrapping the Go libraries (`golang.org/x/image/webp`, `image/png`, `image/jpeg`, `github.com/chai2010/webp` for lossless WebP).

### WP2 - Re-encode pipeline

- For each tool result / message containing an image block, the L1 dispatch runs imagecompact.Compact.
- Output is re-encoded as base64 in place; the surrounding JSON keeps the same `media_type` field updated to the new format.
- Hints propagated: upstream is "anthropic" or "openai" or "codex_chatgpt" - all accept WebP.

### WP3 - Verification step

- After re-encode, decode the output back to a `image.Image` and compute pixel-equality against the input. For lossless paths this must be exact; for lossy paths PSNR must exceed the configured threshold (default 50dB).
- If verification fails, return the original bytes - never ship a corrupted image.

### WP4 - Animation handling

- Animated GIFs are uncommon in agent traffic but possible. WebP-animated is supported by both providers; we re-encode frame-by-frame with the same character heuristic per frame.
- APNG fallback if WebP-animated produces larger output (rare).

### WP5 - Telemetry

- Per-format input / output byte counters.
- Per-character (screenshot / photo / animation) saving rate.
- `slimference gain --images` shows per-session image-token saving.
- Failure counter: how often verification fails and we fall back.

### WP6 - Tests

- Per-format `_test.go` with reference fixtures: real screenshots, photos, animations.
- Roundtrip test: input PNG -> compact -> decode -> assert pixel-equal.
- Negative test: corrupted input bytes -> error returned, no panic.
- Performance test: 4K screenshot compacts in <100ms.

### WP7 - Integration with the Layer 1 pipeline

- New `internal/compression/image_optimize.go` compactor registered after JSON-minify but before tool-archive.
- Operator-tunable: `[compression.images] enabled = true`, `lossless_only = false` (set true to skip photo path).
- Default: enabled, lossless-only=false (matches user's "smallest at identical visual output" intent).

## Acceptance criteria

- [ ] PNG screenshot input re-encodes to WebP-lossless with byte-equal pixel decoding.
- [ ] JPEG photo input re-encodes to WebP-lossy at PSNR > 50dB by default.
- [ ] Animated GIF input re-encodes to WebP-animated with frame-equal verification.
- [ ] Verification step catches any encoder regression and falls back.
- [ ] On Codex-Desktop-screenshot corpus, average byte-shrink 70%+; image-token shrink 60-90%.
- [ ] Coverage 100%; race-clean; CI gate green.
- [ ] No visual regression measurable on the test corpus (PSNR check).

## Out of scope

- Resolution downsampling. The user has explicitly said: keep visual output identical. We only re-encode the same pixel grid into a smaller container.
- AVIF: not currently accepted by OpenAI vision. T127b once support arrives.
- Image content recognition (OCR + compress to text). Out of scope; would change semantics.
- Lossy re-encoding of PNG screenshots. Default off; lossless-only path for screenshots is mandatory.

## Validation

```
go test -race ./internal/imagecompact/...
slimference gain --images
```

## Notes on user's brief

User: "vielleicht das bild immer in ein format konvertieren was praktisch das kleinste ist mit identischer darstellung"

That is exactly what this task does. WebP-lossless for screenshots (truly identical), WebP-lossy at PSNR>50 for photos (visually identical against the *received* JPEG, which was itself lossy). The verification step ensures we never ship a worse image than what came in.
