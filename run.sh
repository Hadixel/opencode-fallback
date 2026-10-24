#!/bin/sh
# Install ocf if needed, then run it — one line:
#
#   curl -fsSL https://raw.githubusercontent.com/Hadixel/opencode-fallback/main/run.sh | sh -s -- "your prompt"
#
# A bare prompt is treated as `ocf run <prompt>`; explicit commands
# (run, check, version, help) are passed through unchanged.
#
# Environment: OCF_BIN, OCF_REPO, OCF_CHAIN (same as ocf).
set -eu

repo=${OCF_REPO:-https://github.com/Hadixel/opencode-fallback}
bin=${OCF_BIN:-${GOBIN:-$HOME/.local/bin}/ocf}

if [ ! -x "$bin" ]; then
	command -v curl >/dev/null 2>&1 || {
		echo "ocf: curl is required to bootstrap (or install manually with install.sh)" >&2
		exit 1
	}
	curl -fsSL "$repo/raw/main/install.sh" | OCF_REPO="$repo" OCF_BIN="$bin" sh
fi

case "${1:-}" in
run | check | version | help | -h | --help | -v | --version)
	exec "$bin" "$@"
	;;
"")
	exec "$bin" help
	;;
*)
	exec "$bin" run "$@"
	;;
esac
