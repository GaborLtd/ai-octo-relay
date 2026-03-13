# Gemini CLI Notes

這份文件整理目前在 `ai-octo-relay` 中使用 Gemini CLI 時，已確認的行為、限制與容易踩到的坑。

Gemini 是目前三家 CLI 中最容易出現 session / model / server capacity 混淆的。

這裡提到的 DM，指的是 Slack 直接私訊 bot 的對話視窗（`channel_type = im`），不是在一般 channel 內用 `@bot` 提問。

## 建議配置

- `mode = oneshot`
- 預設 model 建議 `gemini-2.5-flash`
- 目前視為 `fresh oneshot`
- 不要再設定 `max_session_turns`
- 不建議把 Gemini 當最穩定主力 agent

## 已知坑

### 1. Gemini 3 常是 preview / auto route

Gemini CLI 的 `/model` 畫面會顯示：

- `Auto (Gemini 3)`
- `Auto (Gemini 2.5)`
- `Manual`

如果沒有明確指定 model，CLI 可能會走 Gemini 3 自動路由。

### 2. model 名稱與可用性容易混淆

Gemini CLI 會提示：

- `To use a specific Gemini model on startup, use the --model flag.`

注意：

- Gemini 3 系列常有 preview 型號
- 名稱要用官方實際存在的 model 名稱
- 即使名稱正確，preview / 新模型也可能因為權限或容量不可用

建議先用：

```json
{
  "agents": {
    "gemini": {
      "adapter": "gemini",
      "model": "gemini-2.5-flash"
    }
  }
}
```

### 3. 429 / capacity exhausted 很常見

目前已實際遇到：

- `429 RESOURCE_EXHAUSTED`
- `MODEL_CAPACITY_EXHAUSTED`
- `No capacity available for model ...`

這通常不是 relay 本身壞掉，而是 server 沒容量或 preview model 太擠。

### 4. session 管理目前改為 fresh oneshot

Gemini 在本專案中目前不再走 native session resume，而是固定 fresh oneshot：

- 不保存 / 不重用 Gemini native session
- `!session restart` 不會幫 Gemini 重接原生 session
- 同一個新請求就是一次新的 CLI 執行

因此目前實際建議是：

- 新問題直接開新 thread
- 若出現異常，優先直接重送或改用 `codex:` / `claude:`

### 5. `maxSessionTurns` 在本專案已停用

先前即使是新 thread，也可能在單次請求中因 Gemini CLI 內部流程、重試或工具呼叫打到 turn limit。

目前 relay 端已不再替 Gemini 注入 `maxSessionTurns`，所以：

- config 中不要再設定 `agents.gemini.max_session_turns`
- 若仍看到 `code: 53`
  - 比較像 Gemini CLI 自身的單次執行問題
  - 不是 Slack thread 太長
  - 也不是 relay 還在做 Gemini session resume

所以：

- `Reached max session turns`
- `code: 53`

不一定代表你的 Slack thread 太長或 project 檔案太多。
