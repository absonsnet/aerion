#!/bin/bash
# Calculate SHA256 hashes for shim binaries and update the from-source Flathub manifest
# Usage: ./calculate-hashes.sh v0.1.17

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <version>"
    echo "Example: $0 v0.1.17"
    exit 1
fi

VERSION="$1"
# Second arg is the repo slug (owner/repo); defaults to upstream so the
# script still works when run by hand. The workflow passes
# ${{ github.repository }} so a fork resolves its own tag and release.
REPO="https://github.com/${2:-absonsnet/daak}"

echo "=========================================="
echo "Flathub Manifest Updater (from-source)"
echo "=========================================="
echo "Version: $VERSION"
echo "Repository: $REPO"
echo ""

# Get the directory where this script is located
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
MANIFEST="${SCRIPT_DIR}/net.absons.daak.yml"

if [ ! -f "$MANIFEST" ]; then
    echo "ERROR: Manifest file not found: $MANIFEST"
    exit 1
fi

# Create backup
cp "$MANIFEST" "${MANIFEST}.backup"

# ----- Step 1: Update git tag and commit hash -----
echo "Resolving commit hash for tag ${VERSION}..."

COMMIT_HASH=$(git ls-remote "${REPO}.git" "refs/tags/${VERSION}" | tail -1 | awk '{print $1}')
if [ -z "$COMMIT_HASH" ]; then
    echo "ERROR: Could not resolve commit hash for tag ${VERSION}"
    exit 1
fi
echo "   Tag: ${VERSION}"
echo "   Commit: ${COMMIT_HASH}"
echo ""

# Update tag in manifest
sed -i "s|tag: v[0-9.]*[-a-zA-Z0-9]*|tag: ${VERSION}|" "$MANIFEST"

# Update commit hash in manifest
sed -i "s|commit: [0-9a-f]\{40\}|commit: ${COMMIT_HASH}|" "$MANIFEST"

# Point the git source at this repo — a fork's tag doesn't exist upstream.
sed -i "s|url: https://github.com/[^/]*/daak.git|url: ${REPO}.git|" "$MANIFEST"

# ----- Step 2: Download shim binaries and calculate hashes -----
echo "Downloading shim binaries..."
echo ""

TEMP_DIR=$(mktemp -d)
cd "$TEMP_DIR"

# x86_64 shim
CREDS_X86="flathub-build-env-${VERSION}-linux-x86_64"
if wget -q "${REPO}/releases/download/${VERSION}/${CREDS_X86}"; then
    CREDS_X86_SHA256=$(sha256sum "$CREDS_X86" | awk '{print $1}')
    echo "   x86_64 shim: ${CREDS_X86_SHA256}"
else
    # Fail fast: a missing shim means the release is incomplete, and
    # writing a bogus hash lets the pipeline continue to Flatpak where
    # it fails confusingly on a 404 (v0.4.0/v0.4.1 both did this).
    echo "   ERROR: Could not download x86_64 shim binary — it is missing from the release" >&2
    exit 1
fi

# aarch64 shim
CREDS_ARM="flathub-build-env-${VERSION}-linux-aarch64"
if wget -q "${REPO}/releases/download/${VERSION}/${CREDS_ARM}"; then
    CREDS_ARM_SHA256=$(sha256sum "$CREDS_ARM" | awk '{print $1}')
    echo "   aarch64 shim: ${CREDS_ARM_SHA256}"
else
    echo "   ERROR: Could not download aarch64 shim binary — it is missing from the release" >&2
    exit 1
fi

echo ""

# Cleanup temp dir
cd - > /dev/null
rm -rf "$TEMP_DIR"

# ----- Step 3: Update shim binary URLs and hashes in manifest -----
echo "Updating manifest..."

# Update x86_64 shim URL
sed -i "s|url: https://github.com/[^/]*/daak/releases/download/v[0-9.]*[-a-zA-Z0-9]*/flathub-build-env-v[0-9.]*[-a-zA-Z0-9]*-linux-x86_64|url: ${REPO}/releases/download/${VERSION}/${CREDS_X86}|" "$MANIFEST"

# Update aarch64 shim URL
sed -i "s|url: https://github.com/[^/]*/daak/releases/download/v[0-9.]*[-a-zA-Z0-9]*/flathub-build-env-v[0-9.]*[-a-zA-Z0-9]*-linux-aarch64|url: ${REPO}/releases/download/${VERSION}/${CREDS_ARM}|" "$MANIFEST"

# Update x86_64 shim sha256 (find the sha256 line after the x86_64 URL)
awk -v sha="$CREDS_X86_SHA256" '
/url:.*linux-x86_64$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

# Update aarch64 shim sha256 (find the sha256 line after the aarch64 URL)
awk -v sha="$CREDS_ARM_SHA256" '
/url:.*linux-aarch64$/ { found=1; print; next }
found && /sha256:/ {
    match($0, /^[ \t]*/);
    spaces=substr($0, 1, RLENGTH);
    print spaces "sha256: " sha;
    found=0;
    next
}
{ print }
' "$MANIFEST" > "${MANIFEST}.tmp" && mv "${MANIFEST}.tmp" "$MANIFEST"

echo ""
echo "Manifest updated successfully!"
echo "   Backup saved: ${MANIFEST}.backup"
echo ""
echo "=========================================="
echo "Summary:"
echo "=========================================="
echo ""
echo "Git source:"
echo "  tag: ${VERSION}"
echo "  commit: ${COMMIT_HASH}"
echo ""
echo "Shim binaries:"
echo "  x86_64:  ${CREDS_X86_SHA256}"
echo "  aarch64: ${CREDS_ARM_SHA256}"
echo ""
echo "=========================================="
echo "Next steps:"
echo "1. Review changes: diff ${MANIFEST}.backup ${MANIFEST}"
echo "2. Test build: flatpak-builder --force-clean build-dir ${MANIFEST}"
echo "=========================================="
