# Homebrew and Scoop distribution

`sdkit` is one Go binary. The formula downloads the release archive for the platform and installs it. It has no dependencies: `jq`, `bash`, `python3` and `node` are not needed. Only an analyst repo needs `node`, for the OpenSpec CLI that `sdkit init --env analyst` installs with npm.

This repository is its own Homebrew **tap**: `Formula/sdkit.rb` is committed here.

## For developers
```
brew tap wecii/sdkit https://github.com/wecii/sdkit.git      # once
brew trust wecii/sdkit                                       # once; recent Homebrew refuses formulae from untrusted taps
brew install sdkit
brew upgrade sdkit                                           # later
```
Windows (Scoop; a `winget` package may follow): `scoop install` from `packaging/scoop/sdkit.json`, or download the
`sdkit_<version>_windows_<arch>.zip` release asset and put `sdkit.exe` on `PATH`. `git` is needed too.

Then, in a repo (one person, once): `sdkit init`. `sdkit doctor` checks a repo; `sdkit upgrade` brings a repo up to the
installed version after a `brew upgrade`. The git hook shims find the binary with `sdkit where`.

## For maintainers: every release
`.goreleaser.yaml` builds six targets (darwin, linux, windows; amd64, arm64): `tar.gz` archives (zip on Windows) and
`checksums.txt`. The `brews` and `scoops` sections write `dist/homebrew/Formula/sdkit.rb` and `dist/scoop/sdkit.json`
locally (`skip_upload: true`); nothing is published and no token is configured. `Formula/sdkit.rb` and
`packaging/scoop/sdkit.json` are the committed templates with `__SHA256_*__` placeholders.

0. Run `sdkit dev scan --history` (no hard findings; read the soft ones).
1. Bump the version in all five places (`plugin.json`, `marketplace.json`, README badge, `Formula/sdkit.rb`, `packaging/scoop/sdkit.json`).
2. Try it locally: `goreleaser release --snapshot --clean --skip=publish`, then check `dist/` has six archives, `checksums.txt`, the formula and the manifest.
3. Tag `v<version>` and push the tag. The `release` workflow builds the archives and `checksums.txt` and creates the GitHub Release (a manual run is a snapshot that publishes nothing).
4. Replace the `__SHA256_*__` placeholders in `Formula/sdkit.rb` and `packaging/scoop/sdkit.json` with the values from the release's `checksums.txt` (or copy the generated `dist/homebrew/Formula/sdkit.rb` and `dist/scoop/sdkit.json` from the workflow's `dist` artifact), commit on `master`.

CI templates (`templates/ci/*`) pin the `sdkit` version in the consumer's own CI file, so a new release reaches a repo's gate only when that repo merges a version bump.
