# Slimference Release Process

Reference for cutting a production release. All commands are run from
the repo root.

## 1. Bump the version

Update the default in `internal/buildinfo/buildinfo.go` if it changed
since the last tag. Keep the file authoritative; the release script
injects the same version into the binary via ldflags.

## 2. Update the changelog

Move the Unreleased section in `docs/changelog.md` under a dated
version heading and start a fresh Unreleased block at the top.

## 3. Run the full verification sweep

```bash
go vet ./...
go test ./...
go test -race ./...
bun run scripts/ci/main.go      # coverage gate
```

All three must be green.

## 4. Tag

```bash
git commit -am "Release v<version>"
git tag -s v<version> -m "slimference v<version>"
git push origin main v<version>
```

(Omit `-s` if you do not sign tags; note that unsigned tags cannot
participate in Homebrew signing later.)

## 5. Build the artefacts

Default build ships only the primary target (macOS on Apple M-series).
Other platforms stay supported via `--targets`:

```bash
# Default: darwin/arm64 only.
go run ./scripts/release --version v<version>

# Every supported platform (darwin + linux, arm64 + amd64):
go run ./scripts/release --version v<version> --targets=all

# Hand-picked subset:
go run ./scripts/release --version v<version> --targets=darwin/arm64,linux/amd64
```

The script:

1. Cleans `dist/` and re-creates it.
2. Resolves `--targets` (selector: `primary`, `all`, or
   comma-separated `os/arch` list). Default selector = `primary`.
3. Builds each selected target with `-trimpath` and
   `-ldflags "-s -w -X buildinfo.Version=... -X main.commit=..."`.
4. Copies `LICENSE`, `README.md`, `docs/layer0-exit-codes.md` into
   each bundle directory.
5. Copies `scripts/service/linux/slimference.service` into the Linux
   bundles (when selected).
6. Packs each bundle into `dist/slimference_<version>_<os>_<arch>.tar.gz`.
7. Writes `dist/SHA256SUMS`.

Smoke-test one archive:

```bash
tar -tzf dist/slimference_<version>_darwin_arm64.tar.gz
tar -xzOf dist/slimference_<version>_darwin_arm64.tar.gz slimference_<version>_darwin_arm64/slimference \
  | file -   # should identify as Mach-O 64-bit executable arm64
```

## 6. Sign the checksum file (optional but recommended)

```bash
minisign -Sm dist/SHA256SUMS -s release.key
```

Commit the resulting `dist/SHA256SUMS.minisig` as a release asset.

## 7. Publish

Upload every `.tar.gz` plus `SHA256SUMS` (and `.minisig`) to the
release page on the hosting platform of choice. Include the Unreleased
section from `docs/changelog.md` as the release body.

## 8. Update the Homebrew tap

Compute the macOS archive SHAs:

```bash
grep darwin dist/SHA256SUMS
```

Open a PR on the `slimference/homebrew-tap` repo updating the URL and
`sha256` fields of `Formula/slimference.rb`:

```ruby
url "https://github.com/slimference/slimference/releases/download/v<ver>/slimference_<ver>_darwin_arm64.tar.gz"
sha256 "<arm64 sha>"
version "<ver>"
```

## 9. Verify

On a clean machine:

```bash
brew update
brew install slimference/tap/slimference
slimference --version        # expect the new version
slimference doctor           # expect all checks green
```

## 10. Close the loop

Tick any release-gate items in `docs/todo.md` and open follow-up
tasks for anything that was deferred.
