# 手动添加 MCP 配置到 Warp 客户端

## 📋 配置文件位置

已生成配置文件：`d:\项目\warp-wg\skills_mcp_config.json`

## 🔧 手动添加步骤

### 方法 1：通过 Warp 客户端 UI 添加

1. **打开 Warp 客户端**

2. **进入 MCP 设置**
   - 点击右上角的设置图标（齿轮）
   - 选择 "Settings"
   - 在左侧菜单中找到 "MCP" 或 "Model Context Protocol"

3. **添加新的 MCP 服务器**
   - 点击 "Add MCP Server" 或 "+" 按钮
   - 选择 "Custom" 或 "Manual Configuration"

4. **配置第一个 MCP：skills-echo**
   ```
   Name: skills-echo
   Command: python
   Arguments: C:/Users/Administrator/skills/echo_server.py
   Working Directory: C:/Users/Administrator/skills
   ```

5. **重复步骤 4，添加其他 MCP**
   
   **skills-math:**
   ```
   Name: skills-math
   Command: python
   Arguments: C:/Users/Administrator/skills/math_server.py
   Working Directory: C:/Users/Administrator/skills
   ```
   
   **skills-time:**
   ```
   Name: skills-time
   Command: python
   Arguments: C:/Users/Administrator/skills/time_server.py
   Working Directory: C:/Users/Administrator/skills
   ```

6. **保存并启用**
   - 保存每个 MCP 配置
   - 确保它们显示为"已安装"或"已启用"状态

---

### 方法 2：通过配置文件导入（如果 Warp 支持）

1. **查找 Warp 的 MCP 配置文件**
   - 通常位于：`%LOCALAPPDATA%\warp\Warp\data\` 或用户目录下
   - 可能的文件名：`mcp_config.json`、`settings.json` 等

2. **编辑或导入配置**
   - 将 `skills_mcp_config.json` 的内容复制到 Warp 的配置文件中
   - 或者查看 Warp 是否有"导入配置"功能

---

### 方法 3：使用 Warp CLI（如果可用）

如果 Warp 提供命令行工具，可能可以通过命令添加：

```bash
warp mcp add --config skills_mcp_config.json
```

---

## ✅ 验证步骤

添加完成后：

1. **检查 MCP 状态**
   - 在 Warp 的 MCP 设置页面，应该看到 3 个 skills MCP
   - 状态应该是"已安装"或"已启用"

2. **测试 MCP 功能**
   - 在 Warp 的 AI 对话中，尝试调用这些 MCP 的功能
   - 例如：让 AI 使用 echo、math 或 time 功能

3. **运行备份脚本**
   ```powershell
   go run backup_real_mcp.go
   ```
   - 这会备份你手动添加的 MCP 配置
   - 生成 `mcp_real_backup.json` 文件

4. **测试网关的 MCP 同步功能**
   - 启动网关
   - 触发账号切换
   - 检查 MCP 配置是否被保留

---

## 🔍 如果找不到添加 MCP 的入口

请告诉我：
1. 你的 Warp 客户端版本号
2. 在设置中能看到哪些选项
3. 是否有 "MCP"、"Extensions"、"Plugins" 等相关菜单

我可以根据具体情况提供更详细的指导。

---

## 📝 配置文件内容

`skills_mcp_config.json` 包含：

```json
{
  "skills-echo": {
    "command": "python",
    "args": ["C:/Users/Administrator/skills/echo_server.py"],
    "working_directory": "C:/Users/Administrator/skills"
  },
  "skills-math": {
    "command": "python",
    "args": ["C:/Users/Administrator/skills/math_server.py"],
    "working_directory": "C:/Users/Administrator/skills"
  },
  "skills-time": {
    "command": "python",
    "args": ["C:/Users/Administrator/skills/time_server.py"],
    "working_directory": "C:/Users/Administrator/skills"
  }
}
```

---

## 🎯 下一步

手动添加成功后，请告诉我：
1. MCP 是否成功显示在 Warp 客户端中
2. 运行 `go run backup_real_mcp.go` 的结果
3. 我们可以继续测试网关的 MCP 备份/恢复功能

