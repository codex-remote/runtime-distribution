#!/usr/bin/env bash
set -euo pipefail

if [[ "$#" -lt 3 || "$#" -gt 4 ]]; then
  echo "Usage: ./scripts/update-formula.sh <version> <archive> <homebrew-tap-directory> [release-url]" >&2
  exit 2
fi

version="$1"
archive="$2"
tap_dir="$3"
archive_name="$(basename "${archive}")"
release_url="${4:-https://github.com/codex-remote/homebrew-tap/releases/download/v${version}/${archive_name}}"

if [[ ! -f "${archive}" ]]; then
  echo "Archive not found: ${archive}" >&2
  exit 1
fi
if [[ ! -d "${tap_dir}/Formula" ]]; then
  echo "Tap Formula directory not found: ${tap_dir}/Formula" >&2
  exit 1
fi

sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
formula="${tap_dir}/Formula/codex-remote.rb"
temporary="${formula}.tmp"
cat > "${temporary}" <<RUBY
class CodexRemote < Formula
  desc "Use a phone to control local Codex sessions over your LAN"
  homepage "https://github.com/codex-remote"
  url "${release_url}"
  version "${version}"
  sha256 "${sha256}"

  depends_on arch: :arm64
  depends_on :macos
  depends_on "postgresql@17"
  def install
    bin.install Dir["bin/*"]
    pkgshare.install "manifest.json", "THIRD_PARTY_NOTICES", "LICENSES"
    (pkgshare/"mobile-web").install Dir["share/mobile-web/*"]
  end

  def caveats
    <<~EOS
      Complete the per-user setup after installation:
        codex-remote setup --workspace-root /path/to/your/work

      Setup creates isolated PostgreSQL and Valkey data under:
        ~/Library/Application Support/CodexRemote

      Existing PostgreSQL and Valkey services are not modified. Ordinary
      brew upgrade, brew reinstall, and codex-remote uninstall preserve data.
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/codex-remote version")
  end
end
RUBY
mv "${temporary}" "${formula}"
echo "Updated ${formula}"
echo "Version: ${version}"
echo "SHA256:  ${sha256}"
echo "URL:     ${release_url}"
