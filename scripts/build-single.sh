#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_ROOT="${OUT_DIR:-${GATEWAY_ROOT}/dist}"
SINGLE_DIR="${DIST_ROOT}/single"

OS_INPUT="${OS_LIST:-host}"
ARCH_INPUT="${ARCH_LIST:-amd64,arm64}"
TAGS_INPUT="${TAGS:-}"

mkdir -p "${SINGLE_DIR}"

host_os() {
  local uname_out
  uname_out="$(uname -s)"
  case "${uname_out}" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) echo "linux" ;;
  esac
}

split_list() {
  local input="$1"
  local IFS=","
  read -r -a parts <<< "${input}"
  for item in "${parts[@]}"; do
    item="$(echo "${item}" | tr '[:upper:]' '[:lower:]' | xargs)"
    if [[ -n "${item}" ]]; then
      echo "${item}"
    fi
  done
}

HOST_OS="$(host_os)"
HOST_ARCH="$(go env GOARCH | tr '[:upper:]' '[:lower:]' | xargs)"

os_list=()
os_input="$(echo "${OS_INPUT}" | tr '[:upper:]' '[:lower:]' | xargs)"
case "${os_input}" in
  host) os_list=("${HOST_OS}") ;;
  all) os_list=("windows" "darwin" "linux") ;;
  *) mapfile -t os_list < <(split_list "${os_input}") ;;
esac

arch_list=()
arch_input="$(echo "${ARCH_INPUT}" | tr '[:upper:]' '[:lower:]' | xargs)"
if [[ "${arch_input}" == "all" ]]; then
  arch_list=("amd64" "arm64")
else
  mapfile -t arch_list < <(split_list "${arch_input}")
fi

for os_name in "${os_list[@]}"; do
  for arch_name in "${arch_list[@]}"; do
    if [[ -z "${os_name}" || -z "${arch_name}" ]]; then
      continue
    fi

    needs_cgo=0
    if [[ "${os_name}" != "windows" ]]; then
      needs_cgo=1
    fi

    is_cross_os=0
    if [[ "${os_name}" != "${HOST_OS}" ]]; then
      is_cross_os=1
    fi

    if [[ "${is_cross_os}" -eq 1 && "${needs_cgo}" -eq 1 ]]; then
      echo "Skip ${os_name}/${arch_name}: cgo cross-compile not supported on ${HOST_OS}"
      continue
    fi

    is_cross_arch=0
    if [[ "${arch_name}" != "${HOST_ARCH}" ]]; then
      is_cross_arch=1
    fi

    cc_key="CC_${os_name}_${arch_name}"
    cc_key="${cc_key^^}"
    cc_key="${cc_key//-/_}"
    cc_value="${!cc_key-}"

    if [[ "${os_name}" == "linux" && "${HOST_OS}" == "linux" && "${is_cross_arch}" -eq 1 && "${needs_cgo}" -eq 1 && -z "${cc_value}" && -z "${CC-}" ]]; then
      echo "Skip ${os_name}/${arch_name}: set ${cc_key} for cgo cross-compile"
      continue
    fi

    out_name="warp-gateway_${os_name}_${arch_name}"
    if [[ "${os_name}" == "windows" ]]; then
      out_name="${out_name}.exe"
    fi
    out_path="${SINGLE_DIR}/${out_name}"

    if [[ "${needs_cgo}" -eq 1 ]]; then
      export CGO_ENABLED=1
    else
      export CGO_ENABLED=0
    fi
    export GOOS="${os_name}"
    export GOARCH="${arch_name}"
    if [[ -n "${cc_value}" ]]; then
      export CC="${cc_value}"
    fi

    ldflags="-s -w"
    if [[ "${os_name}" == "windows" ]]; then
      ldflags="-s -w -H=windowsgui"
    fi

    pushd "${GATEWAY_ROOT}" >/dev/null
    if [[ -n "${TAGS_INPUT}" ]]; then
      go build -trimpath -ldflags "${ldflags}" -tags "${TAGS_INPUT}" -o "${out_path}" .
    else
      go build -trimpath -ldflags "${ldflags}" -o "${out_path}" .
    fi
    popd >/dev/null

    echo "Built ${out_path}"
  done
done

echo "Build complete: ${SINGLE_DIR}"
