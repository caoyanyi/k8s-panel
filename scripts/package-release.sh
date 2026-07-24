#!/usr/bin/env bash

set -euo pipefail

version="${1:-}"
commit="${2:-unknown}"
output_dir="${3:-dist}"

if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$ ]]; then
  echo "package-release: version must be a SemVer value without the v prefix" >&2
  exit 2
fi
if [[ ! "$commit" =~ ^[0-9a-fA-F]{7,64}$ ]]; then
  echo "package-release: commit must be a hexadecimal Git object ID" >&2
  exit 2
fi
if [[ ! -d web/dist ]]; then
  echo "package-release: web/dist is missing; run npm run build --prefix web first" >&2
  exit 1
fi

mkdir -p "$output_dir"
if find "$output_dir" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
  echo "package-release: output directory must be empty: $output_dir" >&2
  exit 1
fi
output_dir="$(cd "$output_dir" && pwd)"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

ldflags="-s -w -X github.com/caoyanyi/k8s-panel/internal/buildinfo.Version=${version} -X github.com/caoyanyi/k8s-panel/internal/buildinfo.Commit=${commit}"
targets=(
  "linux amd64"
  "linux arm64"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
)

for target in "${targets[@]}"; do
  read -r target_os target_arch <<<"$target"
  archive_name="k8s-panel_${version}_${target_os}_${target_arch}"
  package_dir="$work_dir/$archive_name"
  binary_suffix=""
  if [[ "$target_os" == "windows" ]]; then
    binary_suffix=".exe"
  fi

  mkdir -p "$package_dir/web"
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    go build -trimpath -ldflags="$ldflags" -o "$package_dir/panel$binary_suffix" ./cmd/panel
  CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
    go build -trimpath -ldflags="$ldflags" -o "$package_dir/panelctl$binary_suffix" ./cmd/panelctl
  cp -R web/dist "$package_dir/web/dist"
  cp README.md .env.example "$package_dir/"

  if [[ "$target_os" == "windows" ]]; then
    (
      cd "$work_dir"
      zip -X -q -r "$output_dir/$archive_name.zip" "$archive_name"
    )
  else
    tar --sort=name --owner=0 --group=0 --numeric-owner \
      -C "$work_dir" -czf "$output_dir/$archive_name.tar.gz" "$archive_name"
  fi
done

(
  cd "$output_dir"
  sha256sum k8s-panel_*.tar.gz k8s-panel_*.zip > checksums.txt
)
