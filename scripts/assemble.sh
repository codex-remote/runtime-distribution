#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: ./scripts/assemble.sh <version> [output-directory]" >&2
}

if [[ "$#" -lt 1 || "$#" -gt 2 ]]; then
  usage
  exit 2
fi

version="$1"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "${script_dir}/.." && pwd)"
workspace_dir="$(cd "${repo_dir}/.." && pwd)"
output_dir="${2:-${repo_dir}/dist}"
platform="darwin-arm64"
archive_name="codex-remote-runtime-${version}-${platform}.tar.gz"
stage_name="codex-remote-runtime-${version}-${platform}"
stage_dir="${output_dir}/${stage_name}"

if [[ "$(uname -s)" != "Darwin" || "$(uname -m)" != "arm64" ]]; then
  echo "Release assembly supports only macOS on Apple Silicon (darwin-arm64)." >&2
  exit 1
fi
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  echo "Version must be SemVer-compatible: ${version}" >&2
  exit 2
fi

for repository in relay-server mac-agent mobile-web; do
  if [[ -n "$(git -C "${workspace_dir}/${repository}" status --porcelain)" ]]; then
    echo "Refusing to assemble from dirty repository: ${repository}" >&2
    exit 1
  fi
done

mkdir -p "${output_dir}"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-release.XXXXXX")"
trap 'rm -rf "${temporary_dir}"' EXIT
build_dir="${temporary_dir}/bin"
mkdir -p "${build_dir}"

valkey_version="9.1.1"
valkey_sha256="7d7232acd1b8a49b4e05d07a00b3ca8c801ae06ab633ca6a3423bc5f385ab7ee"
valkey_archive="${VALKEY_SOURCE_ARCHIVE:-${temporary_dir}/valkey-${valkey_version}.tar.gz}"
if [[ ! -f "${valkey_archive}" ]]; then
  echo "Downloading pinned Valkey ${valkey_version} source..."
  curl --fail --location --retry 3 \
    "https://github.com/valkey-io/valkey/archive/refs/tags/${valkey_version}.tar.gz" \
    --output "${valkey_archive}"
fi
actual_valkey_sha256="$(shasum -a 256 "${valkey_archive}" | awk '{print $1}')"
if [[ "${actual_valkey_sha256}" != "${valkey_sha256}" ]]; then
  echo "Valkey source checksum mismatch: ${actual_valkey_sha256}" >&2
  exit 1
fi
mkdir -p "${temporary_dir}/valkey-source"
tar -xzf "${valkey_archive}" -C "${temporary_dir}/valkey-source"
echo "Building isolated Valkey runtime..."
env -u CFLAGS -u CPPFLAGS -u LDFLAGS -u PKG_CONFIG_PATH \
  make -C "${temporary_dir}/valkey-source/valkey-${valkey_version}" \
  -j"$(sysctl -n hw.ncpu)" BUILD_TLS=no MALLOC=libc valkey-server
env -u CFLAGS -u CPPFLAGS -u LDFLAGS -u PKG_CONFIG_PATH \
  make -C "${temporary_dir}/valkey-source/valkey-${valkey_version}" \
  -j"$(sysctl -n hw.ncpu)" BUILD_TLS=no MALLOC=libc valkey-cli
cp "${temporary_dir}/valkey-source/valkey-${valkey_version}/src/valkey-server" "${build_dir}/codex-remote-valkey-server"
cp "${temporary_dir}/valkey-source/valkey-${valkey_version}/src/valkey-cli" "${build_dir}/codex-remote-valkey-cli"

echo "Building Relay Server tools..."
(
  cd "${workspace_dir}/relay-server"
  go test ./...
  go vet ./...
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "${build_dir}/relay-server" ./cmd/relay
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "${build_dir}/relayctl" ./cmd/relayctl
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "${build_dir}/pairqr" ./cmd/pairqr
)

echo "Building Mac Agent..."
(
  cd "${workspace_dir}/mac-agent"
  go test ./...
  go vet ./...
  CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "${build_dir}/mac-agent" ./cmd/agent
)

echo "Building Mobile Web and Gateway..."
(
  cd "${workspace_dir}/mobile-web"
  npm ci --no-audit --no-fund
  npm test
  npm run build
  cd gateway
  go test ./...
  go vet ./...
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o "${build_dir}/mobile-web-gateway" .
)

echo "Building Codex Remote CLI..."
(
  cd "${repo_dir}"
  go test ./...
  go vet ./...
  CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=${version}" -o "${build_dir}/codex-remote" ./cmd/codex-remote
)

rm -rf "${stage_dir}"
mkdir -p "${stage_dir}/bin" "${stage_dir}/share/mobile-web" "${stage_dir}/LICENSES"
cp "${build_dir}/"* "${stage_dir}/bin/"
cp -R "${workspace_dir}/mobile-web/dist/." "${stage_dir}/share/mobile-web/"
cp "${repo_dir}/docs/BINARY-DISTRIBUTION-NOTICE" "${stage_dir}/LICENSES/"
cp "${temporary_dir}/valkey-source/valkey-${valkey_version}/COPYING" "${stage_dir}/LICENSES/Valkey-COPYING"
cp "${repo_dir}/THIRD_PARTY_NOTICES" "${stage_dir}/"

relay_commit="$(git -C "${workspace_dir}/relay-server" rev-parse HEAD)"
agent_commit="$(git -C "${workspace_dir}/mac-agent" rev-parse HEAD)"
mobile_commit="$(git -C "${workspace_dir}/mobile-web" rev-parse HEAD)"
distribution_commit="$(git -C "${repo_dir}" rev-parse HEAD)"
codex_version="$(codex --version 2>/dev/null || true)"
jq -n \
  --arg runtime_version "${version}" \
  --arg relay_commit "${relay_commit}" \
  --arg agent_commit "${agent_commit}" \
  --arg mobile_commit "${mobile_commit}" \
  --arg distribution_commit "${distribution_commit}" \
  --arg valkey_version "${valkey_version}" \
  --arg valkey_sha256 "${valkey_sha256}" \
  --arg codex_version "${codex_version}" \
  '{schemaVersion:1,runtimeVersion:$runtime_version,channel:"stable",components:{"runtime-distribution":{commit:$distribution_commit,tag:null},"relay-server":{commit:$relay_commit,tag:null},"mac-agent":{commit:$agent_commit,tag:null},"mobile-web":{commit:$mobile_commit,tag:null},"valkey":{version:$valkey_version,sourceSha256:$valkey_sha256,build:"darwin-arm64, non-TLS, libc"}},databaseSchema:2,platforms:["darwin-arm64"],codex:{minimumVersion:"0.148.0",maximumTestedVersion:$codex_version},artifacts:[]}' \
  > "${stage_dir}/manifest.json"

source_epoch="${SOURCE_DATE_EPOCH:-$(git -C "${repo_dir}" log -1 --format=%ct 2>/dev/null || date +%s)}"
touch_stamp="$(date -u -r "${source_epoch}" +%Y%m%d%H%M.%S)"
find "${stage_dir}" -exec touch -h -t "${touch_stamp}" {} +

archive_path="${output_dir}/${archive_name}"
rm -f "${archive_path}"
COPYFILE_DISABLE=1 tar --uid 0 --gid 0 --numeric-owner -C "${output_dir}" -cf - "${stage_name}" | gzip -n > "${archive_path}"
sha256="$(shasum -a 256 "${archive_path}" | awk '{print $1}')"
size="$(stat -f '%z' "${archive_path}")"
release_url="https://github.com/codex-remote/homebrew-tap/releases/download/v${version}/${archive_name}"
jq \
  --arg url "${release_url}" \
  --arg sha256 "${sha256}" \
  --argjson size "${size}" \
  '.artifacts=[{platform:"darwin-arm64",url:$url,byteSize:$size,sha256:$sha256}]' \
  "${stage_dir}/manifest.json" > "${output_dir}/release-manifest.json"

echo "Archive: ${archive_path}"
echo "SHA256:  ${sha256}"
echo "Manifest: ${output_dir}/release-manifest.json"
