#!/usr/bin/env sh
# Copyright 2026 Cisco Systems, Inc. and its affiliates
#
# SPDX-License-Identifier: Apache-2.0
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#	http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
set -eu

REPO="thousandeyes/thousandeyes-cli"
BINARY_NAME="thousandeyes"

log() {
  printf '%s\n' "$*" >&2
}

fail() {
  log "Error: $*"
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "Missing required command: $1"
}

detect_os() {
  os="$(uname -s)"
  case "$os" in
    Linux)
      printf 'Linux\n'
      ;;
    Darwin)
      printf 'Darwin\n'
      ;;
    *)
      fail "Unsupported OS: $os"
      ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)
      printf 'x86_64\n'
      ;;
    arm64|aarch64)
      printf 'arm64\n'
      ;;
    *)
      fail "Unsupported architecture: $arch"
      ;;
  esac
}

sha256_file() {
  file="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  fail "Need either shasum or sha256sum for checksum verification"
}

resolve_version() {
  if [ -n "${TE_VERSION:-}" ]; then
    printf '%s\n' "${TE_VERSION}"
    return
  fi

  latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")" || fail "Unable to resolve latest release"
  version="$(basename "$latest_url")"
  [ -n "$version" ] || fail "Failed to parse latest release version"
  printf '%s\n' "$version"
}

pick_install_dir() {
  if [ -n "${INSTALL_DIR:-}" ]; then
    printf '%s\n' "${INSTALL_DIR}"
    return
  fi

  if [ -w "/usr/local/bin" ]; then
    printf '/usr/local/bin\n'
    return
  fi

  printf '%s\n' "${HOME}/.local/bin"
}

install_binary() {
  src="$1"
  dst_dir="$2"
  dst="${dst_dir}/${BINARY_NAME}"

  mkdir -p "$dst_dir"

  if [ -w "$dst_dir" ]; then
    install -m 0755 "$src" "$dst"
    return
  fi

  if [ -z "${INSTALL_DIR:-}" ] && [ "$dst_dir" = "/usr/local/bin" ] && command -v sudo >/dev/null 2>&1; then
    sudo install -m 0755 "$src" "$dst"
    return
  fi

  fail "Cannot write to ${dst_dir}. Re-run with INSTALL_DIR set to a writable directory."
}

verify_installed_version() {
  bin_path="$1"
  expected_version="$2"

  version_output="$("$bin_path" --version 2>/dev/null || true)"
  case "$version_output" in
    *"$expected_version"*) ;;
    *)
      fail "Installed binary version check failed. Expected ${expected_version}, got: ${version_output}"
      ;;
  esac
}

detect_shell() {
  shell_path="${SHELL:-}"
  [ -n "$shell_path" ] || return 1
  shell_name="$(basename "$shell_path")"
  case "$shell_name" in
    bash|zsh|fish|powershell|pwsh)
      printf '%s\n' "$shell_name"
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

print_completion_instructions() {
  shell_name="$1"

  case "$shell_name" in
    pwsh)
      completion_shell="powershell"
      ;;
    *)
      completion_shell="$shell_name"
      ;;
  esac

  log "To enable shell completion, run one of the following:"
  case "$completion_shell" in
    bash)
      log "  source <(${BINARY_NAME} completion bash)"
      log "  # or add to ~/.bashrc:"
      log "  echo 'source <(${BINARY_NAME} completion bash)' >> ~/.bashrc"
      ;;
    zsh)
      log "  # If zsh completion is not already enabled:"
      log "  autoload -Uz compinit && compinit"
      log "  source <(${BINARY_NAME} completion zsh)"
      log "  # or add to ~/.zshrc (after compinit):"
      log "  echo 'source <(${BINARY_NAME} completion zsh)' >> ~/.zshrc"
      ;;
    fish)
      log "  mkdir -p ~/.config/fish/completions"
      log "  ${BINARY_NAME} completion fish > ~/.config/fish/completions/${BINARY_NAME}.fish"
      ;;
    powershell)
      log "  ${BINARY_NAME} completion powershell | Out-String | Invoke-Expression"
      log "  # or add to \$PROFILE:"
      log "  Add-Content \$PROFILE '${BINARY_NAME} completion powershell | Out-String | Invoke-Expression'"
      ;;
  esac
}

prune_other_path_binaries() {
  installed_path="$1"
  old_ifs="$IFS"
  IFS=":"
  for dir in $PATH; do
    [ -n "$dir" ] || continue
    candidate="${dir}/${BINARY_NAME}"
    [ "$candidate" = "$installed_path" ] && continue
    [ -f "$candidate" ] || continue

    if [ -w "$candidate" ]; then
      rm -f "$candidate"
      log "Removed older ${BINARY_NAME} from ${candidate}"
      continue
    fi

    if [ -w "$dir" ]; then
      rm -f "$candidate"
      log "Removed older ${BINARY_NAME} from ${candidate}"
      continue
    fi

    log "Warning: found another ${BINARY_NAME} at ${candidate} (not writable, leaving as-is)"
  done
  IFS="$old_ifs"
}

main() {
  require_cmd curl
  require_cmd tar
  require_cmd awk
  require_cmd basename
  require_cmd install

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(resolve_version)"
  install_dir="$(pick_install_dir)"

  archive_name="${BINARY_NAME}_${version}_${os}_${arch}.tar.gz"
  checksums_name="${BINARY_NAME}_${version}_checksums.txt"
  base_url="https://github.com/${REPO}/releases/download/${version}"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT INT TERM

  archive_path="${tmpdir}/${archive_name}"
  checksums_path="${tmpdir}/${checksums_name}"

  log "Downloading ${archive_name} (${version})..."
  curl -fsSL "${base_url}/${archive_name}" -o "$archive_path" || fail "Failed to download archive"
  curl -fsSL "${base_url}/${checksums_name}" -o "$checksums_path" || fail "Failed to download checksums"

  expected_sum="$(awk -v file="$archive_name" '$2 == file {print $1}' "$checksums_path")"
  [ -n "$expected_sum" ] || fail "Could not find checksum for ${archive_name}"

  actual_sum="$(sha256_file "$archive_path")"
  [ "$expected_sum" = "$actual_sum" ] || fail "Checksum mismatch for ${archive_name}"

  tar -xzf "$archive_path" -C "$tmpdir" || fail "Failed to extract archive"
  [ -f "${tmpdir}/${BINARY_NAME}" ] || fail "Binary ${BINARY_NAME} not found in archive"

  install_binary "${tmpdir}/${BINARY_NAME}" "$install_dir"
  installed_path="${install_dir}/${BINARY_NAME}"
  verify_installed_version "$installed_path" "$version"
  prune_other_path_binaries "$installed_path"
  log "Installed ${BINARY_NAME} ${version} to ${installed_path}"
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *)
      log "Note: ${install_dir} is not on PATH in this shell."
      log "Add it with: export PATH=\"${install_dir}:\$PATH\""
      ;;
  esac

  if shell_name="$(detect_shell)"; then
    print_completion_instructions "$shell_name"
  else
    log "To enable shell completion, see:"
    log "  https://github.com/${REPO}#shell-completion"
  fi
}

main "$@"
