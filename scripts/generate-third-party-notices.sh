#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd "${script_dir}/.." && pwd)"
workspace_dir="$(cd "${repo_dir}/.." && pwd)"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/codex-remote-licenses.XXXXXX")"
trap 'rm -rf "${temporary_dir}"' EXIT

license_root="${temporary_dir}/LICENSES"
notice_path="${temporary_dir}/THIRD_PARTY_NOTICES"
mkdir -p "${license_root}/go" "${license_root}/node"

sanitize() {
  printf '%s' "$1" | tr '/@:' '___' | tr -cd 'A-Za-z0-9._+-'
}

find_license() {
  find "$1" -maxdepth 1 -type f | awk -F/ '
    tolower($NF) ~ /^(license|licence|copying|notice)(\..*)?$/ { print; exit }
  '
}

go_modules="${temporary_dir}/go-modules.tsv"
: > "${go_modules}"
for repository in relay-server mac-agent; do
  module_dir="${temporary_dir}/go-${repository}"
  mkdir -p "${module_dir}"
  cp "${workspace_dir}/${repository}/go.mod" "${workspace_dir}/${repository}/go.sum" "${module_dir}/"
  (
    cd "${module_dir}"
    go mod download all
    go list -m -json all | jq -rs '.[] | select(.Main != true) | [.Path, .Version, .Dir] | @tsv'
  ) >> "${go_modules}"
done
sort -u -o "${go_modules}" "${go_modules}"

{
  echo "Codex Remote Third-Party Notices"
  echo
  echo "This distribution includes the following third-party components."
  echo "The complete license text for each entry is stored at the listed path."
  echo
  echo "Go modules"
  echo "----------"
} > "${notice_path}"

while IFS=$'\t' read -r module version directory; do
  [[ -n "${module}" && -n "${directory}" ]] || continue
  license_file="$(find_license "${directory}")"
  if [[ -z "${license_file}" ]]; then
    echo "No license file found for Go module ${module}@${version}" >&2
    exit 1
  fi
  destination="go/$(sanitize "${module}@${version}").txt"
  cp "${license_file}" "${license_root}/${destination}"
  printf -- '- %s %s - LICENSES/%s\n' "${module}" "${version}" "${destination}" >> "${notice_path}"
done < "${go_modules}"

{
  echo
  echo "JavaScript packages"
  echo "-------------------"
} >> "${notice_path}"

node_packages="${temporary_dir}/node-packages.txt"
(
  cd "${workspace_dir}/mobile-web"
  npm ls --omit=dev --all --parseable 2>/dev/null | tail -n +2
) | sort -u > "${node_packages}"

while IFS= read -r directory; do
  [[ -n "${directory}" && -f "${directory}/package.json" ]] || continue
  package="$(jq -r '.name // empty' "${directory}/package.json")"
  version="$(jq -r '.version // empty' "${directory}/package.json")"
  license="$(jq -r 'if (.license | type) == "string" then .license else "See license text" end' "${directory}/package.json")"
  license_file="$(find_license "${directory}")"
  if [[ -z "${package}" || -z "${version}" || -z "${license_file}" ]]; then
    echo "Incomplete license metadata for Node package at ${directory}" >&2
    exit 1
  fi
  destination="node/$(sanitize "${package}@${version}").txt"
  cp "${license_file}" "${license_root}/${destination}"
  printf -- '- %s %s (%s) - LICENSES/%s\n' "${package}" "${version}" "${license}" "${destination}" >> "${notice_path}"
done < "${node_packages}"

{
  echo
  echo "Valkey 9.1.1"
  echo "------------"
  echo "Valkey is built from pinned source without TLS and with the system allocator."
  echo "Its BSD 3-Clause license is included as LICENSES/Valkey-COPYING."
} >> "${notice_path}"

rm -rf "${repo_dir}/LICENSES"
mv "${license_root}" "${repo_dir}/LICENSES"
mv "${notice_path}" "${repo_dir}/THIRD_PARTY_NOTICES"
echo "Generated ${repo_dir}/THIRD_PARTY_NOTICES and ${repo_dir}/LICENSES"
