# T47 - Binary-Release Pipeline (Cross-Build, Checksums, Homebrew Tap)

Status: todo
Priority: P1
Scope: `scripts/release/`, `.github/workflows/` (if GitHub), `README.md`, `Formula/slimference.rb` (Homebrew tap repo)
Driver: post-v2 production-readiness audit (2026-04-20)

---

## Problem

There are no pre-built binaries. Every user must clone the repo, have the
Go toolchain installed, run `go build`, and wire paths manually. That is a
substantial friction barrier for:

- people who want to try the tool before trusting it with their Claude
  Code workflow
- reproducibility (build reproducibly once, ship artifacts with SHAs)
- Homebrew / package-manager delivery

## Current State

- `go build ./cmd/slimference` produces a single binary.
- No release tag, no artifacts, no distribution.

## Target State

- Tagged release cuts (`v2.1.0`, ...) produce:
  - `slimference_<version>_darwin_arm64.tar.gz`
  - `slimference_<version>_darwin_amd64.tar.gz`
  - `slimference_<version>_linux_arm64.tar.gz`
  - `slimference_<version>_linux_amd64.tar.gz`
  - `SHA256SUMS` with signed detached signature (minisign or cosign).
- Each archive contains: `slimference` binary, `LICENSE`, `README.md`,
  `CHANGELOG.md`, `scripts/service/linux/slimference.service` (when Linux),
  `scripts/service/docker/Dockerfile`.
- Homebrew tap `slimference/homebrew-tap` with `slimference.rb` formula.
- GitHub Release notes auto-generated from `docs/changelog.md` Unreleased
  section.

## Design

### Build script

`scripts/release/build.ts` (Bun/TS per AGENTS.md):

```
targets = [
  { os: "darwin", arch: "arm64" },
  { os: "darwin", arch: "amd64" },
  { os: "linux",  arch: "arm64" },
  { os: "linux",  arch: "amd64" },
]
for t of targets:
  GOOS=t.os GOARCH=t.arch go build \
    -ldflags "-s -w -X main.version=$VERSION -X main.commit=$SHA" \
    -o dist/<name>/slimference
  tar cfvz dist/<name>.tar.gz dist/<name>/
```

### Version injection

Already present in `internal/buildinfo`. Ensure `-X` flags set
`buildinfo.Version` and `buildinfo.Commit` at link time.

### Reproducibility

- `-trimpath` on every build.
- Fixed `SOURCE_DATE_EPOCH` from Git commit time.
- No CGO (verify `CGO_ENABLED=0` works - `modernc.org/sqlite` is pure Go so
  it should).
- Compare two independent builds byte-equal.

### Checksums + signing

- `sha256sum dist/*.tar.gz > SHA256SUMS`
- `minisign -Sm SHA256SUMS -s release.key`
- Public key in repo at `docs/release-pubkey.minisign`.

### Homebrew formula

Minimal `slimference.rb`:

```ruby
class Slimference < Formula
  desc "Claude/Codex token-optimizing proxy"
  homepage "https://github.com/slimference/slimference"
  url "https://github.com/.../slimference_2.1.0_darwin_arm64.tar.gz"
  sha256 "..."
  version "2.1.0"

  def install
    bin.install "slimference"
  end

  test do
    system "#{bin}/slimference", "--version"
  end
end
```

Keep formula in a **separate repo** (`slimference/homebrew-tap`) so
formula updates don't churn the main repo.

### CI job (GitHub Actions)

`.github/workflows/release.yml` triggers on tag push `v*`:

- Build matrix (4 targets).
- Run `go test -race ./...` first - **release fails if tests fail**.
- Run `scripts/coverage` gate - release fails if < 100 %.
- Upload archives + SHA256SUMS to GitHub Release.
- Open PR to homebrew-tap repo with updated URL + SHA.

## Implementation Plan

### WP1 - Build script
- `scripts/release/build.ts` with cross-build matrix.

### WP2 - Archive + checksum
- Pack binary + LICENSE + README into tar.gz.
- Emit SHA256SUMS.

### WP3 - Signing key
- Generate minisign keypair, commit public key.
- Document key custody in `docs/release-process.md`.

### WP4 - CI workflow
- `.github/workflows/release.yml` on tag push.

### WP5 - Homebrew tap
- Create `slimference/homebrew-tap` repo with initial formula.
- Automation to open PR on each release.

### WP6 - Documentation
- `docs/release-process.md` with step-by-step tag → release instructions.
- README install section updated with `brew install slimference/tap/slimference`
  and `curl | tar -xz` path.

### WP7 - Smoke test on each target
- Run `./slimference --version` and `./slimference doctor` on each OS/arch
  via CI (darwin via macos-runner, linux via ubuntu-runner; arm64 via qemu
  or native runner where available).

---

## Subtasks

- [ ] `scripts/release/build.ts` cross-build.
- [ ] Reproducibility flags (`-trimpath`, fixed SOURCE_DATE_EPOCH).
- [ ] Archive + SHA256SUMS.
- [ ] Minisign keypair + public key committed.
- [ ] GitHub Actions release workflow.
- [ ] Homebrew tap repo bootstrap.
- [ ] Homebrew auto-PR on release.
- [ ] `docs/release-process.md`.
- [ ] README install section rewrite.
- [ ] Smoke test per target in CI.

## Risks

- CGO creeping in via future dep breaks cross-build. Guard: set
  `CGO_ENABLED=0` in CI, fail build if it silently enables.
- Private signing key leak. Mitigation: key lives in a sealed GitHub
  environment, only release workflow can access.
- Homebrew formula drift. Automate PR creation to reduce manual error.

## Acceptance Criteria

- [ ] Tagging `v2.1.0` triggers CI producing 4 archives + SHA256SUMS + sig.
- [ ] `brew install slimference/tap/slimference` works on macOS.
- [ ] `curl -L ... | tar -xz` and run works on Linux.
- [ ] Binary reports correct `--version` and `--commit` via
      `internal/buildinfo`.
- [ ] Two independent builds produce byte-equal archives
      (reproducibility).

## Out of Scope

- Windows builds (macOS-only scope per AGENTS.md; Linux added because
  cross-build is free).
- Package managers beyond Homebrew (no apt/rpm/aur yet).

---

## Validation

```
bun run scripts/release/build.ts --version 2.1.0
sha256sum -c dist/SHA256SUMS
tar -tzf dist/slimference_2.1.0_linux_amd64.tar.gz
./dist/slimference_2.1.0_darwin_arm64/slimference --version
```
