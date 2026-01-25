# Warp Default表备份还原功能使用说明

## 功能概述

此功能允许你备份Warp客户端数据库中的**Default表数据**，并在每次切换账号后自动还原，**无需重启Warp客户端**。

同时，**MCP规则注入**也已优化为在线热更新方式，在切换账号时自动还原MCP配置，全程不重启Warp。

## 使用场景

- **保持个性化配置**：备份你的Warp个性化设置（如AI模型配置、主题设置等）
- **无感切换账号**：切换账号时自动还原Default表，保持Warp客户端在线运行
- **配置迁移**：在不同账号间共享相同的Warp配置

## API接口

### 1. 备份Default表

```http
POST /api/default/backup
```

**功能**：备份当前Warp数据库中的Default表数据

**返回示例**：
```json
{
  "success": true,
  "message": "Default table backup created"
}
```

### 2. 手动还原Default表

```http
POST /api/default/restore
```

**功能**：手动还原之前备份的Default表数据

**返回示例**：
```json
{
  "success": true,
  "message": "Default table restored"
}
```

### 3. 查询备份状态

```http
GET /api/default/status
```

**功能**：查询是否存在Default表备份

**返回示例**：
```json
{
  "success": true,
  "hasBackup": true,
  "createdAt": "2026-01-25T03:15:00Z"
}
```

### 4. 删除备份

```http
POST /api/default/delete
```

**功能**：删除现有的Default表备份

**返回示例**：
```json
{
  "success": true
}
```

## 使用流程

### 步骤1：在控制台配置好Warp客户端

1. 打开Warp客户端
2. 配置你喜欢的AI模型、主题、设置等
3. 确保所有配置都符合你的需求

### 步骤2：备份Default表和MCP

#### 方法A：使用控制台（推荐）

1. 打开控制台 `http://127.0.0.1:8080`
2. 找到**“配置备份”**卡片
3. 点击**“备份全部”**按钮
4. 等待提示“备份成功！”

✅ 此方法会**同时备份Default表和MCP配置**

#### 方法B：使用API

```bash
# 备份Default表
curl -X POST http://127.0.0.1:8080/api/default/backup

# 备份MCP配置
curl -X POST http://127.0.0.1:8080/api/mcp/backup
```

### 步骤3：正常切换账号

之后每次切换账号（手动或自动），网关会自动：
1. 在线更新Warp数据库中的账号凭证
2. 延迟2秒后自动还原Default表备份
3. **全程不重启Warp客户端**

## 技术实现

### 在线更新机制

- 使用SQLite的**WAL模式**（Write-Ahead Logging）
- 允许在Warp运行时进行数据库更新
- 避免数据库锁定冲突

### 自动还原时机

1. **手动切换账号**：调用`/api/accounts/switch`后2秒
2. **无感切换账号**：网关检测到账号需要切换后2秒

### 数据安全

- 使用事务保证数据一致性
- 备份文件存储在：`data/default_table_backup.json`
- 自动处理列顺序和空值

## 注意事项

### ✅ 优势

1. **无需重启**：Warp客户端保持在线运行
2. **配置持久化**：个性化配置不会因切换账号而丢失
3. **自动化**：设置一次，永久生效

### ⚠️ 限制

1. **首次备份必须手动执行**：系统不会自动创建初始备份
2. **仅备份Default表**：其他表（如会话历史）不会备份
3. **Warp版本兼容性**：不同版本的Warp表结构可能不同

### 🔧 故障排查

如果还原后配置没有生效：

1. 检查日志：查看`data/logs/go-gateway.log`中的`[Default]`相关日志
2. 验证备份：调用`/api/default/status`确认备份存在
3. 手动还原：调用`/api/default/restore`手动触发还原
4. 数据库检查：确保`warp.sqlite`数据库存在且Default表结构正常

## 控制台界面说明

### 配置备份卡片

在控制台中，你会看到一个**“配置备份”**卡片，包含：

- **Default表备份状态**：显示是否已备份及备份时间
- **MCP备份状态**：显示MCP配置备份状态
- **备份全部按钮**：一键备份Default表和MCP配置
- **还原全部按钮**：手动还原所有备份（通常不需要，系统会自动还原）

### 使用流程

1. **首次配置**：
   - 配置好Warp的Default表和MCP规则
   - 点击“备份全部”
   
2. **后续使用**：
   - 切换账号时自动还原
   - 无需任何手动操作

3. **状态查看**：
   - 绿色：已备份
   - 灰色：未备份

## 示例脚本

### PowerShell示例

```powershell
# 备份Default表
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/default/backup" -Method Post

# 查询备份状态
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/default/status" -Method Get

# 手动还原
Invoke-RestMethod -Uri "http://127.0.0.1:8080/api/default/restore" -Method Post
```

### 前端集成示例

```javascript
// 备份Default表
async function backupDefaultTable() {
  const response = await fetch('/api/default/backup', { method: 'POST' });
  const data = await response.json();
  if (data.success) {
    console.log('备份成功');
  }
}

// 查询备份状态
async function checkBackupStatus() {
  const response = await fetch('/api/default/status');
  const data = await response.json();
  if (data.hasBackup) {
    console.log('备份已存在，创建时间:', data.createdAt);
  }
}
```

## MCP规则注入也支持热更新

除了Default表，**MCP服务器配置**也已优化为在线热更新方式：

### MCP自动备份还原

1. **自动备份**：在切换账号时，系统会自动备份当前账号的MCP配置
2. **自动还原**：切换到新账号后，自动还原该账号的MCP配置
3. **在线更新**：使用SQLite WAL模式，全程不重启Warp客户端

### MCP涉及的表

- `mcp_server_installations` - MCP服务器安装配置
- `mcp_environment_variables` - MCP环境变量
- `active_mcp_servers` - 激活MCP服务器列表
- `generic_string_objects` - MCP白名单（agent profiles）

### 技术特点

- 使用**DELETE + INSERT**方式替换MCP数据
- 设置`restore_running=1`触发Warp自动安装MCP
- 通过`generic_string_objects`同步MCP白名单

## 更新日志

- **2026-01-25 v2**：MCP规则注入也优化为在线热更新方式
- **2026-01-25 v1**：初始版本，支持在线备份还原Default表，无需重启Warp
