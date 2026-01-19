# Warp Gateway 开发指南

本文档为 Warp AI 提供项目开发上下文，帮助理解代码结构和开发规范。

## 常用命令

### 编译

**本地编译（当前系统/架构）**
```bash
go build -o warp-wg.exe .
```

**跨平台编译脚本**
```powershell
# PowerShell - 编译当前平台
pwsh -File scripts/build.ps1 -Target host -Arch amd64

# PowerShell - 编译所有平台
pwsh -File scripts/build.ps1 -Target all -Arch amd64

# 单文件编译
pwsh -File scripts/build-single.ps1 -OS host -Arch amd64
```

### 运行
```bash
go run .
```
启动后控制面板访问：`http://127.0.0.1:9530`

### 检查
```bash
go vet ./...
go test ./...
```

## 数据目录结构

### 目录位置
- 环境变量 `GATEWAY_DATA_DIR` 优先
- Windows: `<exe_dir>/data`
- macOS: `~/Library/Application Support/warp-gateway`
- Linux: `~/.config/warp-gateway`

### 核心文件
| 文件 | 说明 |
|------|------|
| `config.json` | 本地配置（设备ID、激活状态等）|
| `gateway_accounts.json` | 账号池快照 |
| `warp_config.json` | Warp 客户端路径配置 |
| `logs/` | 日志目录 |

### MCP 备份目录
`~/.warp-gateway/mcp_backups/` - 存储全局 MCP 配置备份（`global_mcp_mcp.json`）

## 项目架构

### 入口和协调器
- **main.go** - 程序入口，初始化路径、配置、日志，启动 App
- **app.go** - 中央协调器 `App`，管理配置、网关、Warp进程、账号

### HTTP API 服务
- **server.go** - 本地 HTTP API，绑定 `127.0.0.1:9530`
- **api_handlers.go** - API 处理函数

#### API 端点
| 路径 | 说明 |
|------|------|
| `/api/activation/*` | 激活管理 |
| `/api/accounts` | 账号列表 |
| `/api/accounts/switch` | 切换账号 |
| `/api/accounts/refresh` | 刷新账号状态 |
| `/api/gateway/*` | 网关生命周期 |
| `/api/warp/*` | Warp 客户端管理 |
| `/api/mcp/*` | MCP 配置管理 |
| `/api/logs/*` | 日志流 |

### 网关/代理层
- **gateway_service.go** - MITM 代理服务，使用 `go-mitmproxy`
- **gateway_control.go** - 网关启停逻辑
- **proxy_control.go** - 系统代理配置（注册表/networksetup/gsettings）

### Warp 客户端集成
- **warp_control.go** - Warp 客户端发现、启停、凭证更新
  - `findWarp()` - 定位 Warp 可执行文件
  - `updateWarpCredentials()` - 更新凭证（支持 DPAPI/Keychain/Keyring）
  - `resetMachineID()` - 重置机器标识
- **warp_transport.go** - 自定义 HTTP Transport，UTLS 支持
- **warp_api.go** - Warp GraphQL API 调用（用量查询）

### MCP 配置同步（全局）
- **mcp_control.go** - MCP 配置的读取、备份、恢复
  - `getMCPServers()` - 读取当前 MCP 服务器列表
  - `backupMCPConfig()` - 备份 MCP 配置到文件
  - `restoreMCPConfig()` - 从备份恢复 MCP 配置
  - `switchAccountWithMCP()` - 切换账号时自动同步 MCP

#### MCP 数据库表
| 表名 | 说明 |
|------|------|
| `mcp_server_installations` | MCP 服务器安装配置 |
| `mcp_environment_variables` | MCP 环境变量 |
| `active_mcp_servers` | 激活的 MCP 服务器 |

### 账号管理
- **accounts.go** - 账号数据结构
- **account_memory.go** - 内存中的账号快照
- **gateway_accounts.go** - 账号选择策略、状态追踪
- **remote_client.go** - 远程后端通信

### 规则注入
- **gateway_rules.go** - Warp AI 规则注入系统
  - 从 `warp_rules_config.json` 和 `warp.sqlite` 加载规则
  - 使用 `[[WARP_AI_RULES_BEGIN]]` / `[[WARP_AI_RULES_END]]` 标记

### 平台集成
- **tray.go** - 系统托盘（使用 systray 库）
- **process_control.go** - 进程管理
- **cert_control.go** - 证书安装

## 开发规范

### 文件组织
- 平台相关代码使用后缀：`*_windows.go`、`*_darwin.go`、`*_linux.go`、`*_other.go`
- 新功能优先扩展现有 `App` 方法

### 凭证处理流程
修改凭证相关逻辑时，检查完整链路：
1. `warp_control.go` - 文件位置和 OS 集成
2. `warp_api.go` - 远程 API 语义
3. `gateway_accounts.go` - 账号选择
4. `mcp_control.go` - MCP 配置同步

### MCP 同步机制
账号切换时自动执行（全局 MCP）：
1. 备份当前本地 MCP 配置 → `~/.warp-gateway/mcp_backups/global_mcp_mcp.json`
2. 更新 Warp 凭证
3. 恢复同一份全局 MCP 配置
4. 延迟多次重试恢复（防止 Warp 启动/同步覆盖）
5. 若备份中没有 active 列表，则自动激活所有已恢复的 MCP 服务器

## Warp 数据位置
| 平台 | 路径 |
|------|------|
| Windows | `%LOCALAPPDATA%\warp\Warp\data\` |
| macOS | `~/Library/Application Support/warp/Warp/data/` |
| Linux | `~/.local/share/warp/Warp/data/` |

主要文件：
- `warp.sqlite` - 主数据库
- `dev.warp.Warp-User` - 用户身份
- `dev.warp.Warp-AiApiKeys` - API 密钥
