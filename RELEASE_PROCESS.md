# Release Process

GoReleaser publishes releases to GitHub. The configuration lives in [`.goreleaser.yaml`](.goreleaser.yaml).

## What GoReleaser Does

- Runs `go mod tidy` and `go generate ./...` before building
- Builds binaries for Linux, macOS, and Windows on `amd64` and `arm64`
- Packages binaries as `.tar.gz` (`.zip` on Windows)
- Generates a changelog from commits since the previous tag (excluding `docs:`
  and `test:` prefixes)
- Creates a GitHub release and uploads all artifacts

## Prerequisites

- [GoReleaser](https://goreleaser.com/install/) installed
- A GitHub personal access token with the `repo` scope, exported as `GITHUB_TOKEN`

```sh
export GITHUB_TOKEN=<your-token>
```

GoReleaser uses `GITHUB_TOKEN` to publish the GitHub release. `gowheels pypi`
reads its own `GOWHEELS_GITHUB_TOKEN` env var to download release assets
without hitting API rate limits — these two variables serve different purposes.

## Steps

### 1. Tag the Release

```sh
git tag -a v0.5.0 -m "Release v0.5.0"
git push origin v0.5.0
```

Use [semantic versioning](https://semver.org): `vMAJOR.MINOR.PATCH`.

### 2. Run GoReleaser

```sh
goreleaser release --clean
```

`--clean` removes the `dist/` directory before building to ensure a fresh output.

### 3. Update This Document to Replace the Old Version Number with the Next Release Tag

```text
./.github/release.sh
```

## Testing a Release Locally

Build and package artifacts without publishing to GitHub:

```sh
goreleaser release --clean --skip=publish
```

Or build a snapshot (no tag required):

```sh
goreleaser release --snapshot --clean
```

GoReleaser writes artifacts to `dist/`.

## Useful Flags

| Flag              | Effect                                              |
| ----------------- | --------------------------------------------------- |
| `--clean`         | Delete `dist/` before building                      |
| `--skip=publish`  | Build and package but do not publish to GitHub      |
| `--skip=validate` | Skip dirty-tree and tag checks                      |
| `--snapshot`      | Build without a tag; implies `--skip=publish`       |
| `--draft`         | Create a draft GitHub release instead of publishing |

## Publishing Python Wheels to PyPI

After a GitHub release exists, you can repackage the pre-built binaries
attached to it as Python wheels and publish them to PyPI using gowheels.

### Install Gowheels

```sh
go install github.com/StevenACoffman/gowheels@latest
```

### Build Wheels Locally (No Upload)

```sh
gowheels pypi --package-name defederator --name defederator --repo StevenACoffman/defederator
```

gowheels writes wheels to `./dist/`. Inspect them before uploading.

### Build and Upload to PyPI in One Step

```sh
export GOWHEELS_GITHUB_TOKEN=<your-token>    # read as --github-token; avoids GitHub API rate limits
export GOWHEELS_PYPI_TOKEN=<your-pypi-token> # read as --pypi-token; authenticates the upload

gowheels pypi --name defederator --package-name defederator --repo StevenACoffman/defederator --upload
```

gowheels reads `GOWHEELS_GITHUB_TOKEN` and `GOWHEELS_PYPI_TOKEN` automatically
from the environment — you do not need the `--github-token` or `--pypi-token`
flags when you set them. `gowheels pypi` fetches the release assets from
GitHub, extracts the binary from each archive, wraps it in a platform-specific
wheel, and uploads each wheel to PyPI.

To target a specific release tag rather than the latest:

```sh
gowheels pypi --name defederator --package-name defederator --repo StevenACoffman/defederator --version v0.5.0 --upload
```

---

## Appendix: Automated PyPI Publishing via GitHub Actions

`.github/workflows/postrelease.yaml` runs automatically whenever you publish a
GitHub release. It uses `gowheels pypi --upload` to build the wheels and
publish them to PyPI via OIDC in a single step — you do not need a
`GOWHEELS_PYPI_TOKEN` secret.

### One-Time Setup: PyPI Trusted Publishing

1. **Create a PyPI account** at <https://pypi.org> if you do not already have one.

2. **Register the project** by publishing the first release manually (see the
   `gowheels pypi` steps above), or by creating the project name on PyPI
   before the first automated run.

3. **Add a trusted publisher** on PyPI:
   - Go to your project page on PyPI → **Manage** → **Publishing**.
   - Under *Add a new publisher*, choose **GitHub Actions**.
   - Fill in the fields:

     | Field            | Value              |
     | ---------------- | ------------------ |
     | Owner            | `StevenACoffman`   |
     | Repository name  | `defederator`           |
     | Workflow name    | `postrelease.yaml` |
     | Environment name | `pypi`             |

   - Click **Add**.

4. **Create the `pypi` environment** in the GitHub repository:
   - Go to the repository on GitHub → **Settings** → **Environments** → **New environment**.
   - Name it `pypi`.
   - Optionally add a required reviewer or deployment branch rule (for example,
     restrict to tags matching `v*`) for extra protection.

Once both sides are configured, pushing a new tag and running the **Build and
Publish** workflow (which creates the GitHub release) will automatically trigger
`postrelease.yaml`, build the wheels, and publish them to PyPI without any
secrets.
