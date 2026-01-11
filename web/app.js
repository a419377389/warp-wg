const $ = (id) => document.getElementById(id);

const elements = {
  activationPill: $("activationPill"),
  activationExpiry: $("activationExpiry"),
  activationRemaining: $("activationRemaining"),
  deviceId: $("deviceId"),
  codeInput: $("codeInput"),
  activateBtn: $("activateBtn"),
  unbindBtn: $("unbindBtn"),
  currentEmail: $("currentEmail"),
  currentStatus: $("currentStatus"),
  currentQuota: $("currentQuota"),
  currentRefresh: $("currentRefresh"),
  refreshBtn: $("refreshBtn"),
  assignedCount: $("assignedCount"),
  switchCount: $("switchCount"),
  totalQuota: $("totalQuota"),
  totalUsed: $("totalUsed"),
  accountSelect: $("accountSelect"),
  switchBtn: $("switchBtn"),
  warpPath: $("warpPath"),
  autoDetectBtn: $("autoDetectBtn"),
  savePathBtn: $("savePathBtn"),
  warpStartBtn: $("warpStartBtn"),
  warpStopBtn: $("warpStopBtn"),
  gatewayStartBtn: $("gatewayStartBtn"),
  gatewayStopBtn: $("gatewayStopBtn"),
  gatewayStatus: $("gatewayStatus"),
  warpStatus: $("warpStatus"),
  logLines: $("logLines"),
  clearLogBtn: $("clearLogBtn"),
  toast: $("toast")
};

function showToast(message, ok = true) {
  if (!elements.toast) return;
  elements.toast.textContent = message;
  elements.toast.style.background = ok ? "rgba(10, 122, 255, 0.85)" : "rgba(220, 68, 55, 0.85)";
  elements.toast.classList.add("show");
  setTimeout(() => elements.toast.classList.remove("show"), 2200);
}

async function apiGet(url) {
  const res = await fetch(url);
  return res.json();
}

async function apiPost(url, payload = {}) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload)
  });
  return res.json();
}

function formatTimestamp(ts) {
  if (!ts) return "—";
  const date = new Date(ts * 1000);
  return date.toLocaleString("zh-CN", { hour12: false });
}

function formatRemaining(expiresAt, serverTime) {
  if (!expiresAt) return "—";
  const now = serverTime ? serverTime : Math.floor(Date.now() / 1000);
  const diff = Math.max(expiresAt - now, 0);
  const days = Math.floor(diff / 86400);
  const hours = Math.floor((diff % 86400) / 3600);
  const mins = Math.floor((diff % 3600) / 60);
  if (days > 0) return `${days}天${hours}小时`;
  if (hours > 0) return `${hours}小时${mins}分钟`;
  return `${mins}分钟`;
}

function updateActivation(data) {
  if (!data || data.success === false) {
    elements.activationPill.textContent = "状态异常";
    elements.activationPill.style.background = "rgba(220, 68, 55, 0.15)";
    elements.activationPill.style.color = "#dc4437";
    return;
  }
  if (!data.activated) {
    elements.activationPill.textContent = "未激活";
    elements.activationPill.style.background = "rgba(245, 158, 11, 0.2)";
    elements.activationPill.style.color = "#b45309";
    elements.activationExpiry.textContent = "到期时间：—";
    elements.activationRemaining.textContent = "—";
    return;
  }
  elements.activationPill.textContent = data.active ? "已激活" : "已过期";
  elements.activationPill.style.background = data.active ? "rgba(10, 122, 255, 0.18)" : "rgba(220, 68, 55, 0.15)";
  elements.activationPill.style.color = data.active ? "#0a7aff" : "#dc4437";
  elements.activationExpiry.textContent = `到期时间：${formatTimestamp(data.expiresAt)}`;
  elements.activationRemaining.textContent = formatRemaining(data.expiresAt, data.serverTime);
  elements.deviceId.textContent = data.deviceId || "—";
}

function updateAccounts(data) {
  if (!data || data.success === false) return;
  const current = data.currentAccount || null;
  elements.currentEmail.textContent = current?.email || "—";
  elements.currentStatus.textContent = current?.status || "—";
  if (current) {
    const remaining = current.quota ? Math.max(current.quota - current.used, 0) : 0;
    elements.currentQuota.textContent = `${current.quota || 0} / ${current.used || 0} (剩余 ${remaining})`;
    elements.currentRefresh.textContent = current.nextRefreshTime || "—";
  } else {
    elements.currentQuota.textContent = "—";
    elements.currentRefresh.textContent = "—";
  }

  const stats = data.stats || {};
  const assigned = stats.assigned_total || data.accountCount || data.localAccounts?.length || 0;
  elements.assignedCount.textContent = assigned;
  elements.switchCount.textContent = data.switchCount ?? 0;
  elements.totalQuota.textContent = stats.total_quota ?? "—";
  elements.totalUsed.textContent = stats.total_used ?? "—";

  elements.accountSelect.innerHTML = "";
  (data.localAccounts || []).forEach(acc => {
    const option = document.createElement("option");
    option.value = acc.email;
    option.textContent = `${acc.email} (${acc.status || "normal"})`;
    if (current && acc.email === current.email) {
      option.selected = true;
    }
    elements.accountSelect.appendChild(option);
  });
}

function updateGatewayStatus(data) {
  if (!data || data.success === false) return;
  elements.gatewayStatus.textContent = data.running ? `网关状态：运行中 (${data.port})` : "网关状态：已停止";
}

function updateWarpStatus(data) {
  if (!data || data.success === false) return;
  elements.warpStatus.textContent = data.running ? "Warp 状态：运行中" : "Warp 状态：已停止";
  if (data.path) {
    elements.warpPath.value = data.path;
  }
}

function appendLog(line) {
  if (!line) return;
  const div = document.createElement("div");
  div.textContent = line;
  elements.logLines.appendChild(div);
  while (elements.logLines.children.length > 200) {
    elements.logLines.removeChild(elements.logLines.firstChild);
  }
  elements.logLines.scrollTop = elements.logLines.scrollHeight;
}

async function loadAll() {
  const [activation, accounts, gateway, warp] = await Promise.all([
    apiGet("/api/activation/status").catch(() => ({})),
    apiGet("/api/accounts").catch(() => ({})),
    apiGet("/api/gateway/status").catch(() => ({})),
    apiGet("/api/warp/status").catch(() => ({}))
  ]);
  updateActivation(activation);
  updateAccounts(accounts);
  updateGatewayStatus(gateway);
  updateWarpStatus(warp);
}

async function initLogs() {
  const tail = await apiGet("/api/logs/tail?lines=80").catch(() => null);
  if (tail?.lines) {
    tail.lines.forEach(appendLog);
  }
  const source = new EventSource("/api/logs/stream");
  source.onmessage = (event) => {
    appendLog(event.data);
  };
}

elements.activateBtn.addEventListener("click", async () => {
  const code = elements.codeInput.value.trim();
  if (!code) {
    showToast("请输入卡密", false);
    return;
  }
  const res = await apiPost("/api/activation/login", { code });
  if (!res.success) {
    showToast(res.error || "激活失败", false);
    return;
  }
  showToast("激活成功");
  await loadAll();
});

elements.unbindBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/activation/unbind");
  if (!res.success) {
    showToast(res.error || "解绑失败", false);
    return;
  }
  showToast("解绑成功");
  await loadAll();
});

elements.refreshBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/accounts/refresh", {});
  if (!res.success) {
    showToast(res.error || "刷新失败", false);
  } else {
    showToast("刷新完成");
  }
  await loadAll();
});

elements.switchBtn.addEventListener("click", async () => {
  const email = elements.accountSelect.value;
  if (!email) {
    showToast("请选择账号", false);
    return;
  }
  const res = await apiPost("/api/accounts/switch", { email });
  if (!res.success) {
    showToast(res.error || "切换失败", false);
    return;
  }
  showToast("切换成功");
  await loadAll();
});

elements.autoDetectBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/warp/path/auto");
  if (!res.success) {
    showToast(res.error || "检测失败", false);
    return;
  }
  elements.warpPath.value = res.path;
  showToast("已自动检测");
});

elements.savePathBtn.addEventListener("click", async () => {
  const path = elements.warpPath.value.trim();
  if (!path) {
    showToast("请填写路径", false);
    return;
  }
  const res = await apiPost("/api/warp/path", { path });
  if (!res.success) {
    showToast(res.error || "保存失败", false);
    return;
  }
  showToast("路径已保存");
});

elements.warpStartBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/warp/start");
  if (!res.success) {
    showToast(res.error || "启动失败", false);
  } else {
    showToast("Warp 已启动");
  }
  await loadAll();
});

elements.warpStopBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/warp/stop");
  if (!res.success) {
    showToast(res.error || "关闭失败", false);
  } else {
    showToast("Warp 已关闭");
  }
  await loadAll();
});

elements.gatewayStartBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/gateway/start");
  if (!res.success) {
    showToast(res.error || "启动失败", false);
  } else {
    showToast("网关已启动");
  }
  await loadAll();
});

elements.gatewayStopBtn.addEventListener("click", async () => {
  const res = await apiPost("/api/gateway/stop");
  if (!res.success) {
    showToast(res.error || "关闭失败", false);
  } else {
    showToast("网关已关闭");
  }
  await loadAll();
});

elements.clearLogBtn.addEventListener("click", () => {
  elements.logLines.innerHTML = "";
});

loadAll();
initLogs();
setInterval(loadAll, 20000);
