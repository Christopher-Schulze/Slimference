# TASK 127: REJECTED - image format optimisation (lossless re-encoding)

Status: REJECTED / NO IMPLEMENTATION (2026-05-01 reality check)
Priority: P0 to remove from active plan
Scope: no code. Keep images untouched.
Driver: the original task assumed lossless byte-size reduction (PNG -> WebP) would reduce OpenAI/Anthropic image-token cost. That assumption is wrong for the provider vision paths Slimference cares about. Image token cost is driven by pixel dimensions/detail policy, not the encoded byte size, for native image blocks. Lossless re-encoding can reduce wire bandwidth, but it does not materially reduce billed vision tokens.

Decision: do not build `internal/imagecompact/`. Do not touch images in Phase R. Most operator traffic is text/tool output, and image manipulation adds CPU/dependency/correctness risk without the claimed token saving.

If a future image task is needed, it must be a new task about adaptive downsampling/detail selection with explicit visual-risk controls. That is a different semantic trade-off and is not T127.

---

## Rejected plan (removed from implementation)

The removed plan was: sniff PNG/JPEG/GIF/WebP, re-encode to smaller WebP/lossless formats, verify pixels, and report image-token savings. It is rejected because it optimises bytes, not the billed image-token mechanism for native vision blocks.

The only future image-token lever worth considering is adaptive resolution/detail selection. That changes the pixel grid and therefore belongs in a separate, explicitly lossy/visual-risk-scoped task. Phase R leaves images untouched.

## Rejection acceptance criteria

- [x] Active Phase R list marks T127 rejected/no-op.
- [x] No `internal/imagecompact/` package is planned.
- [x] No image re-encoding dependency is introduced.
- [x] Future image-token savings, if any, are scoped as a separate adaptive downsampling/detail-policy task, not a lossless re-encode task.

## Out of scope

- Any image re-encoding in Phase R.
- Resolution downsampling or detail-policy changes under this task.
- Image OCR / image-to-text conversion.
- Image dependencies or image telemetry.

## Validation

```
rg -n "image-token shrink|Image-token saving|internal/imagecompact" docs/todo.md docs/todo/t127-image-compression.md
```

## Notes on user's brief

User: "vielleicht das bild immer in ein format konvertieren was praktisch das kleinste ist mit identischer darstellung"

Reality correction: that is useful for bandwidth, not billed image tokens on the relevant provider paths. User decision after the correction: images stay untouched.
