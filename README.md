# Opa

Opa is the centralized OPA (Open Policy Agent) policy evaluation service.
It executes typed OPA policy evaluations accessible to any service via S2S
gRPC. Policy definitions live in shared protos (`proto/opa/`) and are
importable by any service. Tacplus generates typed evaluation RPCs on Opa
and typed client wrappers on importing services' ServiceManagers.

The name is a play on the German translation of OPA (grandpa) — Uncle Grandpa.

## Architecture

Services that need policy decisions call Opa via generated S2S gRPC
clients. Opa evaluates OPA bundles against typed input messages and
returns typed evaluation results. Only kernel has direct access to Opa;
other services' policy evaluations are mediated through kernel.

## Configuration

| Field | Description |
|-------|-------------|
| `entitlements_policy` | OPA bundle for entitlement evaluation |
| `proxy_policy` | OPA bundle for proxy request evaluation (`data.proxypolicy.eval`) |

The proxy policy's result is an object whose fields are all optional:
`allow` (bool; missing denies), `url` and `reason` (strings), `append_aud`
(a string or a list of strings), `extra_claims` (an object), and `auth_type`
and `token_use` (a `TokenType`/`TokenUse` name, its short form or its number).
A present field of another type, or an enum name or number that is not
defined, fails `EvalProxyPolicy` with `INTERNAL` naming the field: kernel mints
no token rather than one without what the policy wrote.

## Build

```bash
just        # generate + compile
just gates  # what every change and release must pass
```

CI ([ci.yml](.github/workflows/ci.yml)) runs `just gates` on main and pull requests in the build image
`tacplus.lock` pins, by digest ([ci/build-image.sh](ci/build-image.sh)).

## Releasing

opa is released by version, and fleets pin a release line, `MAJOR.MINOR`, which is always that line's highest
patch ([tacplus#180](https://github.com/tacplus-dev/tacplus/issues/180)). Versions are `0.x` until the first
production launch (2026-12-01), when `1.0.0` is cut; on `0.x` a minor may break, a patch never does. A change merged to main
is released by the looper as the next patch (a fix) or the next minor (a feature), as a tag on main (a patch to an
older line is tagged on its `release/MAJOR.MINOR` branch). To cut one by hand, from the commit to release:

```sh
git tag -a -m "opa 0.2.2: <what>" v0.2.2   # vMAJOR.MINOR.PATCH
git push origin v0.2.2
```

The tag runs [release.yml](.github/workflows/release.yml): the gates on the tag, in the build image its
`tacplus.lock` pins, then `ghcr.io/tacplus-dev/opa:<version>` ([ci/release-build.sh](ci/release-build.sh),
[ci/release-image.sh](ci/release-image.sh)), labelled with the version and commit
(`org.opencontainers.image.version`, `.revision`), and `:<MAJOR.MINOR>` moved to it only when it is the line's
highest patch, so the line tag never goes backwards and no other line moves
([ci/release-tags.sh](ci/release-tags.sh)). The image is for linux/arm64, what the fleets run on; linux/amd64 is
off by default, as in the fleets: `PLATFORMS: arm64 amd64` in release.yml builds both into one image. The GitHub
release's notes are the commits since the previous release ([ci/release-notes.sh](ci/release-notes.sh)), and it
is marked Latest only when it is the highest release of all ([ci/release-latest.sh](ci/release-latest.sh)). A
pre-release (`v0.3.0-rc.1`) publishes only its own tag. The package is public: pulls need no credential. Never
move or delete a tag once it is pushed.

Running the workflow by hand builds a version without a push of its tag:

```sh
gh workflow run release.yml -f version=v0.1.0 -f publish=true   # publish an existing tag
gh workflow run release.yml -f version=v0.2.2                   # dry run: builds, prints tags and notes, publishes nothing
```

With `publish`, the version must be an existing tag, one made before release.yml existed (v0.1.0 to v0.2.1) or
whose run failed: its own source is checked out, gated, built and published as a push of it would be, by main's
`ci/` ([ci/checkout.sh](ci/checkout.sh)); the tag never moves. A tag that predates `tacplus.lock` (v0.1.0) is
built with the pin [ci/locks/](ci/locks/) names for it. A dry run of an existing tag builds it; of any other
version, it builds main's head, tagged locally. The release rules are tested by
[ci/release_test.sh](ci/release_test.sh), part of `just gates`. The launcher in the image is built from source
at `LAUNCHER_COMMIT` in release.yml.

## Proto

[`services/opa.proto`](../../proto/services/opa.proto)
