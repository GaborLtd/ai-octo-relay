# Codex CLI Notes

這份文件整理目前在 `ai-octo-relay` 中使用 Codex CLI 時，已確認的行為、限制與容易踩到的坑。

## 建議配置

- `mode = oneshot`
- 優先用 `codex exec ...`
- thread 續聊時才考慮 native resume
- 不要把 PTY / persistent 當主流程

## 已知坑

### 1. PTY / interactive 模式不穩

目前已實際踩過：

- cursor position 讀取失敗
- terminal 相容性問題

典型症狀：

- CLI 啟動後直接退出
- 出現 `The cursor position could not be read within a normal duration`
- Slack 端收到很多 terminal control / 雜訊

建議：

- 預設維持 `oneshot`
- 不要把 Codex persistent session 當穩定方案

### 2. model 可以指定，但要靠 CLI flag

Codex CLI 支援：

- `--model <name>`

在本專案中，建議直接用：

```json
{
  "agents": {
    "codex": {
      "adapter": "codex",
      "model": "gpt-5.2-codex"
    }
  }
}
```

### 3. long-running request 不能卡住整個 bot

Codex 有時會執行很久。

目前 bot 端已改成：

- 不同 Slack event 可並行處理
- 不同 thread / command 不應被單一 Codex request 阻塞
- 同一個 session 同一時間只允許一個 prompt

### 4. Slack 不適合 raw transcript

Codex 原始輸出常包含：

- banner
- metadata
- tool log
- structured output 痕跡

目前 relay 會先做：

- noise cleanup
- Slack-friendly formatting
- 長訊息分段
