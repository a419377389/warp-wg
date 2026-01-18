# 证书安装指南

本项目使用 MITM 代理，需要在系统中安装并信任根证书。首次运行程序后会自动生成证书文件 `~/.mitmproxy/mitmproxy-ca-cert.pem`。

## Arch Linux

```bash
# 安装依赖
sudo pacman -S --needed nss ca-certificates-utils

# 安装系统 CA
sudo cp ~/.mitmproxy/mitmproxy-ca-cert.pem /etc/ca-certificates/trust-source/anchors/mitmproxy-ca-cert.crt
sudo update-ca-trust
```

## Ubuntu / Debian

```bash
# 安装依赖
sudo apt install -y libnss3-tools ca-certificates

# 安装系统 CA
sudo cp ~/.mitmproxy/mitmproxy-ca-cert.pem /usr/local/share/ca-certificates/mitmproxy-ca-cert.crt
sudo update-ca-certificates
```

## 浏览器证书（可选）

如果需要通过浏览器使用代理，还需安装 NSS 证书（Chrome/Chromium/Firefox 等使用）：

```bash
mkdir -p ~/.pki/nssdb
certutil -d sql:$HOME/.pki/nssdb -N --empty-password 2>/dev/null
certutil -d sql:$HOME/.pki/nssdb -A -t "CT,C,C" -n "mitmproxy" -i ~/.mitmproxy/mitmproxy-ca-cert.pem
```

## 验证安装

**Arch Linux:**
```bash
trust list | grep -i mitmproxy
```

**Ubuntu/Debian:**
```bash
ls /etc/ssl/certs/ | grep mitmproxy
```

**NSS:**
```bash
certutil -d sql:$HOME/.pki/nssdb -L | grep mitmproxy
```
