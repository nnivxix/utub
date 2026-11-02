# Publishing plan — Homebrew, Scoop and Linux packages

Status: **deferred backlog**. Distribution is currently `go install` plus the GitHub
Releases archives; Homebrew, Scoop and winget are intentionally not wired up. The active
release process is [releasing.md](releasing.md).

Last updated: 2026-11-02 (repo at `v0.1.0`, commit `ecd1ee1`).

> This is a handoff document. It is deliberately separate from `PLAN.md`; do not append
> progress here to `PLAN.md`, and do not treat this file as the project status log.

---

## 1. What already works

| Piece | State |
|---|---|
| `nnivxix/utub` release workflow | works: tag `v*` → goreleaser on `macos-latest`, 2 min |
| Release assets | 5 archives (darwin universal, linux amd64/arm64, windows amd64/arm64) + `checksums.txt` |
| `utub --version` | stamped `0.1.0` via goreleaser ldflags; `go install` reports the tag |
| `nnivxix/homebrew-tap` | public, default branch `main`, no branch protection, has `Formula/gorbang.rb` and a README table |
| `nnivxix/scoop-bucket` | **does not exist** |
| Repo secrets | none |

Decisions already taken, do not re-litigate:

- Ship the **TUI only** for now (`cmd/utubd` is empty).
- yt-dlp/ffmpeg are **not bundled**; package managers install them or `utub --doctor` guides the user.
- **No code signing / notarization** yet.
- **winget is parked** (huge moderation queue); Scoop is the Windows channel.
- Do **not** re-tag `v0.1.0`. Cut `v0.1.1` after the publishers are wired.

---

## 2. Phase 0 — accounts and secrets (only the repo owner can do this)

### 2.1 Create the Scoop bucket

```bash
gh repo create nnivxix/scoop-bucket --public --add-readme
```

Leave it without manifests. GoReleaser writes `utub.json` at the repository **root**
(its docs recommend root over `bucket/`, otherwise `scoop` does not see the manifest).

### 2.2 Create two fine-grained PATs

<https://github.com/settings/personal-access-tokens/new>

| PAT name | Repository access | Permission | Secret name |
|---|---|---|---|
| `utub-homebrew-tap` | only `nnivxix/homebrew-tap` | Contents: **Read and write** | `TAP_GITHUB_TOKEN` |
| `utub-scoop-bucket` | only `nnivxix/scoop-bucket` | Contents: **Read and write** | `SCOOP_TOKEN` |

Metadata read is granted automatically. Set an expiry and put the rotation date in a
calendar. The default `GITHUB_TOKEN` cannot push to another repository — that is the
whole reason these exist.

Store them:

```bash
gh secret set TAP_GITHUB_TOKEN -R nnivxix/utub
gh secret set SCOOP_TOKEN     -R nnivxix/utub
```

Verify:

```bash
gh secret list -R nnivxix/utub
```

Optional hardening: create a `release` GitHub Environment, put both secrets there, and add
`environment: release` to the workflow job. Skipped here for simplicity.

---

## 3. Phase 1 — repository changes

### 3.1 Pick the Homebrew flavour

Three viable options:

| Option | User command | Automation | Unsigned-binary risk |
|---|---|---|---|
| GoReleaser `brews` (prebuilt formula) | `brew install nnivxix/tap/utub` | full, per tag | none — formulas are not quarantined |
| `homebrew_casks` | `brew install --cask nnivxix/tap/utub` | full, per tag | needs the `xattr` hook until notarized |
| Manual source formula (like gorbang) | `brew install nnivxix/tap/utub` | manual `url`/`sha256` bump | none, but users compile |

**Recommended: `brews`.** Fast install of the 4.8 MB universal binary, automated, no
quarantine, matches the existing `Formula/` directory. Caveat: `brews` is marked
deprecated since GoReleaser v2.10 (still functional in v2.19). Migrate to
`homebrew_casks` once the binary is signed and notarized — at that point the cask is the
"proper" artifact and the quarantine workaround disappears.

If the deprecated pipeline is unacceptable, use `homebrew_casks` with this hook:

```yaml
hooks:
  post:
    install_steps: |
      on_macos do
        run "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "{{ .StagedPath }}/utub"]
      end
```

Do not use the source-build formula style unless automation is also added; it needs a
version/hash bump per release and requires Go on the user's machine.

### 3.2 `.goreleaser.yaml` — add the publishers

```yaml
brews:
  - name: utub
    ids:
      - utub
    repository:
      owner: nnivxix
      name: homebrew-tap
      branch: main
      token: "{{ .Env.TAP_GITHUB_TOKEN }}"
    directory: Formula
    homepage: https://github.com/nnivxix/utub
    description: "YouTube / YouTube Music downloader with a terminal UI"
    license: MIT
    install: |
      bin.install "utub"
    test: |
      system "#{bin}/utub", "--version"
    dependencies:
      - name: yt-dlp
      - name: ffmpeg
    skip_upload: auto

scoops:
  - name: utub
    repository:
      owner: nnivxix
      name: scoop-bucket
      branch: main
      token: "{{ .Env.SCOOP_TOKEN }}"
    homepage: https://github.com/nnivxix/utub
    description: "YouTube / YouTube Music downloader with a terminal UI"
    license: MIT
    depends:
      - yt-dlp
      - ffmpeg
    skip_upload: auto
```

Notes:

- The GoReleaser key is `scoops:` (plural), even though the docs prose says "scoop section".
- `skip_upload: auto` keeps pre-releases (`v0.1.1-rc.1`) out of the tap and bucket.
- `brew`/`scoop` `depends`/`dependencies` auto-install yt-dlp and ffmpeg; both package
  names exist in Homebrew core and Scoop's `main` bucket.
- If `brews` is rejected by a future GoReleaser major, switch to `homebrew_casks` (see 3.1).

### 3.3 `.github/workflows/release.yml` — pass the tokens

Add to the goreleaser step's `env`:

```yaml
          TAP_GITHUB_TOKEN: ${{ secrets.TAP_GITHUB_TOKEN }}
          SCOOP_TOKEN: ${{ secrets.SCOOP_TOKEN }}
```

### 3.4 `README.md` — install section

Replace the current Install block with:

```markdown
## Install

| Platform | Command |
|---|---|
| macOS | `brew install nnivxix/tap/utub` |
| Windows | `scoop bucket add nnivxix https://github.com/nnivxix/scoop-bucket`<br>`scoop install utub` |
| Linux | `.deb` / `.rpm` / `.apk` or tarball from [Releases](https://github.com/nnivxix/utub/releases) |
| Any | `go install github.com/nnivxix/utub/cmd/utub@latest` |

Homebrew and Scoop install `yt-dlp` and `ffmpeg` automatically. On other platforms run
`utub --doctor` to see what is missing.
```

Also add a utub row to the `nnivxix/homebrew-tap` README table:

```markdown
| [utub](https://github.com/nnivxix/utub) | YouTube / YouTube Music downloader TUI | `brew install nnivxix/tap/utub` |
```

### 3.5 Optional: Linux packages (`nfpms`)

Self-contained — no secrets or external repos. Adds `.deb`, `.rpm`, `.apk` to the release:

```yaml
nfpms:
  - id: packages
    package_name: utub
    vendor: nnivxix
    homepage: https://github.com/nnivxix/utub
    maintainer: nnivxix
    description: YouTube / YouTube Music downloader with a terminal UI
    license: MIT
    formats:
      - deb
      - rpm
      - apk
    contents:
      - src: ./LICENSE
        dst: /usr/share/licenses/utub/LICENSE
    recommends:
      - yt-dlp
      - ffmpeg
```

`recommends` is honoured by deb/rpm; apk has no weak dependencies and will ignore it.
Use `recommends` rather than `depends` — Ubuntu 22.04's yt-dlp is too old, and a hard
dependency makes the package uninstallable on minimal systems.

### 3.6 Version bump

Nothing to do. The tag drives the version; `utub --version` and the Makefile both read it.
The next tag is `v0.1.1`.

---

## 4. Phase 2 — cut the release

Only after the two secrets exist and `3.2`/`3.3` are merged.

```bash
git add .goreleaser.yaml .github/workflows/release.yml README.md
git commit -m "chore(release): publish to homebrew and scoop"
git push

git tag -a v0.1.1 -m "v0.1.1"
git push origin v0.1.1

gh run watch
```

Expected result:

- GitHub release `v0.1.1` with the usual archives.
- `nnivxix/homebrew-tap` gets a commit: `Formula/utub.rb`.
- `nnivxix/scoop-bucket` gets a commit: `utub.json` at the root.

Do **not** attempt to publish the tap/bucket for `v0.1.0`; the tag is already out and
re-tagging is messier than a `v0.1.1` patch release.

---

## 5. Phase 3 — verify

### macOS (use a machine/account that never installed utub)

```bash
brew tap nnivxix/tap
brew install nnivxix/tap/utub
utub --version        # 0.1.1
utub --doctor         # yt-dlp + ffmpeg must be present, pulled in as deps
brew uninstall utub
```

### Windows (VM or a `windows-latest` job)

```powershell
scoop bucket add nnivxix https://github.com/nnivxix/scoop-bucket
scoop install utub
utub --version
utub --doctor
```

### Linux (if 3.5 was done)

```bash
sudo dpkg -i utub_0.1.1_linux_amd64.deb
utub --doctor
dpkg -s utub | grep -i recommends
```

Also check:

- `brew update && brew upgrade utub` picks up the next tag.
- `scoop update; scoop update utub` does the same.
- The tap formula's `test` block runs: `brew test utub`.

---

## 6. Troubleshooting

| Symptom | Cause / fix |
|---|---|
| `resource not accessible by integration` | PAT missing, wrong repo, or Contents is read-only |
| Release succeeded, tap/bucket empty | publisher step failed; read the goreleaser log, fix the token, then re-run |
| `brew install` says formula not found | tap not updated: `brew update`, or check `Formula/utub.rb` on the tap's `main` |
| Scoop cannot find the manifest | manifest must be at the bucket repo root, not `bucket/` |
| Formula installs but `utub` fails | arm64/x86_64 mismatch — confirm the universal archive was used |
| Future release fails only on publishers | expired PAT; rotate and update the secret |

Re-running goreleaser for a tag whose GitHub release already exists will fail on release
creation. If a release is half-published, the clean fix is to delete the release and tag,
fix the cause, and re-tag — acceptable while the project is pre-1.0, but avoid it by
testing the tokens before the first tag.

---

## 7. Deferred (do not do in this pass)

- **winget** — needs a fork of `microsoft/winget-pkgs`, a PAT, and weeks of moderation.
- **Apple signing/notarization** — needed only for the cask/direct-download path; the
  formula path does not require it.
- **homebrew-core** — needs notability (stars/forks) and a PR to Homebrew.
- **AUR** — needs an AUR account and SSH key; add `aurs:` later if desired.
- **Cosign signatures / SBOMs** — release currently ships checksums only.
- **CI workflow for pull requests** — only the release workflow exists today.

---

## 8. Checklist

- [ ] `nnivxix/scoop-bucket` created (public, `main`, README only)
- [ ] `TAP_GITHUB_TOKEN` created (Contents: RW on `homebrew-tap`)
- [ ] `SCOOP_TOKEN` created (Contents: RW on `scoop-bucket`)
- [ ] Both secrets added to `nnivxix/utub`
- [ ] Homebrew flavour chosen (`brews` recommended)
- [ ] `.goreleaser.yaml` publishers added
- [ ] `release.yml` env updated
- [ ] README install section rewritten
- [ ] Tap README table updated
- [ ] (optional) `nfpms` added
- [ ] `v0.1.1` tagged and workflow green
- [ ] `brew install` verified on a clean Mac
- [ ] `scoop install` verified on Windows
- [ ] `utub --doctor` confirms yt-dlp/ffmpeg arrived as dependencies
