#!/usr/bin/env bash
#
# update-krew-manifest.sh — sync plugins/gpu-top.yaml with a published
# GitHub release, so it's ready to PR into kubernetes-sigs/krew-index.
#
# What it does:
#   1. Downloads checksums.txt from the given release tag.
#   2. Rewrites plugins/gpu-top.yaml in place:
#        - spec.version                 → <tag>
#        - every uri: .../download/...  → <tag>
#        - every sha256: <placeholder>  → real sha from checksums.txt
#   3. Commits the result on the current branch (does not push).
#   4. Prints the exact `gh` command block to submit the krew-index PR.
#
# Usage:
#   ./hack/update-krew-manifest.sh v0.2.0
#
# Requires: gh (authenticated), python3, git.
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  echo "usage: $0 <version-tag>   (e.g. v0.1.0)" >&2
  exit 1
fi
if [[ "$VERSION" != v* ]]; then
  echo "error: version must start with 'v' (got: $VERSION)" >&2
  exit 1
fi

REPO_SLUG="jia-gao/kube-gpu-top"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST="$REPO_ROOT/plugins/gpu-top.yaml"

if [[ ! -f "$MANIFEST" ]]; then
  echo "error: manifest not found: $MANIFEST" >&2
  exit 1
fi

for cmd in gh python3 git; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "error: required command not found: $cmd" >&2
    exit 1
  fi
done

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo ">> downloading checksums.txt for $REPO_SLUG $VERSION"
gh release download "$VERSION" \
  --repo "$REPO_SLUG" \
  --pattern "checksums.txt" \
  --dir "$tmp"

# Parse sha256 for each platform tarball.
declare -A shas
for platform in linux_amd64 linux_arm64 darwin_amd64 darwin_arm64; do
  file="kubectl-gpu-top_${VERSION}_${platform}.tar.gz"
  sha=$(awk -v f="$file" '$2==f {print $1}' "$tmp/checksums.txt")
  if [[ -z "$sha" ]]; then
    echo "error: no sha256 entry for $file in checksums.txt" >&2
    echo "contents of checksums.txt:" >&2
    cat "$tmp/checksums.txt" >&2
    exit 1
  fi
  shas[$platform]="$sha"
  printf '   %-14s  %s\n' "$platform" "$sha"
done

echo ">> rewriting $MANIFEST"
python3 - "$MANIFEST" "$VERSION" \
  "${shas[linux_amd64]}" "${shas[linux_arm64]}" \
  "${shas[darwin_amd64]}" "${shas[darwin_arm64]}" <<'PY'
import re
import sys

path, version = sys.argv[1], sys.argv[2]
shas = {
    "linux_amd64":  sys.argv[3],
    "linux_arm64":  sys.argv[4],
    "darwin_amd64": sys.argv[5],
    "darwin_arm64": sys.argv[6],
}

with open(path) as f:
    text = f.read()

# 1. Bump spec.version (only the first `  version: vX.Y.Z` line under spec).
text, n = re.subn(
    r'(?m)^(\s*version:\s*)v[0-9][^\s]*',
    lambda m: m.group(1) + version,
    text,
    count=1,
)
if n != 1:
    sys.exit("error: could not find `version:` line in manifest")

# 2. Replace both occurrences of the old tag in each uri line:
#    .../releases/download/v0.1.0/kubectl-gpu-top_v0.1.0_linux_amd64.tar.gz
text, n = re.subn(
    r'(/releases/download/)v[0-9][^/]*(/kubectl-gpu-top_)v[0-9][^_]*(_[^/]+\.tar\.gz)',
    lambda m: m.group(1) + version + m.group(2) + version + m.group(3),
    text,
)
if n != 4:
    sys.exit(f"error: expected to rewrite 4 uri lines, rewrote {n}")

# 3. For each uri/sha256 pair, substitute the sha256 matching its platform.
def rewrite_block(match):
    block = match.group(0)
    for platform, sha in shas.items():
        if f"_{platform}.tar.gz" in block:
            return re.sub(r'sha256:\s*\S+', f'sha256: {sha}', block)
    return block

text, n = re.subn(
    r'uri: https://[^\n]*\n\s*sha256:[^\n]*',
    rewrite_block,
    text,
)
if n != 4:
    sys.exit(f"error: expected to rewrite 4 sha256 lines, rewrote {n}")

with open(path, 'w') as f:
    f.write(text)
PY

# Sanity check: no placeholders left.
if grep -q REPLACE_WITH_SHA256 "$MANIFEST"; then
  echo "error: manifest still contains REPLACE_WITH_SHA256 placeholders" >&2
  grep -n REPLACE_WITH_SHA256 "$MANIFEST" >&2
  exit 1
fi

echo ">> diff:"
git -C "$REPO_ROOT" --no-pager diff -- "$MANIFEST" || true

# Commit if there are changes.
if git -C "$REPO_ROOT" diff --quiet -- "$MANIFEST"; then
  echo ">> no changes (manifest already up to date)"
else
  git -C "$REPO_ROOT" add "$MANIFEST"
  git -C "$REPO_ROOT" commit -m "chore(krew): update manifest for $VERSION"
  echo ">> committed locally on $(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD) (not pushed)"
fi

cat <<EOF

===============================================================================
 Next step: submit the PR to kubernetes-sigs/krew-index
===============================================================================

Run the following (one-time fork; subsequent releases reuse the same fork):

  # 1. Fork krew-index (skip if already forked)
  gh repo fork kubernetes-sigs/krew-index --clone --remote

  cd krew-index
  git checkout main
  git pull upstream main

  # 2. Create a release branch
  git checkout -b gpu-top-$VERSION

  # 3. Copy the updated manifest into the krew-index fork
  mkdir -p plugins
  cp "$MANIFEST" plugins/gpu-top.yaml

  # 4. Validate it locally (optional but strongly recommended)
  kubectl krew install --manifest=plugins/gpu-top.yaml

  # 5. Push and open the PR
  git add plugins/gpu-top.yaml
  git commit -m "gpu-top: bump to $VERSION"
  git push -u origin gpu-top-$VERSION
  gh pr create --repo kubernetes-sigs/krew-index \\
    --title "gpu-top: bump to $VERSION" \\
    --body  "Release notes: https://github.com/$REPO_SLUG/releases/tag/$VERSION"

EOF
