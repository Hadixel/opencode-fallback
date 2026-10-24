#!/bin/sh
# Install ocf from source.
#
#   curl -fsSL https://raw.githubusercontent.com/Hadixel/opencode-fallback/main/install.sh | sh
#
# Environment:
#   OCF_BIN   install path (default: ${GOBIN:-$HOME/.local/bin}/ocf)
#   OCF_REPO  source repository (default: the public GitHub repo)
set -eu

repo=${OCF_REPO:-https://github.com/Hadixel/opencode-fallback}
bin=${OCF_BIN:-${GOBIN:-$HOME/.local/bin}/ocf}

need() {
	command -v "$1" >/dev/null 2>&1 || {
		echo "ocf: $1 is required" >&2
		exit 1
	}
}
need git
need go

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

if ! git clone --depth 1 "$repo" "$tmp/src" >/dev/null 2>&1; then
	echo "ocf: could not clone $repo" >&2
	exit 1
fi

mkdir -p "$(dirname "$bin")"
(cd "$tmp/src" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$bin" .)

echo "installed $bin"
case ":$PATH:" in
*":$(dirname "$bin"):"*) ;;
*) echo "note: $(dirname "$bin") is not on PATH" ;;
esac
"$bin" version
