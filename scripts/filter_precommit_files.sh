#!/usr/bin/env bash

set -euo pipefail

# Filters a list of filenames (from pre-commit) to exclude any paths that match
# directories listed (non-negated) in scripts/upstream_files.txt, and skips
# non-text files. Designed to be used as:
#   scripts/filter_precommit_files.sh <cmd> [cmd-args ...] -- <files ...>
# with pass_filenames: true so pre-commit appends filenames after '--'.

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)
REPO_ROOT=$(cd -- "${SCRIPT_DIR}/.." &>/dev/null && pwd)
UPSTREAM_FILE="${REPO_ROOT}/scripts/upstream_files.txt"

die() {
  echo "error: $*" >&2
  exit 2
}

require_upstream_file() {
  [[ -f "${UPSTREAM_FILE}" ]] || die "not found: ${UPSTREAM_FILE}"
}

build_dirs_regex() {
  # Build a regex like ^(core/|eth/|internal/|node/) from non-negated entries
  # in scripts/upstream_files.txt. Returns empty string if no entries.
  require_upstream_file
  local topdirs_list=()
  while IFS= read -r line; do
    # strip leading/trailing whitespace
    line="${line%%[[:space:]]*}"
    line="${line##[[:space:]]}"
    [[ -z "${line}" ]] && continue
    [[ "${line}" = \!* ]] && continue
    first_segment="${line%%/*}"
    [[ -z "${first_segment}" ]] && continue
    [[ "${first_segment}" = "." || "${first_segment}" = "*" ]] && continue
    topdirs_list+=("${first_segment}")
  done < "${UPSTREAM_FILE}"

  if [[ ${#topdirs_list[@]} -eq 0 ]]; then
    printf '%s' ""
    return 0
  fi

  local unique_topdirs
  unique_topdirs=$(printf '%s\n' "${topdirs_list[@]}" | sort -u)
  local parts=()
  while IFS= read -r d; do
    [[ -z "$d" ]] && continue
    parts+=("${d}/")
  done <<< "${unique_topdirs}"

  if [[ ${#parts[@]} -eq 0 ]]; then
    printf '%s' ""
    return 0
  fi

  local joined_parts
  joined_parts=$(printf '%s|' "${parts[@]}")
  joined_parts=${joined_parts%|}
  printf '^(%s)' "$joined_parts"
}

is_text_file() {
  # Return 0 if the given path appears to be a text file, 1 if binary.
  local f="$1"
  [[ -f "$f" ]] || return 1
  [[ ! -s "$f" ]] && return 0
  if command -v file >/dev/null 2>&1; then
    local mime
    mime=$(file -b --mime "$f" || true)
    case "$mime" in
      *charset=binary*) return 1 ;;
    esac
  fi
  if LC_ALL=C grep -Iq . "$f" 2>/dev/null; then
    return 0
  fi
  return 1
}

should_skip_path() {
  # Args: <path> <dirs_regex>
  local p="$1"
  local re="${2-}"
  # Skip directories
  [[ -d "$p" ]] && return 0
  # Skip obvious binaries by extension
  case "$p" in
    *.bin) return 0 ;;
  esac
  # Skip non-text
  is_text_file "$p" || return 0
  # Skip upstream directories if regex provided
  if [[ -n "$re" ]] && [[ "$p" =~ $re ]]; then
    return 0
  fi
  return 1
}

parse_args() {
  # Populate CMD[] and FILES[] from argv using '--' as delimiter
  CMD=()
  FILES=()
  local found_delim=false
  for arg in "$@"; do
    if ! $found_delim; then
      if [[ "$arg" == "--" ]]; then
        found_delim=true
        continue
      fi
      CMD+=("$arg")
    else
      FILES+=("$arg")
    fi
  done
  if [[ ${#CMD[@]} -eq 0 ]]; then
    die "usage: $0 <cmd> [args...] -- [files...]"
  fi
}

main() {
  parse_args "$@"
  local dirs_regex
  dirs_regex="$(build_dirs_regex)"

  # If no filenames provided, run the command as-is
  if [[ ${#FILES[@]} -eq 0 ]]; then
    exec "${CMD[@]}"
  fi

  local filtered=()
  for p in "${FILES[@]}"; do
    if should_skip_path "$p" "$dirs_regex"; then
      continue
    fi
    filtered+=("$p")
  done

  # Nothing left to process
  if [[ ${#filtered[@]} -eq 0 ]]; then
    exit 0
  fi

  exec "${CMD[@]}" "${filtered[@]}"
}

main "$@"
