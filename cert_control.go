package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/lqqyt2423/go-mitmproxy/cert"
)

var (
	errCertNotTrusted   = errors.New("\u8bf7\u5148\u5b89\u88c5\u5e76\u4fe1\u4efb\u4ee3\u7406\u8bc1\u4e66")
	errCertGenFailed    = errors.New("\u8bc1\u4e66\u751f\u6210\u5931\u8d25")
	errCertInstallFailed = errors.New("\u8bc1\u4e66\u5b89\u88c5\u5931\u8d25")
	errCertInstallLinux  = errors.New("\u8bc1\u4e66\u5b89\u88c5\u5931\u8d25\uff0c\u8bf7\u624b\u52a8\u6267\u884c: sudo cp ~/.mitmproxy/mitmproxy-ca-cert.pem /usr/local/share/ca-certificates/mitmproxy-ca-cert.crt && sudo update-ca-certificates")
)

func ensureCertificateReady(log *Logger) error {
	certPath, err := ensureMitmproxyCA()
	if err != nil {
		if log != nil {
			log.Error("certificate generation failed: " + err.Error())
		}
		return errCertGenFailed
	}
	if certPath == "" || !pathExists(certPath) {
		if log != nil {
			log.Error("certificate file missing")
		}
		return errCertGenFailed
	}

	trusted, err := checkCertificateTrusted(certPath)
	if err == nil && trusted {
		return nil
	}

	if err := installCertificate(certPath); err != nil {
		if log != nil {
			log.Warn("certificate install failed: " + err.Error())
		}
		if runtime.GOOS == "linux" {
			return errCertInstallLinux
		}
		return errCertInstallFailed
	}

	trusted, _ = checkCertificateTrusted(certPath)
	if trusted {
		return nil
	}
	return errCertNotTrusted
}

func ensureMitmproxyCA() (string, error) {
	dir := mitmproxyDir()
	if dir == "" {
		return "", errors.New("mitmproxy dir not available")
	}
	if _, err := cert.NewCA(dir); err != nil {
		return "", err
	}
	return mitmproxyCertFile(dir), nil
}

func mitmproxyDir() string {
	if override := strings.TrimSpace(os.Getenv("MITMPROXY_DIR")); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mitmproxy")
}

func mitmproxyCertFile(dir string) string {
	if dir == "" {
		return ""
	}
	pem := filepath.Join(dir, "mitmproxy-ca-cert.pem")
	cer := filepath.Join(dir, "mitmproxy-ca-cert.cer")
	if runtime.GOOS == "windows" {
		if pathExists(cer) {
			return cer
		}
		if pathExists(pem) {
			return pem
		}
		return cer
	}
	if pathExists(pem) {
		return pem
	}
	if pathExists(cer) {
		return cer
	}
	return pem
}

func checkCertificateTrusted(certPath string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		return checkCertificateTrustedWindows(certPath)
	case "darwin":
		return checkCertificateTrustedMac()
	default:
		return checkCertificateTrustedLinux(certPath)
	}
}

func installCertificate(certPath string) error {
	switch runtime.GOOS {
	case "windows":
		return installCertificateWindows(certPath)
	case "darwin":
		return installCertificateMac(certPath)
	default:
		return installCertificateLinux(certPath)
	}
}

func checkCertificateTrustedWindows(certPath string) (bool, error) {
	script := fmt.Sprintf(`$certPath = '%s'
if (-not (Test-Path $certPath)) {
    Write-Output "FILE_NOT_EXISTS"
    exit 0
}
try {
    $cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2
    $cert.Import($certPath)
    $thumbprint = $cert.Thumbprint
    $store = New-Object System.Security.Cryptography.X509Certificates.X509Store("Root", "CurrentUser")
    $store.Open("ReadOnly")
    $found = $store.Certificates | Where-Object { $_.Thumbprint -eq $thumbprint }
    $store.Close()
    if ($found) {
        Write-Output "INSTALLED"
    } else {
        Write-Output "NOT_INSTALLED"
    }
} catch {
    Write-Output "ERROR"
}
`, escapeWindowsPath(certPath))
	out, err := runPowerShellScript(script)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "INSTALLED", nil
}

func installCertificateWindows(certPath string) error {
	script := fmt.Sprintf(`$cert = New-Object System.Security.Cryptography.X509Certificates.X509Certificate2
$cert.Import('%s')
$store = New-Object System.Security.Cryptography.X509Certificates.X509Store("Root", "CurrentUser")
$store.Open("ReadWrite")
$store.Add($cert)
$store.Close()
Write-Output "OK"
`, escapeWindowsPath(certPath))
	out, err := runPowerShellScript(script)
	if err != nil {
		return err
	}
	if !strings.Contains(out, "OK") {
		return errors.New("certificate install failed")
	}
	return nil
}

func checkCertificateTrustedMac() (bool, error) {
	// 检查证书是否在系统钥匙串中（不是 login.keychain）
	cmd := exec.Command("security", "find-certificate", "-c", "mitmproxy", "-a", "/Library/Keychains/System.keychain")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, nil
	}
	return strings.Contains(string(out), "mitmproxy"), nil
}

func installCertificateMac(certPath string) error {
	// 直接安装到系统钥匙串并设置信任策略
	script := fmt.Sprintf(`do shell script "security add-trusted-cert -d -r trustRoot -p ssl -p basic -k /Library/Keychains/System.keychain '%s'" with administrator privileges`, escapeAppleScriptValue(certPath))
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript failed: %v, output: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func checkCertificateTrustedLinux(certPath string) (bool, error) {
	home, _ := os.UserHomeDir()
	nssdb := filepath.Join(home, ".pki", "nssdb")
	var nssOK, systemOK bool

	// 检查 NSS 数据库
	if commandExists("certutil") {
		if exec.Command("certutil", "-d", "sql:"+nssdb, "-L", "-n", "mitmproxy").Run() == nil {
			nssOK = true
		} else if exec.Command("certutil", "-d", "sql:"+nssdb, "-L", "-n", "mitmproxy-ca-cert").Run() == nil {
			nssOK = true
		}
	}

	// 使用 openssl 验证系统 CA 是否信任
	if commandExists("openssl") && pathExists("/etc/ssl/certs") {
		out, err := exec.Command("openssl", "verify", "-CApath", "/etc/ssl/certs", certPath).CombinedOutput()
		if err == nil && strings.Contains(string(out), ": OK") {
			systemOK = true
		}
	}

	// 备用：检查文件是否存在
	if !systemOK {
		systemCAPath := "/usr/local/share/ca-certificates/mitmproxy-ca-cert.crt"
		if pathExists(systemCAPath) {
			srcData, err1 := os.ReadFile(certPath)
			dstData, err2 := os.ReadFile(systemCAPath)
			if err1 == nil && err2 == nil && string(srcData) == string(dstData) {
				systemOK = true
			}
		}
	}

	// NSS 和系统 CA 都需要安装才算完全信任
	return nssOK && systemOK, nil
}

func installCertificateLinux(certPath string) error {
	home, _ := os.UserHomeDir()
	nssdb := filepath.Join(home, ".pki", "nssdb")
	var nssOK, systemOK bool

	// 1. 安装到 NSS 数据库（Chrome/Chromium 等使用）
	if commandExists("certutil") {
		if err := ensureNssDb(nssdb); err == nil {
			_ = exec.Command("certutil", "-d", "sql:"+nssdb, "-D", "-n", "mitmproxy").Run()
			if exec.Command("certutil", "-d", "sql:"+nssdb, "-A", "-t", "CT,C,C", "-n", "mitmproxy", "-i", certPath).Run() == nil {
				nssOK = true
			}
		}
	}

	// 2. 安装到系统 CA 存储（Warp 等使用系统 CA 的应用）
	systemOK = installCertificateLinuxSystem(certPath)

	if nssOK || systemOK {
		return nil
	}
	return errors.New("certificate install failed")
}

func installCertificateLinuxSystem(certPath string) bool {
	// 系统 CA 目录
	caDir := "/usr/local/share/ca-certificates"
	destPath := filepath.Join(caDir, "mitmproxy-ca-cert.crt")

	// 检查是否已安装
	if pathExists(destPath) {
		// 检查内容是否相同
		srcData, err1 := os.ReadFile(certPath)
		dstData, err2 := os.ReadFile(destPath)
		if err1 == nil && err2 == nil && string(srcData) == string(dstData) {
			return true
		}
	}

	// 尝试使用 pkexec 安装（图形化提权）
	if commandExists("pkexec") {
		script := fmt.Sprintf(`#!/bin/bash
mkdir -p '%s'
cp '%s' '%s'
update-ca-certificates 2>/dev/null || true
echo OK`, caDir, certPath, destPath)
		tmpScript, err := os.CreateTemp("", "install_cert_*.sh")
		if err == nil {
			tmpScript.WriteString(script)
			tmpScript.Close()
			os.Chmod(tmpScript.Name(), 0755)
			defer os.Remove(tmpScript.Name())
			if out, err := exec.Command("pkexec", "bash", tmpScript.Name()).CombinedOutput(); err == nil && strings.Contains(string(out), "OK") {
				return true
			}
		}
	}

	// 尝试直接使用 sudo（非交互式，可能失败）
	if commandExists("sudo") {
		_ = exec.Command("sudo", "-n", "mkdir", "-p", caDir).Run()
		if exec.Command("sudo", "-n", "cp", certPath, destPath).Run() == nil {
			_ = exec.Command("sudo", "-n", "update-ca-certificates").Run()
			return true
		}
	}

	return false
}

func ensureNssDb(path string) error {
	if !commandExists("certutil") {
		return errors.New("certutil not available")
	}
	if exec.Command("certutil", "-d", "sql:"+path, "-L").Run() == nil {
		return nil
	}
	_ = os.MkdirAll(path, 0o700)
	return exec.Command("certutil", "-d", "sql:"+path, "-N", "--empty-password").Run()
}

func runPowerShellScript(script string) (string, error) {
	tmp, err := os.CreateTemp("", "gateway_cert_*.ps1")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("\ufeff" + script); err != nil {
		_ = tmp.Close()
		return "", err
	}
	_ = tmp.Close()

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-File", tmp.Name())
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func escapeAppleScriptValue(value string) string {
	return strings.ReplaceAll(value, "'", "'\\''")
}
