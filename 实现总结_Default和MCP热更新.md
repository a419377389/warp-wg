# Warp Default表和MCP配置热更新实现总结

## 🎯 实现目标

实现无需重启Warp客户端的Default表和MCP配置备份还原功能，支持账号无感切换。

## ✅ 已完成功能

### 1. **Default表热更新**
- ✅ 使用SQLite WAL模式实现在线热更新
- ✅ 备份/还原/查询/删除 4个完整API接口
- ✅ 账号切换后自动还原（延迟2秒）
- ✅ 全程不重启Warp客户端

### 2. **MCP配置热更新**
- ✅ 优化所有数据库连接为WAL模式
- ✅ 支持在线热更新MCP规则
- ✅ 账号切换后自动还原MCP配置
- ✅ 全程不重启Warp客户端

### 3. **控制台UI**
- ✅ 添加"配置备份"卡片（需要手动应用补丁）
- ✅ 显示Default表和MCP备份状态
- ✅ 一键备份全部按钮
- ✅ 手动还原全部按钮

## 📂 修改的文件清单

| 文件 | 修改内容 | 状态 |
|------|---------|------|
| `warp_control.go` | 添加Default表备份/还原函数（WAL模式） | ✅ 完成 |
| `mcp_control.go` | 所有数据库连接改为WAL模式 | ✅ 完成 |
| `api_handlers.go` | 添加4个Default表API + 修改切换逻辑 | ✅ 完成 |
| `server.go` | 注册Default表路由 | ✅ 完成 |
| `app.go` | 添加辅助方法 | ✅ 完成 |
| `gateway_service.go` | 修改自动切换逻辑为热更新 | ✅ 完成 |
| `ui_embedded.go` | 添加备份功能UI | 📝 需手动应用 |
| `web/index.html` | 外部UI测试文件 | ✅ 完成 |
| `web/app.js` | 外部UI测试文件 | ✅ 完成 |

## 🔧 技术实现要点

### SQLite WAL模式

```go
sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_timeout=5000")
db.SetMaxOpenConns(1)
db.SetMaxIdleConns(1)
```

**优势**：
- 支持并发读写
- 避免数据库锁定冲突
- Warp运行时可安全更新数据库

### 自动还原时机

1. **手动切换账号** (`/api/accounts/switch`):
   - 更新凭证
   - 延迟2秒后自动还原Default表
   - 可选是否重启Warp

2. **无感切换账号** (`gateway_service.go`):
   - 网关检测到需要切换
   - 更新凭证
   - 延迟2秒后自动还原Default表
   - 全程不重启Warp

### 数据流程

```
配置Warp → 备份Default+MCP → 切换账号 → 自动还原 → Warp无感更新
```

## 📡 API接口

### Default表相关

| 接口 | 方法 | 功能 |
|------|------|------|
| `/api/default/backup` | POST | 备份Default表 |
| `/api/default/restore` | POST | 还原Default表 |
| `/api/default/status` | GET | 查询备份状态 |
| `/api/default/delete` | POST | 删除备份 |

### MCP相关

| 接口 | 方法 | 功能 |
|------|------|------|
| `/api/mcp/backup` | POST | 备份MCP配置 |
| `/api/mcp/restore` | POST | 还原MCP配置 |
| `/api/mcp/backups` | GET | 查询MCP备份 |
| `/api/mcp/backup/delete` | POST | 删除MCP备份 |

## 🎨 控制台UI（需手动应用）

由于控制台是内嵌在`ui_embedded.go`中的单文件HTML+CSS+JS，需要手动添加备份功能。

### 应用方法

参考 `控制台备份功能补丁.md` 文件，按照以下步骤修改 `ui_embedded.go`：

1. **第790行**：添加备份卡片HTML
2. **第891行**：添加elements定义
3. **第1135行**：添加updateBackupStatus函数
4. **第1191行**：修改loadAll函数
5. **第1342行**：添加按钮事件处理

### 预期效果

- 界面新增"配置备份"卡片
- 显示Default表和MCP备份状态（绿色=已备份，灰色=未备份）
- 点击"备份全部"同时备份两者
- 点击"还原全部"手动触发还原

## 📖 使用说明

### 首次设置

1. 打开Warp客户端，配置好：
   - Default表（AI模型、主题等）
   - MCP服务器规则

2. 打开控制台 `http://127.0.0.1:8080`

3. 找到"配置备份"卡片，点击**"备份全部"**

4. 等待提示"备份成功！"

### 日常使用

- 切换账号（手动或自动）后，系统会自动还原配置
- 无需任何手动操作
- Warp客户端全程运行，无感知

## 🐛 故障排查

### 如果还原后配置没有生效

1. **检查日志**：
   ```
   查看 data/logs/go-gateway.log
   搜索 [Default] 或 [MCP] 相关日志
   ```

2. **验证备份**：
   ```bash
   curl http://127.0.0.1:8080/api/default/status
   ```

3. **手动还原**：
   ```bash
   curl -X POST http://127.0.0.1:8080/api/default/restore
   ```

4. **检查数据库**：
   - 确认 `warp.sqlite` 存在
   - 确认Default表存在且有数据

### 常见问题

**Q: 切换账号后需要多久生效？**
A: 延迟2秒自动还原，通常3-5秒内生效

**Q: 需要重启Warp吗？**
A: 不需要，全程热更新

**Q: MCP规则也会自动还原吗？**
A: 是的，Default表和MCP配置都会自动还原

**Q: 备份文件存放在哪里？**
A: 
- Default表：`data/default_table_backup.json`
- MCP配置：`~/.warp-gateway/mcp_backups/global_mcp_mcp.json`

## 📝 文档清单

| 文档 | 说明 |
|------|------|
| `Default表备份还原说明.md` | 完整使用指南 |
| `控制台备份功能补丁.md` | UI修改步骤 |
| `实现总结_Default和MCP热更新.md` | 本文档 |

## 🚀 后续优化建议

1. **自动定时备份**：定期自动备份，防止配置丢失
2. **多版本备份**：支持保存多个历史版本
3. **导入导出**：支持备份文件的导入导出
4. **选择性还原**：可单独选择还原Default或MCP
5. **云端同步**：支持将备份同步到云端

## 🎉 总结

现在你可以：

1. ✅ **一次配置**：在Warp中设置好Default表和MCP规则
2. ✅ **一键备份**：点击控制台"备份全部"按钮
3. ✅ **无限切换**：手动或自动切换账号，全程不关闭Warp
4. ✅ **自动恢复**：配置自动还原，无感知

**完全实现了无感切换账号，保持Warp在线运行的需求！** 🎊
