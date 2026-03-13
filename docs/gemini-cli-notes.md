# Gemini CLI Notes

這份文件整理目前在 `ai-octo-relay` 中使用 Gemini CLI 時，已確認的行為、限制與容易踩到的坑。

Gemini 是目前三家 CLI 中最容易出現 session / model / server capacity 混淆的。

## 建議配置

- `mode = oneshot`
- 預設 model 建議 `gemini-2.5-flash`
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

### 4. session 管理目前不夠可靠

Gemini 在本專案中目前最需要小心的是 session 邏輯：

- config 若自訂 `args`，native resume 不一定真的生效
- store 仍可能保存 native session id
- 錯誤摘要又可能把 CLI 內部錯誤翻成 `max turns`

因此目前實際建議是：

- 不要過度依賴 Gemini native session resume
- 新問題直接開新 thread
- 若出現 session / turn 異常，優先 `!session restart`

### 5. `maxSessionTurns` 不代表一定是 thread 太長

即使是新 thread，也可能在單次請求中因 Gemini CLI 內部流程、重試或工具呼叫打到 turn limit。

所以：

- `Reached max session turns`
- `code: 53`

不一定代表你的 Slack thread 太長或 project 檔案太多。
