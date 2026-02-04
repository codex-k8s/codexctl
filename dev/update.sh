#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "error: not a git repository" >&2
  exit 1
fi

CURRENT_TAG="$(git tag --list 'v*' --sort=-v:refname | head -n 1)"
if [[ -z "$CURRENT_TAG" ]]; then
  NEXT_TAG="v0.0.1"
  echo "no tags found, using default: $NEXT_TAG"
else
  if [[ "$CURRENT_TAG" != v* ]]; then
    echo "error: unexpected tag format: $CURRENT_TAG" >&2
    exit 1
  fi

  VERSION="${CURRENT_TAG#v}"
  IFS='.' read -r MAJOR MINOR PATCH <<<"$VERSION"

  if [[ -z "$MAJOR" || -z "$MINOR" || -z "$PATCH" ]]; then
    echo "error: tag must be in form vX.Y.Z, got $CURRENT_TAG" >&2
    exit 1
  fi

  NEXT_MAJOR="v$((MAJOR + 1)).0.0"
  NEXT_MINOR="v${MAJOR}.$((MINOR + 1)).0"
  NEXT_PATCH="v${MAJOR}.${MINOR}.$((PATCH + 1))"

  echo "current tag: $CURRENT_TAG"
  echo "select next tag:"
  echo "1) $NEXT_MAJOR"
  echo "2) $NEXT_MINOR"
  echo "3) $NEXT_PATCH"

  read -r -p "choice (1-3): " CHOICE
  case "$CHOICE" in
    1) NEXT_TAG="$NEXT_MAJOR" ;;
    2) NEXT_TAG="$NEXT_MINOR" ;;
    3) NEXT_TAG="$NEXT_PATCH" ;;
    *) echo "error: invalid choice" >&2; exit 1 ;;
  esac
fi

if git rev-parse "$NEXT_TAG" >/dev/null 2>&1; then
  echo "error: tag already exists: $NEXT_TAG" >&2
  exit 1
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "error: working tree has uncommitted changes" >&2
  exit 1
fi

git tag -a "$NEXT_TAG" -m "$NEXT_TAG"
git push origin "$NEXT_TAG"

echo "warming go proxy: $NEXT_TAG"
GOPROXY=proxy.golang.org go list -m "github.com/codex-k8s/codexctl@$NEXT_TAG"

echo "done: $NEXT_TAG"
