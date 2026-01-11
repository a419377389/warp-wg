package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

func setSystemProxy(proxy string, enable bool) error {
	switch runtime.GOOS {
	case "windows":
		return setWindowsProxy(proxy, enable)
	case "darwin":
		return setMacProxy(proxy, enable)
	default:
		return setLinuxProxy(proxy, enable)
	}
}

func setWindowsProxy(proxy string, enable bool) error {
	if enable {
		if proxy == "" {
			return errors.New("proxy address empty")
		}
		_ = exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "1", "/f").Run()
		_ = exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyServer", "/t", "REG_SZ", "/d", proxy, "/f").Run()
		_ = exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyOverride", "/t", "REG_SZ", "/d", "localhost;127.0.0.1;*.local", "/f").Run()
		_ = exec.Command("netsh", "winhttp", "import", "proxy", "source=ie").Run()
		return nil
	}
	_ = exec.Command("reg", "add", `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`, "/v", "ProxyEnable", "/t", "REG_DWORD", "/d", "0", "/f").Run()
	_ = exec.Command("netsh", "winhttp", "import", "proxy", "source=ie").Run()
	return nil
}

func setLinuxProxy(proxy string, enable bool) error {
	if !enable {
		if commandExists("gsettings") {
			_ = exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "none").Run()
			return nil
		}
		if kde := resolveKDEConfig(); kde != "" {
			_ = exec.Command(kde, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "ProxyType", "0").Run()
			return nil
		}
		return nil
	}

	host, port := splitHostPort(proxy)
	if host == "" || port == "" {
		return errors.New("invalid proxy address")
	}

	if commandExists("gsettings") {
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy", "mode", "manual").Run()
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "host", host).Run()
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.http", "port", port).Run()
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "host", host).Run()
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy.https", "port", port).Run()
		_ = exec.Command("gsettings", "set", "org.gnome.system.proxy", "ignore-hosts", "['localhost','127.0.0.1','::1']").Run()
		return nil
	}
	if kde := resolveKDEConfig(); kde != "" {
		_ = exec.Command(kde, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "ProxyType", "1").Run()
		_ = exec.Command(kde, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "httpProxy", "http://"+host+":"+port).Run()
		_ = exec.Command(kde, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "httpsProxy", "http://"+host+":"+port).Run()
		_ = exec.Command(kde, "--file", "kioslaverc", "--group", "Proxy Settings", "--key", "NoProxyFor", "localhost,127.0.0.1,::1").Run()
		return nil
	}

	return errors.New("unsupported desktop environment")
}

func setMacProxy(proxy string, enable bool) error {
	service := activeNetworkService()
	if service == "" {
		service = "Wi-Fi"
	}
	if !enable {
		_ = exec.Command("networksetup", "-setwebproxystate", service, "off").Run()
		_ = exec.Command("networksetup", "-setsecurewebproxystate", service, "off").Run()
		return nil
	}
	host, port := splitHostPort(proxy)
	if host == "" || port == "" {
		return errors.New("invalid proxy address")
	}
	_ = exec.Command("networksetup", "-setwebproxy", service, host, port).Run()
	_ = exec.Command("networksetup", "-setsecurewebproxy", service, host, port).Run()
	_ = exec.Command("networksetup", "-setproxybypassdomains", service, "localhost", "127.0.0.1", "*.local").Run()
	return nil
}

func activeNetworkService() string {
	out, err := exec.Command("networksetup", "-listallnetworkservices").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "*") || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "wi-fi") || strings.Contains(lower, "wifi") || strings.Contains(lower, "ethernet") {
			return line
		}
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "*") {
			return line
		}
	}
	return ""
}

func resolveKDEConfig() string {
	if commandExists("kwriteconfig6") {
		return "kwriteconfig6"
	}
	if commandExists("kwriteconfig5") {
		return "kwriteconfig5"
	}
	return ""
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func splitHostPort(proxy string) (string, string) {
	parts := strings.Split(proxy, ":")
	if len(parts) < 2 {
		return "", ""
	}
	host := strings.TrimSpace(parts[0])
	port := strings.TrimSpace(parts[1])
	if host == "" || port == "" {
		return "", ""
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", ""
	}
	return host, port
}
