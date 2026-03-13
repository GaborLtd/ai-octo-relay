# Claude CLI Notes

這份文件整理目前在 `ai-octo-relay` 中使用 Claude CLI 時，已確認的行為、限制與容易踩到的坑。

## 建議配置

- `mode = oneshot`
- 非互動輸出用 `-p`
- 輸出格式優先 `text`
- session 續聊優先用 `--session-id`

## 已知坑

### 1. 未登入時 CLI 直接拒絕

本地 `claude --help` 可以跑，但真正執行命令時，如果尚未登入，會直接出現：

- `Not logged in · Please run /login`

這不屬於 relay 邏輯錯誤，而是本機 Claude CLI 狀態問題。

### 2. model 可以指定

Claude CLI 支援：

- `--model <name>`

建議預設：

```json
{
  "agents": {
    "claude": {
      "adapter": "claude",
      "model": "claude-sonnet-4-20250514"
    }
  }
}
```

### 3. thread / session 邊界要一致

目前模型是：

- `thread = 單一問題 / 單一 session`

所以不要：

- 把多個不同問題塞進同一個 thread
- 在同一個已建立 session 的 thread 內切 agent

### 4. DM read-only 不是只靠 prompt

若 `dm_read_only = true`，Claude 端除了附加 read-only prompt，也會加上：

- `--permission-mode plan`
