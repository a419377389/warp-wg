#!/usr/bin/env bash
set -euo pipefail

BIN_PATH="${1:-}"
VERSION_INPUT="${2:-0.0.0}"
ARCH_INPUT="${3:-amd64}"
OUT_DIR="${4:-dist/packages}"

if [[ -z "${BIN_PATH}" || ! -f "${BIN_PATH}" ]]; then
  echo "binary not found: ${BIN_PATH}" >&2
  exit 1
fi

VERSION="$(echo "${VERSION_INPUT}" | tr -cd '0-9A-Za-z.+~:-')"
if [[ -z "${VERSION}" ]]; then
  VERSION="0.0.0"
fi

case "${ARCH_INPUT}" in
  amd64) DEB_ARCH="amd64"; RPM_ARCH="x86_64" ;;
  arm64) DEB_ARCH="arm64"; RPM_ARCH="aarch64" ;;
  *) DEB_ARCH="${ARCH_INPUT}"; RPM_ARCH="${ARCH_INPUT}" ;;
esac

PKG_NAME="warp-gateway"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

mkdir -p "${OUT_DIR}"

DEB_ROOT="${WORK_DIR}/deb"
mkdir -p "${DEB_ROOT}/DEBIAN" "${DEB_ROOT}/usr/local/bin"
install -m 0755 "${BIN_PATH}" "${DEB_ROOT}/usr/local/bin/${PKG_NAME}"

cat > "${DEB_ROOT}/DEBIAN/control" <<EOF
Package: ${PKG_NAME}
Version: ${VERSION}
Section: utils
Priority: optional
Architecture: ${DEB_ARCH}
Maintainer: Warp Gateway <support@example.com>
Depends: libayatana-appindicator3-1 | libappindicator3-1, libgtk-3-0
Description: Warp gateway tray service
EOF

dpkg-deb --build "${DEB_ROOT}" "${OUT_DIR}/${PKG_NAME}_${VERSION}_${DEB_ARCH}.deb"

RPM_ROOT="${WORK_DIR}/rpmbuild"
mkdir -p "${RPM_ROOT}"/{BUILD,BUILDROOT,RPMS,SOURCES,SPECS,SRPMS}
SRC_DIR="${WORK_DIR}/src/${PKG_NAME}-${VERSION}"
mkdir -p "${SRC_DIR}"
install -m 0755 "${BIN_PATH}" "${SRC_DIR}/${PKG_NAME}"
tar -C "$(dirname "${SRC_DIR}")" -czf "${RPM_ROOT}/SOURCES/${PKG_NAME}-${VERSION}.tar.gz" "$(basename "${SRC_DIR}")"

cat > "${RPM_ROOT}/SPECS/${PKG_NAME}.spec" <<EOF
Name: ${PKG_NAME}
Version: ${VERSION}
Release: 1%{?dist}
Summary: Warp gateway tray service
License: Proprietary
Source0: %{name}-%{version}.tar.gz
BuildArch: ${RPM_ARCH}
Requires: libayatana-appindicator3.so.1, libgtk-3.so.0

%description
Warp gateway tray service.

%prep
%setup -q

%install
mkdir -p %{buildroot}/usr/local/bin
install -m 0755 ${PKG_NAME} %{buildroot}/usr/local/bin/${PKG_NAME}

%files
/usr/local/bin/${PKG_NAME}

%changelog
* $(date +"%a %b %d %Y") Warp Gateway <support@example.com> - ${VERSION}-1
- Automated build.
EOF

rpmbuild -bb "${RPM_ROOT}/SPECS/${PKG_NAME}.spec" --define "_topdir ${RPM_ROOT}"

RPM_OUT="${RPM_ROOT}/RPMS/${RPM_ARCH}"
if [[ -d "${RPM_OUT}" ]]; then
  cp -f "${RPM_OUT}"/*.rpm "${OUT_DIR}/"
fi
