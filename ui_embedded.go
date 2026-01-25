package main

import (
	"io"
	"net/http"
	"strings"
)

var embeddedUIHTML string

func init() {
	var builder strings.Builder
	builder.WriteString(`<!doctype html>
<html lang="zh">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>Warp 无感网关</title>
    <style>
      :root {
        --bg: #f4f6fb;
        --bg-deep: #e6ecf7;
        --card: rgba(255, 255, 255, 0.85);
        --text: #0f172a;
        --muted: #64748b;
        --line: rgba(15, 23, 42, 0.08);
        --primary: #0a7aff;
        --danger: #dc4437;
        --warning: #f59e0b;
        --good: #16a34a;
        --shadow: 0 20px 45px rgba(15, 23, 42, 0.15);
      }

      * {
        box-sizing: border-box;
      }

      body {
        margin: 0;
        font-family: "SF Pro Text", "SF Pro Display", "Helvetica Neue", "PingFang SC", "Microsoft YaHei", "Segoe UI", sans-serif;
        color: var(--text);
        background: radial-gradient(circle at top left, #ffffff 0%, var(--bg) 50%, var(--bg-deep) 100%);
        min-height: 100vh;
      }

      body.locked {
        overflow: hidden;
      }

      .backdrop {
        position: fixed;
        inset: 0;
        pointer-events: none;
        background:
          radial-gradient(circle at 10% 10%, rgba(10, 122, 255, 0.15), transparent 45%),
          radial-gradient(circle at 90% 20%, rgba(22, 163, 74, 0.12), transparent 35%),
          radial-gradient(circle at 70% 80%, rgba(245, 158, 11, 0.15), transparent 40%);
        z-index: 0;
      }

      .shell {
        position: relative;
        z-index: 1;
        max-width: 1200px;
        margin: 0 auto;
        padding: 32px 24px 48px;
      }

      .topbar {
        display: flex;
        justify-content: space-between;
        gap: 24px;
        align-items: center;
        flex-wrap: wrap;
        margin-bottom: 24px;
      }

      .brand {
        display: flex;
        gap: 16px;
        align-items: center;
      }

      .brand-mark {
        width: 52px;
        height: 52px;
        border-radius: 16px;
        background: linear-gradient(140deg, #0a7aff, #66b3ff);
        box-shadow: 0 10px 20px rgba(10, 122, 255, 0.35);
        display: grid;
        place-items: center;
        color: white;
        font-weight: 700;
        font-size: 22px;
        letter-spacing: 0.08em;
      }

      .brand-title {
        font-size: 24px;
        font-weight: 700;
      }

      .brand-sub {
        font-size: 14px;
        color: var(--muted);
        margin-top: 6px;
      }

      .activation-panel {
        display: flex;
        flex-direction: column;
        gap: 8px;
        align-items: flex-end;
        min-width: 320px;
      }

      .activation-strip {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: flex-end;
        gap: 14px;
      }

      .activation-pill {
        padding: 6px 12px;
        border-radius: 999px;
        font-weight: 600;
        font-size: 13px;
        background: rgba(10, 122, 255, 0.12);
        color: var(--primary);
      }

      .activation-item {
        display: flex;
        flex-direction: column;
        gap: 4px;
        padding-left: 14px;
        border-left: 1px dashed var(--line);
        min-width: 110px;
      }

      .activation-item.first {
        border-left: none;
        padding-left: 0;
      }

      .activation-label {
        font-size: 11px;
        color: var(--muted);
      }

      .activation-value {
        font-size: 13px;
        font-weight: 600;
      }

      .activation-actions {
        display: flex;
        gap: 8px;
      }

      .activation-error {
        color: var(--danger);
        font-size: 12px;
        min-height: 16px;
        text-align: right;
      }

      .notice-bar {
        display: none;
        align-items: center;
        gap: 12px;
        padding: 10px 14px;
        border-radius: 16px;
        border: 1px solid var(--line);
        background: rgba(255, 255, 255, 0.8);
        box-shadow: var(--shadow);
        margin-bottom: 20px;
        overflow: hidden;
      }

      .notice-bar.show {
        display: flex;
      }

      .notice-bar.scroll .notice-track {
        animation: noticeScroll 18s linear infinite;
      }

      .notice-label {
        font-size: 11px;
        font-weight: 700;
        color: var(--muted);
        letter-spacing: 0.2em;
      }

      .notice-window {
        position: relative;
        overflow: hidden;
        flex: 1;
        min-width: 0;
      }

      .notice-track {
        display: inline-flex;
        align-items: center;
        gap: 16px;
        white-space: nowrap;
        will-change: transform;
      }

      .notice-text {
        font-size: 13px;
        font-weight: 600;
        color: var(--text);
        text-decoration: none;
      }

      .notice-text.linked {
        color: var(--primary);
      }

      .notice-text.linked:hover {
        text-decoration: underline;
      }

      .notice-gap {
        color: var(--muted);
      }

      .grid {
        display: grid;
        grid-template-columns: repeat(12, minmax(0, 1fr));
        gap: 20px;
      }
`)
	builder.WriteString(`
      .card {
        background: var(--card);
        border: 1px solid var(--line);
        border-radius: 18px;
        padding: 18px;
        box-shadow: var(--shadow);
        backdrop-filter: blur(12px);
        animation: rise 0.6s ease both;
      }

      .card-header {
        display: flex;
        justify-content: space-between;
        align-items: center;
        margin-bottom: 14px;
        gap: 12px;
      }

      .card-title {
        font-weight: 700;
        font-size: 16px;
      }

      .card-actions {
        display: flex;
        gap: 10px;
        flex-wrap: wrap;
      }

      .card-title-divider {
        color: var(--muted);
        font-weight: 400;
      }

      .service-connection {
        grid-column: span 6;
      }

      .account-stats {
        grid-column: span 6;
      }

      .logs {
        grid-column: span 12;
      }

      .split-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 16px;
      }

      .panel {
        background: rgba(15, 23, 42, 0.04);
        border-radius: 16px;
        padding: 14px;
        border: 1px solid rgba(15, 23, 42, 0.06);
      }

      .panel-title {
        font-size: 12px;
        color: var(--muted);
        margin-bottom: 10px;
      }

      .status-row {
        display: flex;
        gap: 8px;
        flex-wrap: wrap;
      }

      .status-pill {
        padding: 6px 10px;
        border-radius: 999px;
        font-size: 12px;
        font-weight: 600;
        background: rgba(100, 116, 139, 0.12);
        color: #334155;
      }

      .status-pill.running {
        background: rgba(22, 163, 74, 0.15);
        color: #15803d;
      }

      .status-pill.stopped {
        background: rgba(220, 68, 55, 0.12);
        color: #b42318;
      }

      .service-item {
        display: grid;
        grid-template-columns: 1fr 1fr;
        gap: 8px 12px;
        align-items: center;
        padding: 12px 0;
      }

      .service-item + .service-item {
        border-top: 1px solid var(--line);
      }

      .service-label {
        font-weight: 600;
      }

      .service-status {
        font-size: 13px;
        color: var(--muted);
      }

      .service-actions {
        grid-column: span 2;
        display: flex;
        gap: 10px;
        flex-wrap: wrap;
        margin-top: 6px;
      }

      .account-email {
        font-size: 18px;
        font-weight: 600;
        word-break: break-all;
      }

      .badge-row {
        display: flex;
        gap: 8px;
        margin-top: 6px;
        flex-wrap: wrap;
      }

      .badge {
        padding: 4px 10px;
        border-radius: 999px;
        font-size: 12px;
        background: rgba(100, 116, 139, 0.15);
        color: #475569;
      }

      .badge.tone-good {
        background: rgba(22, 163, 74, 0.12);
        color: #15803d;
      }

      .badge.tone-warn {
        background: rgba(245, 158, 11, 0.18);
        color: #b45309;
      }

      .badge.tone-bad {
        background: rgba(220, 68, 55, 0.12);
        color: #b42318;
      }

      .field-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 12px;
        margin-top: 16px;
      }

      .field {
        display: flex;
        flex-direction: column;
        gap: 6px;
        font-size: 13px;
      }

      .label {
        color: var(--muted);
      }

      .value {
        font-weight: 600;
      }

      .input {
        width: 100%;
        padding: 10px 12px;
        border-radius: 12px;
        border: 1px solid var(--line);
        font-size: 14px;
        outline: none;
        background: rgba(255, 255, 255, 0.9);
      }

      .input:focus {
        border-color: rgba(10, 122, 255, 0.6);
        box-shadow: 0 0 0 3px rgba(10, 122, 255, 0.12);
      }

      .btn {
        border: none;
        border-radius: 12px;
        padding: 9px 14px;
        font-size: 13px;
        font-weight: 600;
        cursor: pointer;
        transition: transform 0.15s ease, box-shadow 0.15s ease;
      }

      .btn.primary {
        background: linear-gradient(135deg, #0a7aff, #59b6ff);
        color: white;
        box-shadow: 0 10px 18px rgba(10, 122, 255, 0.25);
      }

      .btn.ghost {
        background: rgba(15, 23, 42, 0.06);
        color: #334155;
      }

      .btn.small {
        padding: 6px 10px;
      }

      .btn:active {
        transform: scale(0.98);
      }

      .stat-grid {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: 14px;
      }

      .stat {
        background: rgba(15, 23, 42, 0.04);
        border-radius: 14px;
        padding: 12px;
        border: 1px solid rgba(15, 23, 42, 0.06);
      }

      .stat-label {
        font-size: 12px;
        color: var(--muted);
      }

      .stat-value {
        margin-top: 6px;
        font-size: 18px;
        font-weight: 700;
      }

      .log-lines {
        height: 260px;
        overflow-y: auto;
        font-family: "SF Mono", "JetBrains Mono", "Cascadia Mono", "Menlo", monospace;
        font-size: 12px;
        background: rgba(15, 23, 42, 0.9);
        color: #e2e8f0;
        border-radius: 14px;
        padding: 12px;
        line-height: 1.5;
      }

      .log-lines div {
        white-space: pre-wrap;
      }

      #toast {
        position: fixed;
        right: 20px;
        bottom: 20px;
        background: rgba(10, 122, 255, 0.9);
        color: white;
        padding: 10px 14px;
        border-radius: 999px;
        font-size: 13px;
        opacity: 0;
        transform: translateY(10px);
        transition: opacity 0.2s ease, transform 0.2s ease;
        z-index: 20;
      }

      #toast.show {
        opacity: 1;
        transform: translateY(0);
      }

      .modal {
        position: fixed;
        inset: 0;
        background: rgba(15, 23, 42, 0.6);
        display: flex;
        align-items: center;
        justify-content: center;
        opacity: 0;
        pointer-events: none;
        transition: opacity 0.2s ease;
        z-index: 30;
      }

      .modal.show {
        opacity: 1;
        pointer-events: auto;
      }

      .modal-card {
        width: min(420px, 90vw);
        background: white;
        border-radius: 20px;
        padding: 24px;
        box-shadow: var(--shadow);
        display: flex;
        flex-direction: column;
        gap: 14px;
      }

      .modal-title {
        font-size: 20px;
        font-weight: 700;
      }

      .modal-desc {
        font-size: 13px;
        color: var(--muted);
      }

      .modal-error {
        color: var(--danger);
        font-size: 12px;
        min-height: 16px;
      }

      .modal-actions {
        display: flex;
        gap: 10px;
        margin-top: 6px;
      }

      .modal-hint {
        font-size: 12px;
        color: var(--muted);
      }

      @keyframes rise {
        from {
          opacity: 0;
          transform: translateY(12px);
        }
        to {
          opacity: 1;
          transform: translateY(0);
        }
      }

      @keyframes noticeScroll {
        from {
          transform: translateX(0);
        }
        to {
          transform: translateX(calc(-1 * var(--notice-distance, 50%)));
        }
      }

      @media (max-width: 980px) {
        .topbar {
          flex-direction: column;
          align-items: flex-start;
        }
        .activation-panel {
          align-items: flex-start;
          width: 100%;
        }
        .activation-strip {
          justify-content: flex-start;
        }
        .activation-item {
          border-left: none;
          padding-left: 0;
        }
        .notice-bar {
          width: 100%;
        }
        .grid {
          grid-template-columns: repeat(1, minmax(0, 1fr));
        }
        .service-connection,
        .account-stats,
        .logs {
          grid-column: span 1;
        }
        .split-grid {
          grid-template-columns: repeat(1, minmax(0, 1fr));
        }
        .field-grid {
          grid-template-columns: repeat(1, minmax(0, 1fr));
        }
        .stat-grid {
          grid-template-columns: repeat(1, minmax(0, 1fr));
        }
      }
    </style>
  </head>
`)
	builder.WriteString(`
  <body class="locked">
    <div class="backdrop"></div>
    <div class="shell">
      <header class="topbar">
        <div class="brand">
          <div class="brand-mark">WG</div>
          <div>
            <div class="brand-title" data-i18n="title"></div>
            <div class="brand-sub" data-i18n="subtitle"></div>
          </div>
        </div>
        <div class="activation-panel">
          <div class="activation-strip">
            <div class="activation-pill" id="activationPill">--</div>
            <div class="activation-item first">
              <span class="activation-label" data-i18n="activationExpiresLabel"></span>
              <span class="activation-value" id="activationExpiry">--</span>
            </div>
            <div class="activation-item">
              <span class="activation-label" data-i18n="activationRemainingLabel"></span>
              <span class="activation-value" id="activationRemaining">--</span>
            </div>
            <div class="activation-item">
              <span class="activation-label" data-i18n="activationDeviceLabel"></span>
              <span class="activation-value" id="deviceId">--</span>
            </div>
            <div class="activation-actions">
              <button class="btn ghost small" id="unbindBtn" data-i18n="unbind"></button>
            </div>
          </div>
          <div class="activation-error" id="activationError"></div>
        </div>
      </header>

      <div class="notice-bar" id="noticeBar">
        <div class="notice-label" data-i18n="noticeLabel"></div>
        <div class="notice-window" id="noticeWindow">
          <div class="notice-track" id="noticeTrack">
            <a class="notice-text" id="noticeLink"></a>
            <span class="notice-gap">•</span>
            <a class="notice-text" id="noticeLinkClone"></a>
          </div>
        </div>
      </div>

      <main class="grid">
        <section class="card service-connection" style="animation-delay: 0s;">
          <div class="card-header">
            <div class="card-title">
              <span data-i18n="sectionService"></span>
              <span class="card-title-divider">/</span>
              <span data-i18n="sectionWarp"></span>
            </div>
            <div class="status-row">
              <span class="status-pill" id="warpStatusBadge">--</span>
              <span class="status-pill" id="gatewayStatusBadge">--</span>
            </div>
          </div>
          <div class="split-grid">
            <div class="panel">
              <div class="panel-title" data-i18n="sectionService"></div>
              <div class="service-item">
                <div class="service-label" data-i18n="labelWarp"></div>
                <div class="service-status" id="warpStatus">--</div>
                <div class="service-actions">
                  <button class="btn primary" id="warpStartBtn" data-i18n="start"></button>
                  <button class="btn ghost" id="warpStopBtn" data-i18n="stop"></button>
                </div>
              </div>
              <div class="service-item">
                <div class="service-label" data-i18n="labelGateway"></div>
                <div class="service-status" id="gatewayStatus">--</div>
                <div class="service-actions">
                  <button class="btn primary" id="gatewayStartBtn" data-i18n="start"></button>
                  <button class="btn ghost" id="gatewayStopBtn" data-i18n="stop"></button>
                </div>
              </div>
            </div>
            <div class="panel">
              <div class="panel-title" data-i18n="sectionWarp"></div>
              <div class="field">
                <div class="label" data-i18n="warpPathLabel"></div>
                <input class="input" id="warpPath" data-i18n-placeholder="warpPathPlaceholder">
              </div>
              <div class="card-actions" style="margin-top: 12px;">
                <button class="btn ghost" id="autoDetectBtn" data-i18n="autoDetect"></button>
                <button class="btn primary" id="savePathBtn" data-i18n="savePath"></button>
              </div>
            </div>
          </div>
        </section>

        <section class="card account-stats" style="animation-delay: 0.05s;">
          <div class="card-header">
            <div class="card-title">
              <span data-i18n="sectionAccount"></span>
              <span class="card-title-divider">/</span>
              <span data-i18n="sectionStats"></span>
            </div>
            <div class="card-actions">
              <button class="btn ghost" id="refreshBtn" data-i18n="refreshQuota"></button>
              <button class="btn primary" id="switchBtn" data-i18n="switchAccount"></button>
            </div>
          </div>
          <div class="split-grid account-split">
            <div class="panel">
              <div class="panel-title" data-i18n="sectionAccount"></div>
              <div class="account-email" id="currentEmail">--</div>
              <div class="badge-row">
                <span class="badge" id="currentStatus">--</span>
                <span class="badge" id="currentType">--</span>
              </div>
              <div class="field-grid">
                <div class="field">
                  <div class="label" data-i18n="accountQuota"></div>
                  <div class="value" id="currentQuota">--</div>
                </div>
                <div class="field">
                  <div class="label" data-i18n="accountRemaining"></div>
                  <div class="value" id="currentRemaining">--</div>
                </div>
                <div class="field">
                  <div class="label" data-i18n="accountNextRefresh"></div>
                  <div class="value" id="currentRefresh">--</div>
                </div>
              </div>
            </div>
            <div class="panel">
              <div class="panel-title" data-i18n="sectionStats"></div>
              <div class="stat-grid">
                <div class="stat">
                  <div class="stat-label" data-i18n="statAssigned"></div>
                  <div class="stat-value" id="assignedCount">--</div>
                </div>
                <div class="stat">
                  <div class="stat-label" data-i18n="statSwitchCount"></div>
                  <div class="stat-value" id="switchCount">--</div>
                </div>
                <div class="stat">
                  <div class="stat-label" data-i18n="statTotalQuota"></div>
                  <div class="stat-value" id="totalQuota">--</div>
                </div>
                <div class="stat">
                  <div class="stat-label" data-i18n="statTotalUsed"></div>
                  <div class="stat-value" id="totalUsed">--</div>
                </div>
                <div class="stat">
                  <div class="stat-label" data-i18n="statVirtualUsed"></div>
                  <div class="stat-value" id="totalVirtualUsed">--</div>
                </div>
              </div>
            </div>
          </div>
        </section>

        <section class="card" style="grid-column: span 6; animation-delay: 0.15s;">
          <div class="card-header">
            <div class="card-title">
              <span data-i18n="sectionBackup"></span>
              <span class="card-title-divider">/</span>
              <span data-i18n="sectionRestore"></span>
            </div>
          </div>
          <p style="font-size: 13px; color: var(--muted); margin-bottom: 14px;" data-i18n="backupDesc"></p>
          <div class="field-grid">
            <div class="field">
              <div class="label" data-i18n="defaultBackupLabel"></div>
              <div class="value" id="defaultBackupStatus">--</div>
            </div>
            <div class="field">
              <div class="label" data-i18n="mcpBackupLabel"></div>
              <div class="value" id="mcpBackupStatus">--</div>
            </div>
          </div>
          <div class="card-actions" style="margin-top: 14px;">
            <button class="btn primary" id="backupAllBtn" data-i18n="backupAll"></button>
            <button class="btn ghost" id="restoreAllBtn" data-i18n="restoreAll"></button>
          </div>
        </section>

        <section class="card logs" style="animation-delay: 0.2s;">
          <div class="card-header">
            <div class="card-title" data-i18n="sectionLogs"></div>
            <div class="card-actions">
              <button class="btn ghost small" id="clearLogBtn" data-i18n="logClear"></button>
            </div>
          </div>
          <div class="log-lines" id="logLines"></div>
        </section>
      </main>
    </div>

    <div class="modal show" id="activationModal">
      <div class="modal-card">
        <div class="modal-title" data-i18n="activateModalTitle"></div>
        <div class="modal-desc" data-i18n="activateModalDesc"></div>
        <input class="input" id="codeInput" data-i18n-placeholder="codePlaceholder">
        <div class="modal-error" id="modalError"></div>
        <div class="modal-actions">
          <button class="btn primary" id="activateBtn" data-i18n="activateBtn"></button>
        </div>
        <div class="modal-hint" data-i18n="activateModalHint"></div>
      </div>
    </div>

    <div id="toast"></div>

    <script>
`)
	builder.WriteString(`
      const i18n = {
        title: "\u7f51\u5173\u63a7\u5236\u53f0",
        subtitle: "\u7f51\u5173\u4e0e\u8d26\u53f7\u8f6e\u6362\u7684\u7cbe\u7ec6\u63a7\u5236",
        activationExpiresLabel: "\u5230\u671f\u65f6\u95f4",
        activationRemainingLabel: "\u5269\u4f59\u65f6\u95f4",
        activationDeviceLabel: "\u8bbe\u5907\u6807\u8bc6",
        activationLockedTip: "\u8bf7\u5148\u6fc0\u6d3b",
        activationExpiredTip: "\u6388\u6743\u5df2\u8fc7\u671f",
        activationUnauthorized: "\u8bbe\u5907\u672a\u7ed1\u5b9a\u6216\u5df2\u8fc7\u671f",
        activateModalTitle: "\u5361\u5bc6\u6fc0\u6d3b",
        activateModalDesc: "\u8f93\u5165\u6709\u6548\u5361\u5bc6\u4ee5\u6fc0\u6d3b\u8bbe\u5907",
        activateModalHint: "\u672a\u6fc0\u6d3b\u524d\u65e0\u6cd5\u4f7f\u7528\u529f\u80fd",
        activateBtn: "\u7acb\u5373\u6fc0\u6d3b",
        unbind: "\u89e3\u7ed1\u8bbe\u5907",
        sectionService: "\u670d\u52a1\u72b6\u6001",
        sectionAccount: "\u5f53\u524d\u8d26\u53f7",
        sectionWarp: "\u8def\u5f84\u4e0e\u8fde\u63a5",
        sectionStats: "\u6570\u636e\u603b\u89c8",
        sectionLogs: "\u65e5\u5fd7\u76d1\u63a7",
        labelWarp: "WARP",
        labelGateway: "\u7f51\u5173",
        start: "\u542f\u52a8",
        stop: "\u505c\u6b62",
        refreshQuota: "\u5237\u65b0\u989d\u5ea6",
        switchAccount: "\u5207\u6362\u8d26\u53f7",
        accountQuota: "\u989d\u5ea6\u4f7f\u7528",
        accountRemaining: "\u5269\u4f59\u989d\u5ea6",
        accountNextRefresh: "\u4e0b\u6b21\u5237\u65b0",
        warpPathLabel: "WARP \u8def\u5f84",
        warpPathPlaceholder: "\u8bf7\u586b\u5199 WARP \u5ba2\u6237\u7aef\u8def\u5f84",
        autoDetect: "\u81ea\u52a8\u68c0\u6d4b",
        savePath: "\u4fdd\u5b58\u8def\u5f84",
        statAssigned: "\u5206\u914d\u8d26\u53f7",
        statSwitchCount: "\u5207\u6362\u6b21\u6570",
        statTotalQuota: "\u603b\u989d\u5ea6",
        statTotalUsed: "\u5df2\u4f7f\u7528",
        statVirtualUsed: "\u865a\u62df\u7528\u91cf",
        logClear: "\u6e05\u7a7a\u65e5\u5fd7",
        statusActive: "\u5df2\u6fc0\u6d3b",
        statusInactive: "\u672a\u6fc0\u6d3b",
        statusExpired: "\u5df2\u8fc7\u671f",
        statusError: "\u72b6\u6001\u5f02\u5e38",
        noticeLabel: "\u516c\u544a",
        toastActivationSuccess: "\u6fc0\u6d3b\u6210\u529f",
        toastActivationFailed: "\u6fc0\u6d3b\u5931\u8d25",
        toastUnbindSuccess: "\u89e3\u7ed1\u6210\u529f",
        toastUnbindFailed: "\u89e3\u7ed1\u5931\u8d25",
        toastSwitchSuccess: "\u5207\u6362\u6210\u529f",
        toastSwitchFailed: "\u5207\u6362\u5931\u8d25",
        toastRefreshDone: "\u5237\u65b0\u5b8c\u6210",
        toastRefreshFailed: "\u5237\u65b0\u5931\u8d25",
        toastStartWarpSuccess: "Warp \u5df2\u542f\u52a8",
        toastStopWarpSuccess: "Warp \u5df2\u505c\u6b62",
        toastStartGatewaySuccess: "\u7f51\u5173\u5df2\u542f\u52a8",
        toastStopGatewaySuccess: "\u7f51\u5173\u5df2\u505c\u6b62",
        toastPathAutoSuccess: "\u5df2\u81ea\u52a8\u68c0\u6d4b",
        toastPathSaved: "\u8def\u5f84\u5df2\u4fdd\u5b58",
        toastInputCode: "\u8bf7\u8f93\u5165\u5361\u5bc6",
        toastInputPath: "\u8bf7\u586b\u5199\u8def\u5f84",
        sectionBackup: "\u914d\u7f6e\u5907\u4efd",
        sectionRestore: "\u81ea\u52a8\u8fd8\u539f",
        backupDesc: "\u5907\u4efdWarp\u914d\u7f6e\uff0c\u5207\u6362\u8d26\u53f7\u540e\u81ea\u52a8\u8fd8\u539f",
        defaultBackupLabel: "Default\u8868\u5907\u4efd",
        mcpBackupLabel: "MCP\u5907\u4efd",
        backupAll: "\u5907\u4efd\u5168\u90e8",
        restoreAll: "\u8fd8\u539f\u5168\u90e8",
        toastBackupSuccess: "\u5907\u4efd\u6210\u529f",
        toastBackupFailed: "\u5907\u4efd\u5931\u8d25",
        toastRestoreSuccess: "\u8fd8\u539f\u6210\u529f",
        toastRestoreFailed: "\u8fd8\u539f\u5931\u8d25",
        toastBackuping: "\u6b63\u5728\u5907\u4efd...",
        toastRestoring: "\u6b63\u5728\u8fd8\u539f...",
        backupStatusYes: "\u5df2\u5907\u4efd",
        backupStatusNo: "\u672a\u5907\u4efd"
      };

      const statusLabels = {
        normal: "\u6b63\u5e38",
        available: "\u53ef\u7528",
        banned: "\u5df2\u5c01\u7981",
        error: "\u5f02\u5e38"
      };

      const $ = (id) => document.getElementById(id);
      const elements = {
        activationPill: $("activationPill"),
        activationExpiry: $("activationExpiry"),
        activationRemaining: $("activationRemaining"),
        deviceId: $("deviceId"),
        activationError: $("activationError"),
        modalError: $("modalError"),
        activationModal: $("activationModal"),
        codeInput: $("codeInput"),
        activateBtn: $("activateBtn"),
        unbindBtn: $("unbindBtn"),
        noticeBar: $("noticeBar"),
        noticeWindow: $("noticeWindow"),
        noticeTrack: $("noticeTrack"),
        noticeLink: $("noticeLink"),
        noticeLinkClone: $("noticeLinkClone"),
        currentEmail: $("currentEmail"),
        currentStatus: $("currentStatus"),
        currentType: $("currentType"),
        currentQuota: $("currentQuota"),
        currentRemaining: $("currentRemaining"),
        currentRefresh: $("currentRefresh"),
        refreshBtn: $("refreshBtn"),
        switchBtn: $("switchBtn"),
        assignedCount: $("assignedCount"),
        switchCount: $("switchCount"),
        totalQuota: $("totalQuota"),
        totalUsed: $("totalUsed"),
        totalVirtualUsed: $("totalVirtualUsed"),
        defaultBackupStatus: $("defaultBackupStatus"),
        mcpBackupStatus: $("mcpBackupStatus"),
        backupAllBtn: $("backupAllBtn"),
        restoreAllBtn: $("restoreAllBtn"),
        warpPath: $("warpPath"),
        autoDetectBtn: $("autoDetectBtn"),
        savePathBtn: $("savePathBtn"),
        warpStartBtn: $("warpStartBtn"),
        warpStopBtn: $("warpStopBtn"),
        gatewayStartBtn: $("gatewayStartBtn"),
        gatewayStopBtn: $("gatewayStopBtn"),
        gatewayStatus: $("gatewayStatus"),
        warpStatus: $("warpStatus"),
        warpStatusBadge: $("warpStatusBadge"),
        gatewayStatusBadge: $("gatewayStatusBadge"),
        logLines: $("logLines"),
        clearLogBtn: $("clearLogBtn"),
        toast: $("toast")
      };

      function applyI18n() {
        document.querySelectorAll("[data-i18n]").forEach((el) => {
          const key = el.getAttribute("data-i18n");
          if (i18n[key]) {
            el.textContent = i18n[key];
          }
        });
        document.querySelectorAll("[data-i18n-placeholder]").forEach((el) => {
          const key = el.getAttribute("data-i18n-placeholder");
          if (i18n[key]) {
            el.setAttribute("placeholder", i18n[key]);
          }
        });
      }

      function showToast(message, ok = true) {
        if (!elements.toast) return;
        elements.toast.textContent = message;
        elements.toast.style.background = ok ? "rgba(10, 122, 255, 0.9)" : "rgba(220, 68, 55, 0.9)";
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
        if (!ts) return "--";
        const date = new Date(ts * 1000);
        return date.toLocaleString("zh-CN", { hour12: false });
      }

      function formatRemaining(expiresAt, serverTime) {
        if (!expiresAt) return "--";
        const now = serverTime ? serverTime : Math.floor(Date.now() / 1000);
        const diff = Math.max(expiresAt - now, 0);
        const days = Math.floor(diff / 86400);
        const hours = Math.floor((diff % 86400) / 3600);
        const mins = Math.floor((diff % 3600) / 60);
        if (days > 0) return String(days) + "\u5929" + String(hours) + "\u5c0f\u65f6";
        if (hours > 0) return String(hours) + "\u5c0f\u65f6" + String(mins) + "\u5206\u949f";
        return String(mins) + "\u5206\u949f";
      }

      function setActivationLock(locked) {
        document.body.classList.toggle("locked", locked);
        if (elements.activationModal) {
          elements.activationModal.classList.toggle("show", locked);
        }
      }

      function setActivationError(message) {
        if (elements.activationError) {
          elements.activationError.textContent = message || "";
        }
        if (elements.modalError) {
          elements.modalError.textContent = message || "";
        }
      }

      function updateActivation(data) {
        if (!data || data.success === false) {
          elements.activationPill.textContent = i18n.statusError;
          elements.activationPill.style.background = "rgba(220, 68, 55, 0.12)";
          elements.activationPill.style.color = "#b42318";
          setActivationError((data && data.error) || i18n.statusError);
          setActivationLock(true);
          return;
        }

        if (!data.activated) {
          elements.activationPill.textContent = i18n.statusInactive;
          elements.activationPill.style.background = "rgba(245, 158, 11, 0.18)";
          elements.activationPill.style.color = "#b45309";
          elements.activationExpiry.textContent = "--";
          elements.activationRemaining.textContent = "--";
          elements.deviceId.textContent = data.deviceId || "--";
          const msg = data.error === "unauthorized" ? i18n.activationUnauthorized : (data.error || i18n.activationLockedTip);
          setActivationError(msg);
          setActivationLock(true);
          if (elements.unbindBtn) {
            elements.unbindBtn.disabled = true;
          }
          return;
        }

        elements.unbindBtn.disabled = false;
        elements.activationExpiry.textContent = formatTimestamp(data.expiresAt);
        elements.activationRemaining.textContent = formatRemaining(data.expiresAt, data.serverTime);
        elements.deviceId.textContent = data.deviceId || "--";

        if (!data.active) {
          elements.activationPill.textContent = i18n.statusExpired;
          elements.activationPill.style.background = "rgba(220, 68, 55, 0.12)";
          elements.activationPill.style.color = "#b42318";
          setActivationError(i18n.activationExpiredTip);
          setActivationLock(true);
          return;
        }

        elements.activationPill.textContent = i18n.statusActive;
        elements.activationPill.style.background = "rgba(10, 122, 255, 0.18)";
        elements.activationPill.style.color = "#0a7aff";
        setActivationError("");
        setActivationLock(false);
      }
`)
	builder.WriteString(`
      function formatAccountStatus(status) {
        const key = (status || "").toLowerCase();
        if (statusLabels[key]) {
          return { label: statusLabels[key], tone: key };
        }
        if (status) {
          return { label: status, tone: "unknown" };
        }
        return { label: "--", tone: "unknown" };
      }

      function updateBadge(el, label, tone) {
        if (!el) return;
        el.textContent = label || "--";
        el.classList.remove("tone-good", "tone-warn", "tone-bad");
        if (tone === "normal" || tone === "available") {
          el.classList.add("tone-good");
        } else if (tone === "banned" || tone === "error") {
          el.classList.add("tone-bad");
        } else if (tone) {
          el.classList.add("tone-warn");
        }
      }

      function updateAccounts(data) {
        if (!data || data.success === false) return;
        const current = data.currentAccount || null;
        elements.currentEmail.textContent = current?.email || "--";
        const statusInfo = formatAccountStatus(current?.status);
        updateBadge(elements.currentStatus, statusInfo.label, statusInfo.tone);
        updateBadge(elements.currentType, current?.type || "--", current?.type ? "available" : "");

        const quota = current?.quota || 0;
        const used = current?.used || 0;
        const remaining = Math.max(quota - used, 0);
        elements.currentQuota.textContent = String(used) + " / " + String(quota);
        elements.currentRemaining.textContent = quota > 0 ? String(remaining) : "--";
        elements.currentRefresh.textContent = current?.nextRefreshTime || "--";

        const stats = data.stats || {};
        const assigned = stats.assigned_total || data.accountCount || data.localAccounts?.length || 0;
        elements.assignedCount.textContent = assigned;
        elements.switchCount.textContent = data.switchCount ?? 0;
        elements.totalQuota.textContent = stats.total_quota ?? "--";
        elements.totalUsed.textContent = stats.total_used ?? "--";
        elements.totalVirtualUsed.textContent = data.totalVirtualUsed ?? "--";
      }

      function updateGatewayStatus(data) {
        if (!data || data.success === false) return;
        if (data.running) {
          const portLabel = data.port ? " [" + data.port + "] " : " ";
          elements.gatewayStatus.textContent = i18n.labelGateway + portLabel + "\u8fd0\u884c\u4e2d";
          elements.gatewayStatusBadge.textContent = i18n.labelGateway + "\u8fd0\u884c\u4e2d";
          elements.gatewayStatusBadge.classList.add("running");
          elements.gatewayStatusBadge.classList.remove("stopped");
        } else {
          elements.gatewayStatus.textContent = i18n.labelGateway + "\u5df2\u505c\u6b62";
          elements.gatewayStatusBadge.textContent = i18n.labelGateway + "\u5df2\u505c\u6b62";
          elements.gatewayStatusBadge.classList.add("stopped");
          elements.gatewayStatusBadge.classList.remove("running");
        }
      }

      function updateWarpStatus(data) {
        if (!data || data.success === false) return;
        if (data.running) {
          elements.warpStatus.textContent = i18n.labelWarp + "\u8fd0\u884c\u4e2d";
          elements.warpStatusBadge.textContent = i18n.labelWarp + "\u8fd0\u884c\u4e2d";
          elements.warpStatusBadge.classList.add("running");
          elements.warpStatusBadge.classList.remove("stopped");
        } else {
          elements.warpStatus.textContent = i18n.labelWarp + "\u5df2\u505c\u6b62";
          elements.warpStatusBadge.textContent = i18n.labelWarp + "\u5df2\u505c\u6b62";
          elements.warpStatusBadge.classList.add("stopped");
          elements.warpStatusBadge.classList.remove("running");
        }
        if (data.path) {
          elements.warpPath.value = data.path;
        }
      }

      function updateBackupStatus(defaultStatus, mcpStatus) {
        if (defaultStatus?.hasBackup) {
          const time = defaultStatus.createdAt ? new Date(defaultStatus.createdAt).toLocaleString("zh-CN", { hour12: false }) : "";
          elements.defaultBackupStatus.textContent = i18n.backupStatusYes + (time ? " (" + time + ")" : "");
          elements.defaultBackupStatus.style.color = "#16a34a";
        } else {
          elements.defaultBackupStatus.textContent = i18n.backupStatusNo;
          elements.defaultBackupStatus.style.color = "#64748b";
        }
        if (mcpStatus?.backups && mcpStatus.backups.length > 0) {
          const backup = mcpStatus.backups[0];
          const time = backup.backupTime ? new Date(backup.backupTime).toLocaleString("zh-CN", { hour12: false }) : "";
          elements.mcpBackupStatus.textContent = i18n.backupStatusYes + (time ? " (" + time + ")" : "");
          elements.mcpBackupStatus.style.color = "#16a34a";
        } else {
          elements.mcpBackupStatus.textContent = i18n.backupStatusNo;
          elements.mcpBackupStatus.style.color = "#64748b";
        }
      }

      function setNoticeLink(el, message, link) {
        if (!el) return;
        el.textContent = message;
        if (link) {
          el.setAttribute("href", link);
          el.setAttribute("target", "_blank");
          el.setAttribute("rel", "noopener");
          el.classList.add("linked");
        } else {
          el.removeAttribute("href");
          el.removeAttribute("target");
          el.removeAttribute("rel");
          el.classList.remove("linked");
        }
      }

      function updateNotice(data) {
        if (!elements.noticeBar || !elements.noticeTrack || !elements.noticeWindow) return;
        const notice = data?.notice || null;
        if (!data || data.success === false || !notice || !notice.enabled || !notice.message) {
          elements.noticeBar.classList.remove("show", "scroll");
          return;
        }
        const message = String(notice.message || "").trim();
        if (!message) {
          elements.noticeBar.classList.remove("show", "scroll");
          return;
        }
        elements.noticeBar.classList.add("show");
        setNoticeLink(elements.noticeLink, message, notice.link);
        setNoticeLink(elements.noticeLinkClone, message, notice.link);
        requestAnimationFrame(() => {
          const needsScroll = elements.noticeTrack.scrollWidth > elements.noticeWindow.clientWidth;
          elements.noticeBar.classList.toggle("scroll", needsScroll);
          if (needsScroll) {
            const distance = elements.noticeTrack.scrollWidth / 2;
            elements.noticeTrack.style.setProperty("--notice-distance", distance + "px");
          } else {
            elements.noticeTrack.style.removeProperty("--notice-distance");
          }
        });
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
        const [activation, accounts, gateway, warp, notice, defaultStatus, mcpStatus] = await Promise.all([
          apiGet("/api/activation/status").catch(() => ({})),
          apiGet("/api/accounts").catch(() => ({})),
          apiGet("/api/gateway/status").catch(() => ({})),
          apiGet("/api/warp/status").catch(() => ({})),
          apiGet("/api/notice").catch(() => ({})),
          apiGet("/api/default/status").catch(() => ({})),
          apiGet("/api/mcp/backups").catch(() => ({}))
        ]);
        updateActivation(activation);
        updateAccounts(accounts);
        updateGatewayStatus(gateway);
        updateWarpStatus(warp);
        updateNotice(notice);
        updateBackupStatus(defaultStatus, mcpStatus);
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
`)
	builder.WriteString(`
      elements.activateBtn.addEventListener("click", async () => {
        const code = elements.codeInput.value.trim();
        if (!code) {
          showToast(i18n.toastInputCode, false);
          setActivationError(i18n.toastInputCode);
          return;
        }
        const res = await apiPost("/api/activation/login", { code });
        if (!res.success) {
          const msg = res.error || i18n.toastActivationFailed;
          showToast(msg, false);
          setActivationError(msg);
          return;
        }
        showToast(i18n.toastActivationSuccess);
        elements.codeInput.value = "";
        setActivationError("");
        await loadAll();
      });

      elements.codeInput.addEventListener("keydown", (event) => {
        if (event.key === "Enter") {
          elements.activateBtn.click();
        }
      });

      elements.unbindBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/activation/unbind");
        if (!res.success) {
          const msg = res.error || i18n.toastUnbindFailed;
          showToast(msg, false);
          setActivationError(msg);
          return;
        }
        showToast(i18n.toastUnbindSuccess);
        await loadAll();
      });

      elements.refreshBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/accounts/refresh", {});
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
        } else {
          showToast(i18n.toastRefreshDone);
        }
        await loadAll();
      });

      elements.switchBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/accounts/switch", {});
        if (!res.success) {
          showToast(res.error || i18n.toastSwitchFailed, false);
          return;
        }
        showToast(i18n.toastSwitchSuccess);
        await loadAll();
      });

      elements.autoDetectBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/warp/path/auto");
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
          return;
        }
        elements.warpPath.value = res.path;
        showToast(i18n.toastPathAutoSuccess);
      });

      elements.savePathBtn.addEventListener("click", async () => {
        const path = elements.warpPath.value.trim();
        if (!path) {
          showToast(i18n.toastInputPath, false);
          return;
        }
        const res = await apiPost("/api/warp/path", { path });
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
          return;
        }
        showToast(i18n.toastPathSaved);
      });

      elements.warpStartBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/warp/start");
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
        } else {
          showToast(i18n.toastStartWarpSuccess);
        }
        await loadAll();
      });

      elements.warpStopBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/warp/stop");
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
        } else {
          showToast(i18n.toastStopWarpSuccess);
        }
        await loadAll();
      });

      elements.gatewayStartBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/gateway/start");
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
        } else {
          showToast(i18n.toastStartGatewaySuccess);
        }
        await loadAll();
      });

      elements.gatewayStopBtn.addEventListener("click", async () => {
        const res = await apiPost("/api/gateway/stop");
        if (!res.success) {
          showToast(res.error || i18n.toastRefreshFailed, false);
        } else {
          showToast(i18n.toastStopGatewaySuccess);
        }
        await loadAll();
      });

      elements.clearLogBtn.addEventListener("click", () => {
        elements.logLines.innerHTML = "";
      });

      elements.backupAllBtn.addEventListener("click", async () => {
        showToast(i18n.toastBackuping);
        const [defaultRes, mcpRes] = await Promise.all([
          apiPost("/api/default/backup").catch(() => ({ success: false })),
          apiPost("/api/mcp/backup").catch(() => ({ success: false }))
        ]);
        const errors = [];
        if (!defaultRes.success) errors.push("Default");
        if (!mcpRes.success) errors.push("MCP");
        if (errors.length === 0) {
          showToast(i18n.toastBackupSuccess);
        } else {
          showToast(i18n.toastBackupFailed + ": " + errors.join(", "), false);
        }
        await loadAll();
      });

      elements.restoreAllBtn.addEventListener("click", async () => {
        if (!confirm("\u786e\u5b9a\u8981\u8fd8\u539f\u6240\u6709\u5907\u4efd\u5417\uff1f")) return;
        showToast(i18n.toastRestoring);
        const [defaultRes, mcpRes] = await Promise.all([
          apiPost("/api/default/restore").catch(() => ({ success: false })),
          apiPost("/api/mcp/restore").catch(() => ({ success: false }))
        ]);
        const errors = [];
        if (!defaultRes.success) errors.push("Default");
        if (!mcpRes.success) errors.push("MCP");
        if (errors.length === 0) {
          showToast(i18n.toastRestoreSuccess);
        } else {
          showToast(i18n.toastRestoreFailed + ": " + errors.join(", "), false);
        }
        await loadAll();
      });

      applyI18n();
      loadAll();
      initLogs();
      setInterval(loadAll, 20000);
    </script>
  </body>
</html>
`)
	embeddedUIHTML = builder.String()
}

func serveEmbeddedUI(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, embeddedUIHTML)
}
