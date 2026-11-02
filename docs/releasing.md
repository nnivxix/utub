# Releasing utub — version and release runbook

Applies to the current setup: TUI-only `cmd/utub`, tag-driven GitHub Actions release,
no Homebrew/Scoop/winget publishers.

Last updated: 2026-11-02 — current tag `v0.1.0`, HEAD `ecd1ee1`.

---

## 1. TL;DR

```bash
# 1. verify before tagging
go vet ./...
go test ./...
make tui && ./bin/utub --version
./bin/utub --doctor

# 2. commit and push
git add <files>
git commit -m "fix: ..."        # conventional message, see §4
git push origin main

# 3. tag and publish
git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1

# 4. watch the release workflow
gh run list --workflow=release.yml --limit 1
gh run watch <run-id>

# 5. verify the artifact and the module
gh release view v0.1.1 -R nnivxix/utub
d=$(mktemp -d); GOBIN=$d go install github.com/nnivxix/utub/cmd/utub@v0.1.1; $d/utub --version
```

There is **no version file to edit**. The git tag is the single source of truth.

---

## 2. How the version flows

| Path | Source of version |
|---|---|
| Release binary | workflow runs goreleaser with `-X main.version={{ .Version }}` (tag without `v`) |
| `make tui` / `make run` / `make doctor` | `VERSION ?= $(shell git describe --tags --always --dirty)` |
| `go install ...@vX.Y.Z` | module version recorded in buildinfo, read by `resolveVersion` |
| Local `go build` | `dev-<short-sha>[-dirty]` from VCS metadata |
| `go run` | `dev` (Go does not embed VCS info for `go run`) |

Implementation: `cmd/utub/version.go`, `cmd/utub/main.go` (`var version`), `Makefile`.
The TUI header, `--version` and `--doctor` all read the same variable.

Consequence: **publishing = pushing a tag.** Nothing else changes the version users see.

---

## 3. Choosing the next version (0.x semver)

| Change | Bump | Example |
|---|---|---|
| Bug fixes only | patch | `v0.1.1` |
| New feature, compatible | minor | `v0.2.0` |
| Breaking CLI flag, config key, or TUI behaviour | minor (pre-1.0) | `v0.2.0` |
| Docs / CI / refactor only | **no release** | keep `@latest` on the last tag |
| Test build | prerelease | `v0.2.0-rc.1` |

Notes:

- `go install ...@latest` ignores prereleases while a normal release exists, so `-rc`
  tags are safe to push and test.
- Docs-only changes don't need a tag. `go install` builds from the last tagged snapshot,
  so a README fix on `main` won't reach module users until the next tag — usually fine.
- Do not bump to `v1.0.0` until the web and desktop milestones land and the CLI surface is
  considered stable.

---

## 4. Pre-flight checklist

- [ ] Working tree clean (`git status`), on `main`, up to date with origin.
- [ ] `go vet ./...` passes.
- [ ] `go test ./...` passes.
- [ ] `make tui` builds and `./bin/utub --version` runs.
- [ ] `./bin/utub --doctor` finds `yt-dlp` and `ffmpeg`.
- [ ] Manual smoke test on the screen you changed (home → probe → formats → download).
- [ ] Commit messages are conventional (`feat:`, `fix:`, `perf:`, `docs:`, `chore:`).
      Release notes are generated from commit subjects.
- [ ] Tag is annotated and points at a commit on `main`.
- [ ] Target version doesn't already exist (`git tag -l`, `gh release list`).

There is no PR CI workflow. Local checks are the gate.

### Changelog caveat (current config)

`.goreleaser.yaml` excludes `^docs:`, `^chore:`, `^test:`, but **not** scoped forms such as
`chore(release): …` or `docs(readme): …`, and not free-form messages like
`Update README.md`. Those show up in the release notes (as seen in `v0.1.0`).

Optional fix when you touch the config:

```yaml
changelog:
  sort: asc
  filters:
    exclude:
      - "^docs(\\(.+\\))?:"
      - "^chore(\\(.+\\))?:"
      - "^test(\\(.+\\))?:"
      - "^ci(\\(.+\\))?:"
      - "^Merge "
```

---

## 5. What the release workflow does

`.github/workflows/release.yml`:

- Trigger: push of any tag matching `v*`.
- Runner: `macos-latest` (required — `universal_binaries` shells out to `lipo`).
- Steps: checkout (`fetch-depth: 0`), `setup-go` from `go.mod`, `goreleaser release --clean`.
- `permissions: contents: write` with the default `GITHUB_TOKEN`; no repo secrets required.

`.goreleaser.yaml` produces:

- 6 build targets: darwin amd64+arm64 (combined into one universal binary),
  linux amd64/arm64, windows amd64/arm64.
- `CGO_ENABLED=0`, `-trimpath`, `-s -w`, version ldflags.
- Archives: `utub_<version>_darwin_all.tar.gz`, `linux_<arch>.tar.gz`, `windows_<arch>.zip`,
  each containing `utub`/`utub.exe`, `LICENSE`, `README.md`.
- `checksums.txt` and auto-generated release notes.
- No tap, bucket, winget, cosign or SBOM steps.

Only `cmd/utub` is built. `cmd/utubd` is still empty — add it to `builds`/`archives` when
the web server milestone lands.

---

## 6. Publishing

```bash
git switch main
git pull --ff-only

git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1
```

Then:

```bash
gh run list --workflow=release.yml --limit 1
gh run watch <run-id> --exit-status
```

Green run → release exists with 5 archives + `checksums.txt`.

If you need to re-run before the release exists (e.g. transient GitHub failure), delete the
tag and push it again:

```bash
git tag -d v0.1.1
git push origin :refs/tags/v0.1.1
# fix, then re-tag
```

---

## 7. Verification

```bash
# 1. assets
gh release view v0.1.1 -R nnivxix/utub --json assets \
  --jq '[.assets[] | {name, size}]'

# 2. the artifact actually runs and is stamped
tmp=$(mktemp -d) && cd "$tmp"
gh release download v0.1.1 -R nnivxix/utub \
  --pattern 'utub_*_darwin_all.tar.gz' --pattern checksums.txt
tar xzf utub_*_darwin_all.tar.gz
./utub --version            # utub 0.1.1 darwin/arm64
lipo -archs utub            # arm64 x86_64
grep darwin_all checksums.txt | shasum -a 256 -c -

# 3. the module path works
d=$(mktemp -d)
GOBIN=$d go install github.com/nnivxix/utub/cmd/utub@v0.1.1
$d/utub --version

# 4. @latest now resolves to the new tag
go list -m github.com/nnivxix/utub@latest
```

Notes:

- `proxy.golang.org` may need a short moment before a brand-new tag resolves. Retry
  `go list -m ...@v0.1.1` after a minute if it 404s.
- `@latest` follows the newest **release** tag, not the branch head. Untagged commits are
  not served by `@latest`.

---

## 8. Rules — do not break these

1. **Never move or delete a published tag.** Once `proxy.golang.org` (and
   `sum.golang.org`) has cached a version, the content is permanent; re-tagging produces
   checksum failures for everyone who installed it.
2. **Treat a published release as immutable** even though GitHub's immutable-releases flag
   is currently **disabled** for this repo (checked 2026-11-02). Don't replace assets.
3. **Fix forward.** If a release is broken, cut `v0.1.2`. Optionally edit the old release
   notes to point at the fix; don't rewrite history.
4. **Tag only `main`.** No tags on feature branches or worktrees.
5. **Delete the tag only if the release was never created.** After that, forward-only.
6. Keep `go.mod`'s `go` directive at or below what the deps need if you want broader
   `go install` compatibility. Highest current dep requirement: `charm.land/bubbletea/v2`
   at **go 1.26.0**; this module says `go 1.27.3`, so older toolchains auto-download a
   newer one.

---

## 9. Bad-release playbook

| Situation | Action |
|---|---|
| Workflow failed, no release created | delete tag (local + remote), fix, re-tag the same version |
| Release created, one asset corrupt | forward-fix with a patch version (preferred), or re-run goreleaser against the tag before anyone consumes it |
| Binary builds but crashes at startup | publish a patch immediately; edit the broken release notes to say "superseded by vX" |
| Wrong version printed | the tag was wrong — new tag, never re-tag |
| `go install` fails with checksum mismatch | someone re-tagged; restore the original tag content or publish a new version and direct users there |

---

## 10. Optional improvements (not required)

- Fix the changelog filters (§4) so scoped commits and "Update README.md" stop appearing.
- Add `.github/workflows/ci.yml` so tags are only pushed after a green test run.
- Bump actions to `actions/checkout@v5` / `actions/setup-go@v6` to clear the Node 20
  deprecation annotation (harmless today).
- Add `workflow_dispatch` to the release workflow if you ever want to publish an existing
  tag manually (goreleaser still fails if the release already exists).
- README: make `go install` the primary install path and document that
  `$(go env GOPATH)/bin` (`%USERPROFILE%\go\bin`) must be on `PATH`.
- Homebrew/Scoop/winget remain optional; see `docs/distribution.md` for the deferred
  publishing plan.

---

## 11. File map

| File | Role |
|---|---|
| `cmd/utub/version.go` | version resolution (stamp → VCS → module → `dev`) |
| `cmd/utub/main.go` | `var version`, `init` hook, `--version`, `--doctor`, `--demo` |
| `Makefile` | `VERSION` from `git describe`, `tui`/`run`/`doctor` targets |
| `.goreleaser.yaml` | builds, universal binary, archives, checksums, changelog |
| `.github/workflows/release.yml` | tag → goreleaser → GitHub release |
| `go.mod` | module path and minimum Go/toolchain |
| `docs/distribution.md` | deferred Homebrew/Scoop/winget publishing plan (not in use) |
