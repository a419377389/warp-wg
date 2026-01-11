#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${GATEWAY_ROOT}/.." && pwd)"
DIST="${GATEWAY_ROOT}/dist"
TARGET="${1:-host}"
ARCH="${ARCH:-amd64}"
INCLUDE_GATEWAY="${INCLUDE_GATEWAY:-1}"

mkdir -p "${DIST}"

get_host_target() {
  local uname_out
  uname_out="$(uname -s)"
  case "${uname_out}" in
    Darwin) echo "darwin/${ARCH}" ;;
    *) echo "linux/${ARCH}" ;;
  esac
}

targets=()
case "${TARGET}" in
  host)
    targets=("$(get_host_target)")
    ;;
  all)
    targets=("windows/${ARCH}" "darwin/${ARCH}" "linux/${ARCH}")
    ;;
  *)
    targets=("${TARGET}/${ARCH}")
    ;;
esac

for target in "${targets[@]}"; do
  os_name="${target%%/*}"
  arch_name="${target##*/}"
  target_dir="${DIST}/${os_name}-${arch_name}"
  mkdir -p "${target_dir}"

  bin="warp-gateway"
  if [[ "${os_name}" == "windows" ]]; then
    bin="${bin}.exe"
  fi

  ldflags="-s -w"
  if [[ "${os_name}" == "windows" ]]; then
    ldflags="-s -w -H=windowsgui"
  fi

  pushd "${GATEWAY_ROOT}" >/dev/null
  CGO_ENABLED=1 GOOS="${os_name}" GOARCH="${arch_name}" \
    go build -trimpath -ldflags "${ldflags}" -o "${target_dir}/${bin}" .
  popd >/dev/null

  rm -rf "${target_dir}/web" "${target_dir}/assets"
  cp -R "${GATEWAY_ROOT}/web" "${target_dir}/web"
  cp -R "${GATEWAY_ROOT}/assets" "${target_dir}/assets"

  if [[ "${INCLUDE_GATEWAY}" == "1" ]]; then
    rm -rf "${target_dir}/backend" "${target_dir}/resources"
    cp -R "${REPO_ROOT}/backend" "${target_dir}/backend"
    cp -R "${REPO_ROOT}/resources" "${target_dir}/resources"
  fi

  mkdir -p "${target_dir}/data" "${target_dir}/logs"
done

echo "Build complete: ${DIST}"
