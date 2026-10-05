#!/usr/bin/env bash
# Shared helper: obtain the usbridge fork of Sunshine and stage it next to the
# agent binary. Sunshine is the Moonlight GameStream host — it owns all
# video/audio/input capture for the Moonlight/Sunshine protocol; usbridge_agent
# pairs with it and relays PINs.
#
# We use our own fork (the sunshine/ directory of
# USBridge-Technologies/Streamers-Forks; it used to be itsme228/Sunshine)
# instead of the upstream LizardByte release because it adds web_bind_address
# -- a config key that lets the HTTPS admin UI bind to a separate address
# (127.0.0.1) while streaming ports stay bound to the VPN/LAN interface via
# bind_address -- and hands the client's USB devices to the USB broker (see
# agent/internal/streamhost/usb_broker_bridge.go).
#
# macOS and Linux are built from source (the fork's master branch).
# Windows still uses a prebuilt release from upstream (the feature is not
# strictly needed on Windows where Moonlight normally connects over LAN).
#
# Env overrides:
#   USBRIDGE_SKIP_SUNSHINE=1     skip bundling Sunshine entirely (offline/dev builds)
#   USBRIDGE_SUNSHINE_FORCE=1    rebuild/re-download even if already staged
#   USBRIDGE_SUNSHINE_VERSION=x  pin a release tag instead of "latest"
#   USBRIDGE_SUNSHINE_CUDA=1     (Linux only, source build) build with Nvidia CUDA/NVENC support
#                                (fork releases are already built with CUDA in CI)
#   USBRIDGE_SUNSHINE_JOBS=n     parallel jobs for a source build (default: MemAvailable/2GB)

_sunshine_repo="USBridge-Technologies/Streamers-Forks"
# Where Sunshine sits inside that repository (a source build needs it).
_sunshine_subdir="sunshine"
# Self-resolved rather than relying on a caller-provided SCRIPT_DIR — this
# file is sourced from build_macos.sh/build_linux.sh/build_windows.*, and
# _sunshine_verify_download below needs an absolute path to
# verify_release_manifest.go regardless of which of those sourced it.
_sunshine_file_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

_sunshine_require() {
    if ! command -v "$1" >/dev/null 2>&1; then
        echo -e "${RED}Missing dependency: $1 (needed to fetch Sunshine)${NC}"
        echo "$2"
        exit 1
    fi
}

# _sunshine_clean_creds <dir>
# Removes Sunshine credential/state files that may have been written by a
# previous run of Sunshine from the dist directory. These files contain device
# pairing keys and admin password hashes that must never be shipped in a
# distribution archive.
_sunshine_clean_creds() {
    local dir="$1"
    for _f in sunshine_state.json credentials.json creds.json; do
        if [[ -f "$dir/$_f" ]]; then
            rm -f "$dir/$_f"
            echo -e "  ${YELLOW}Removed Sunshine credential file: $_f${NC}"
        fi
    done
}

# GitHub's unauthenticated API rate limit (60/hour/IP) is easy to exhaust on
# shared Actions runner IP ranges, especially with several jobs querying it
# in the same workflow run. Authenticate with GITHUB_TOKEN/GH_TOKEN (already
# available in every Actions job) when present to get the 5000/hour limit
# instead — a silent rate-limit failure here looks identical to "no release
# asset yet" and falls through to a 10-60 min source build.
_sunshine_curl_auth=()
if [[ -n "${GITHUB_TOKEN:-${GH_TOKEN:-}}" ]]; then
    _sunshine_curl_auth=(-H "Authorization: Bearer ${GITHUB_TOKEN:-$GH_TOKEN}")
fi

# _sunshine_releases_json
# Fetches the repo's full releases list once per process and caches it — both
# _sunshine_asset_url and _sunshine_resolve_tag need it when scanning for a
# release by asset rather than by GitHub's own "latest" pointer.
_sunshine_releases_json() {
    if [[ -z "${_sunshine_releases_cache:-}" ]]; then
        _sunshine_releases_cache="$(curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" "https://api.github.com/repos/${_sunshine_repo}/releases" 2>/dev/null || true)"
        [[ -z "$_sunshine_releases_cache" ]] && _sunshine_releases_cache="[]"
    fi
    echo "$_sunshine_releases_cache"
}

# _sunshine_find_release <asset_name>
# This repo's releases aren't exclusively Sunshine builds — other components
# (e.g. punktfunk-host) publish their own releases here too, and one of those
# can be newer than our last Sunshine release, which makes GitHub's own
# /releases/latest pointer resolve to a release with no Sunshine assets at
# all. So "latest" here means "newest non-draft, non-prerelease release that
# actually carries this asset", found by scanning the list ourselves instead
# of trusting the API's latest pointer. Prints
# "<tag_name>\t<download_url>\t<manifest_url>\t<sig_url>" — the last two are
# empty for a release published before release-all.yml started signing a
# manifest.json/.sig alongside its assets (see _sunshine_verify_download).
_sunshine_find_release() {
    local asset_name="$1"
    _sunshine_releases_json | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
except Exception:
    sys.exit(0)
for rel in data:
    if rel.get('draft') or rel.get('prerelease'):
        continue
    by_name = {a['name']: a['browser_download_url'] for a in rel.get('assets', [])}
    if '${asset_name}' in by_name:
        print('\t'.join([
            rel['tag_name'],
            by_name['${asset_name}'],
            by_name.get('manifest.json', ''),
            by_name.get('manifest.json.sig', ''),
        ]))
        sys.exit(0)
" 2>/dev/null || true
}

# _sunshine_verify_download <file> <asset_name> <manifest_url> <sig_url>
# Checks file's SHA-256 against the Ed25519-signed manifest.json published
# alongside the release asset_name came from, via
# verify_release_manifest.go (AGENT_UPDATE_ED25519_PRIVATE_KEY's public
# half — the agent's own update-signing key, reused here rather than a new
# one since the agent already embeds and trusts it, see that script's doc
# comment). Closes the gap plain HTTPS-to-GitHub leaves open: a compromised
# GITHUB_TOKEN or CI run could otherwise edit a release's assets after the
# fact and this function's caller would have no way to tell.
#
# manifest_url/sig_url are empty for a release published before
# release-all.yml started signing manifests — that's a warning, not a
# failure, so older or individually re-dispatched per-fork releases keep
# working. A manifest that IS present but doesn't verify, or disagrees with
# the downloaded bytes, is always a hard failure — never silently ignored.
_sunshine_verify_download() {
    local file="$1" asset_name="$2" manifest_url="$3" sig_url="$4"
    if [[ -z "$manifest_url" || -z "$sig_url" ]]; then
        echo -e "${YELLOW}⚠${NC}  release has no signed manifest (pre-dates release-all.yml) — skipping integrity check for $asset_name"
        return 0
    fi
    _sunshine_require go "Install Go to verify the signed release manifest: https://go.dev/dl/"

    local tmp_manifest tmp_sig
    tmp_manifest="$(mktemp)"
    tmp_sig="$(mktemp)"
    if ! curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" -o "$tmp_manifest" "$manifest_url" \
        || ! curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" -o "$tmp_sig" "$sig_url"; then
        rm -f "$tmp_manifest" "$tmp_sig"
        echo -e "${RED}✗${NC} failed to download signed manifest for $asset_name"
        return 1
    fi

    if go run "$_sunshine_file_dir/verify_release_manifest.go" \
        -manifest "$tmp_manifest" -sig "$tmp_sig" -asset "$asset_name" -file "$file"; then
        rm -f "$tmp_manifest" "$tmp_sig"
        return 0
    fi
    rm -f "$tmp_manifest" "$tmp_sig"
    echo -e "${RED}✗${NC} signed manifest verification FAILED for $asset_name — refusing to use this download"
    return 1
}

# _sunshine_release_info <asset_name>
# Prints "<tag>\t<asset_url>\t<manifest_url>\t<sig_url>" for whichever
# release would be fetched right now (USBRIDGE_SUNSHINE_VERSION pinned, or
# the newest non-draft/prerelease release with asset_name — see
# _sunshine_find_release). manifest_url/sig_url are empty when that release
# predates release-all.yml's signed manifest.
_sunshine_release_info() {
    local asset_name="$1"
    local version="${USBRIDGE_SUNSHINE_VERSION:-latest}"
    if [[ "$version" != "latest" ]]; then
        # Pinned version: the user named an exact tag, so look at that
        # release's assets directly rather than scanning the whole list.
        curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" \
            "https://api.github.com/repos/${_sunshine_repo}/releases/tags/${version}" 2>/dev/null | python3 -c "
import sys, json
try:
    data = json.load(sys.stdin)
    by_name = {a['name']: a['browser_download_url'] for a in data.get('assets', [])}
    if '${asset_name}' in by_name:
        print('\t'.join([
            data.get('tag_name', '${version}'),
            by_name['${asset_name}'],
            by_name.get('manifest.json', ''),
            by_name.get('manifest.json.sig', ''),
        ]))
except Exception:
    pass
" 2>/dev/null || true
        return 0
    fi
    _sunshine_find_release "$asset_name"
}

_sunshine_asset_url() { _sunshine_release_info "$1" | cut -f2; }

# _sunshine_build_jobs
# Parallel job count for a Sunshine source build. Its C++ TUs (boost, nvcc'd
# cuda.cu) peak at ~1.5-2 GB each, so -j$(nproc) on e.g. 20 threads / 8 GB RAM
# OOMs the whole desktop. Cap at MemAvailable/2GB (min 1), overridable with
# USBRIDGE_SUNSHINE_JOBS.
_sunshine_build_jobs() {
    if [[ -n "${USBRIDGE_SUNSHINE_JOBS:-}" ]]; then
        echo "$USBRIDGE_SUNSHINE_JOBS"
        return 0
    fi
    local cpus mem_kb jobs
    cpus="$(nproc 2>/dev/null || sysctl -n hw.ncpu)"
    if [[ -r /proc/meminfo ]]; then
        mem_kb="$(awk '/^MemAvailable:/ {print $2}' /proc/meminfo)"
    else
        mem_kb="$(( $(sysctl -n hw.memsize) / 1024 ))"
    fi
    jobs=$(( mem_kb / (2 * 1024 * 1024) ))
    (( jobs < 1 )) && jobs=1
    (( jobs > cpus )) && jobs=$cpus
    echo "$jobs"
}

# _sunshine_resolve_tag [asset_name]
# Tag of the release that would be fetched right now. With asset_name, this
# is the tag _sunshine_release_info would pick (so cache comparisons track
# the release that actually has our asset, not whatever release GitHub
# happens to call "latest" this week). Without it, falls back to GitHub's own
# latest pointer.
_sunshine_resolve_tag() {
    local asset_name="${1:-}"
    local version="${USBRIDGE_SUNSHINE_VERSION:-}"
    if [[ -n "$version" ]]; then
        echo "$version"
        return 0
    fi
    if [[ -n "$asset_name" ]]; then
        _sunshine_release_info "$asset_name" | cut -f1
        return 0
    fi
    local py
    py="$(command -v python3 || command -v python)" || return 1
    curl -fsSL "${_sunshine_curl_auth[@]+"${_sunshine_curl_auth[@]}"}" "https://api.github.com/repos/${_sunshine_repo}/releases/latest" \
        | "$py" -c "import sys, json; print(json.load(sys.stdin)['tag_name'])"
}

# The release tag a staged dest came from is recorded outside dest itself
# (dest ends up inside the AppImage / signed .app bundle), keyed by its path.
_sunshine_tag_file() {
    local key
    key="$(cd "$(dirname "$1")" 2>/dev/null && pwd)/$(basename "$1")"
    echo "${XDG_CACHE_HOME:-$HOME/.cache}/usbridge-sunshine/$(echo "$key" | tr '/:\\ ' '____').tag"
}

_sunshine_record_tag() {
    local tag="$1" file
    [[ -n "$tag" ]] || return 0
    file="$(_sunshine_tag_file "$2")"
    mkdir -p "$(dirname "$file")" && echo "$tag" > "$file"
}

# _sunshine_staged_is_current <dest> <staged_path> <asset_name>
# True when staged_path exists and came from the release that would be
# fetched now, so a local rebuild picks up a new fork release instead of
# reusing whatever an earlier build staged. When the wanted tag can't be
# resolved (offline, rate-limited) an existing stage is kept.
_sunshine_staged_is_current() {
    local dest="$1" staged="$2" asset_name="$3" want have
    [[ -e "$staged" && "${USBRIDGE_SUNSHINE_FORCE:-0}" != "1" ]] || return 1
    want="$(_sunshine_resolve_tag "$asset_name" 2>/dev/null || true)"
    [[ -z "$want" ]] && return 0
    have="$(cat "$(_sunshine_tag_file "$dest")" 2>/dev/null || true)"
    if [[ "$want" != "$have" ]]; then
        echo -e "${YELLOW}Staged Sunshine is ${have:-unknown}, fork release is $want — re-fetching${NC}"
        return 1
    fi
}

# fetch_sunshine_linux / build_sunshine_linux <dest_dir>
# Stages the Sunshine fork (Streamers-Forks, sunshine/) under
# $dest_dir as a cmake install tree: $dest_dir/usr/bin/sunshine and
# $dest_dir/usr/local/assets/. Built with SUNSHINE_BUILD_APPIMAGE=ON so
# the binary uses relative asset paths (./usr/local/assets relative to cwd),
# making it relocatable and compatible with being bundled in the agent AppImage.
# No system-wide install — the agent binary is responsible for launching it.
# Fast path: download pre-built tarball from fork's GitHub Releases.
# Slow path (fallback): clone fork and build from source with cmake.
fetch_sunshine_linux() { build_sunshine_linux "$@"; }
build_sunshine_linux() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    local arch
    arch="$(uname -m)"
    local asset_name="Sunshine-Linux-x86_64.tar.gz"
    [[ "$arch" == "aarch64" ]] && asset_name="Sunshine-Linux-aarch64.tar.gz"

    if _sunshine_staged_is_current "$dest" "$dest/usr/bin/sunshine" "$asset_name"; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping"
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install with: sudo apt install curl"
    _sunshine_require python3 "Install with: sudo apt install python3"

    # Fast path: download pre-built tarball from our fork's releases.
    echo -e "${YELLOW}Fetching Sunshine fork (Streamers-Forks)...${NC}"
    local tag url manifest_url sig_url
    IFS=$'\t' read -r tag url manifest_url sig_url <<< "$(_sunshine_release_info "$asset_name")"

    if [[ -n "$url" ]]; then
        local tmp_tgz
        tmp_tgz="$(mktemp).tar.gz"
        echo "Downloading: $url"
        curl -fL --progress-bar -o "$tmp_tgz" "$url"
        if ! _sunshine_verify_download "$tmp_tgz" "$asset_name" "$manifest_url" "$sig_url"; then
            rm -f "$tmp_tgz"
            exit 1
        fi
        rm -rf "$dest"
        mkdir -p "$dest"
        tar -xzf "$tmp_tgz" -C "$dest"
        rm -f "$tmp_tgz"
        chmod +x "$dest/usr/bin/sunshine" 2>/dev/null || true
        _sunshine_clean_creds "$dest"
        _sunshine_record_tag "$tag" "$dest"
        echo -e "${GREEN}✓${NC} Sunshine (fork release) staged at $dest"
        return 0
    fi

    # Slow path: no release yet — build from source.
    echo -e "${YELLOW}No fork release found — building Sunshine from source (15-60+ min)...${NC}"
    _sunshine_require git "Install with: sudo apt install git"
    _sunshine_require cmake "Install with: sudo apt install cmake"
    _sunshine_require sudo "cmake install needs sudo for build deps"

    local repo_dir src_dir
    repo_dir="$(mktemp -d)"
    git clone --depth 1 --recurse-submodules --shallow-submodules \
        "https://github.com/${_sunshine_repo}.git" "$repo_dir"
    src_dir="$repo_dir/$_sunshine_subdir"

    local cuda_flag="OFF"
    [[ "${USBRIDGE_SUNSHINE_CUDA:-0}" == "1" ]] && cuda_flag="ON"

    cmake \
        -B "$src_dir/build" -S "$src_dir" \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_DOCS=OFF \
        -DSUNSHINE_BUILD_APPIMAGE=ON \
        -DSUNSHINE_ENABLE_TRAY=OFF \
        -DSUNSHINE_ENABLE_CUDA="$cuda_flag" \
        -DSUNSHINE_PUBLISHER_NAME="usbridge_agent" \
        -DSUNSHINE_PUBLISHER_WEBSITE="https://github.com/itsme228/usbridge_agent" \
        -DSUNSHINE_PUBLISHER_ISSUE_URL="https://github.com/itsme228/usbridge_agent/issues"

    cmake --build "$src_dir/build" -j "$(_sunshine_build_jobs)"

    rm -rf "$dest"
    DESTDIR="$dest" cmake --install "$src_dir/build" --prefix /usr
    rm -rf "$repo_dir"
    chmod +x "$dest/usr/bin/sunshine" 2>/dev/null || true

    _sunshine_clean_creds "$dest"
    echo -e "${GREEN}✓${NC} Sunshine (fork source build) staged at $dest"
}

# fetch_sunshine_windows <dest_dir>
fetch_sunshine_windows() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    local asset_name="Sunshine-Windows-x86_64-portable.zip"

    if _sunshine_staged_is_current "$dest" "$dest/sunshine.exe" "$asset_name"; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping download"
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install with: pacman -S --needed curl"
    _sunshine_require python "Install with: pacman -S --needed mingw-w64-ucrt-x86_64-python"

    echo -e "${YELLOW}Fetching Sunshine fork (Streamers-Forks)...${NC}"
    local tag url manifest_url sig_url
    IFS=$'\t' read -r tag url manifest_url sig_url <<< "$(_sunshine_release_info "$asset_name")"
    if [[ -z "$url" ]]; then
        echo -e "${RED}Failed to resolve Sunshine Windows download URL${NC}"
        exit 1
    fi

    local tmp_zip
    tmp_zip="$(mktemp).zip"
    echo "Downloading: $url"
    curl -fL --progress-bar -o "$tmp_zip" "$url"
    if ! _sunshine_verify_download "$tmp_zip" "$asset_name" "$manifest_url" "$sig_url"; then
        rm -f "$tmp_zip"
        exit 1
    fi

    rm -rf "$dest"
    mkdir -p "$dest"
    python -m zipfile -e "$tmp_zip" "$dest"
    rm -f "$tmp_zip"

    # The portable zip places everything under a top-level Sunshine/ subdirectory.
    # Flatten it so sunshine.exe lives directly under $dest (matching BinaryPath in Go).
    if [[ -f "$dest/Sunshine/sunshine.exe" ]]; then
        mv "$dest/Sunshine/"* "$dest/"
        rmdir "$dest/Sunshine" 2>/dev/null || true
    fi

    _sunshine_clean_creds "$dest"
    _sunshine_record_tag "$tag" "$dest"
    echo -e "${GREEN}✓${NC} Sunshine staged at $dest"
}

# fetch_sunshine_macos / build_sunshine_macos <dest_dir>
# Stages the Sunshine fork (Streamers-Forks, sunshine/).
# Fast path: download pre-built DMG from fork's GitHub Releases.
# Slow path (fallback): clone fork and build from source via CMake.
fetch_sunshine_macos() { build_sunshine_macos "$@"; }
build_sunshine_macos() {
    local dest="$1"
    if [[ "${USBRIDGE_SKIP_SUNSHINE:-0}" == "1" ]]; then
        echo -e "${YELLOW}USBRIDGE_SKIP_SUNSHINE=1 — skipping Sunshine bundling${NC}"
        return 0
    fi
    local arch
    arch="$(uname -m)"
    local asset_name="Sunshine-macOS-arm64.dmg"
    [[ "$arch" != "arm64" ]] && asset_name="Sunshine-macOS-x86_64.dmg"

    if _sunshine_staged_is_current "$dest" "$dest/Sunshine.app" "$asset_name"; then
        echo -e "${GREEN}✓${NC} Sunshine already staged at $dest, skipping"
        _sunshine_clean_creds "$dest"
        return 0
    fi

    _sunshine_require curl "Install Xcode Command Line Tools: xcode-select --install"
    _sunshine_require python3 "Install Xcode Command Line Tools: xcode-select --install"

    # Fast path: download pre-built DMG from our fork's releases.
    echo -e "${YELLOW}Fetching Sunshine fork (Streamers-Forks)...${NC}"
    local tag url manifest_url sig_url
    IFS=$'\t' read -r tag url manifest_url sig_url <<< "$(_sunshine_release_info "$asset_name")"

    if [[ -n "$url" ]]; then
        local tmp_dmg
        tmp_dmg="$(mktemp).dmg"
        echo "Downloading: $url"
        curl -fL --progress-bar -o "$tmp_dmg" "$url"
        if ! _sunshine_verify_download "$tmp_dmg" "$asset_name" "$manifest_url" "$sig_url"; then
            rm -f "$tmp_dmg"
            exit 1
        fi

        local mount_point
        mount_point="$(mktemp -d)"
        echo "y" | hdiutil attach -nobrowse -mountpoint "$mount_point" "$tmp_dmg" >/dev/null

        rm -rf "$dest"
        mkdir -p "$dest"
        cp -R "$mount_point"/*.app "$dest/Sunshine.app"

        hdiutil detach -quiet "$mount_point"
        rm -f "$tmp_dmg"

        xattr -dr com.apple.quarantine "$dest/Sunshine.app" 2>/dev/null || true
        _sunshine_clean_creds "$dest"
        _sunshine_record_tag "$tag" "$dest"
        echo -e "${GREEN}✓${NC} Sunshine (fork release) staged at $dest/Sunshine.app"
        return 0
    fi

    # Slow path: no release yet — build from source.
    echo -e "${YELLOW}No fork release found — building Sunshine from source (10-30 min)...${NC}"
    _sunshine_require cmake "Install with: brew install cmake"
    _sunshine_require git "Install Xcode Command Line Tools: xcode-select --install"
    _sunshine_require brew "Install Homebrew: https://brew.sh"
    _sunshine_require node "Install with: brew install node"

    brew install --quiet cmake node pkgconf "icu4c@78" miniupnpc "openssl@3" opus \
        2>&1 | grep -v "already installed" || true

    local repo_dir src_dir
    repo_dir="$(mktemp -d)"
    git clone --depth 1 --recurse-submodules --shallow-submodules \
        "https://github.com/${_sunshine_repo}.git" "$repo_dir"
    src_dir="$repo_dir/$_sunshine_subdir"

    cmake \
        -B "$src_dir/build" -S "$src_dir" \
        -DCMAKE_BUILD_TYPE=Release \
        -DBUILD_DOCS=OFF \
        -DICU_ROOT="$(brew --prefix "icu4c@78" 2>/dev/null)" \
        -DOPENSSL_ROOT_DIR="$(brew --prefix "openssl@3" 2>/dev/null)" \
        -DOpus_ROOT_DIR="$(brew --prefix opus 2>/dev/null)" \
        -DSUNSHINE_PUBLISHER_NAME="usbridge_agent" \
        -DSUNSHINE_PUBLISHER_WEBSITE="https://github.com/itsme228/usbridge_agent" \
        -DSUNSHINE_PUBLISHER_ISSUE_URL="https://github.com/itsme228/usbridge_agent/issues"

    cmake --build "$src_dir/build" -j "$(_sunshine_build_jobs)"
    (cd "$src_dir/build" && cpack -G DragNDrop --config CPackConfig.cmake)

    local dmg_file
    dmg_file="$(find "$src_dir/build" -name "*.dmg" | head -1)"
    if [[ -z "$dmg_file" ]]; then
        rm -rf "$repo_dir"
        echo -e "${RED}Sunshine source build failed — no .dmg produced${NC}"
        exit 1
    fi

    local mount_point
    mount_point="$(mktemp -d)"
    echo "y" | hdiutil attach -nobrowse -mountpoint "$mount_point" "$dmg_file" >/dev/null

    rm -rf "$dest"
    mkdir -p "$dest"
    cp -R "$mount_point"/*.app "$dest/Sunshine.app"

    hdiutil detach -quiet "$mount_point"
    rm -rf "$repo_dir"

    xattr -dr com.apple.quarantine "$dest/Sunshine.app" 2>/dev/null || true
    _sunshine_clean_creds "$dest"
    echo -e "${GREEN}✓${NC} Sunshine (fork source build) staged at $dest/Sunshine.app"
}
