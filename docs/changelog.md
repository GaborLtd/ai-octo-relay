# Changelog

這份文件記錄專案的重要修改與設計決策歷程。

不是每次小改動都要記，但以下情況建議記：

- 互動模型改變
- session 規則改變
- Slack 回覆行為改變
- agent 預設模式改變
- config schema 改變

## 2026-03-12

### 初版 MVP 建立

- 建立 Go 版 Slack bot 骨架
- 支援 `codex` / `claude` / `gemini`
- 建立 config / store / project / session / Slack routing 基礎模組

### logging 分級建立

- 新增 `trace / debug / info / warn / error`
- 重新調整 `info`，讓日常互動流程可觀測

### codex 預設改為 oneshot

- `codex` 不再預設 persistent
- 原因是 interactive TUI 在 PTY/session 下不穩，曾出現 cursor 讀取失敗

### thread-centric 互動模型確立

- `channel = project room`
- `thread = 單一問題 / 單一 session`
- channel 根訊息提問時，自動在該訊息 thread 回覆

### thread 後續訊息可不再 mention

- 修正已建立 session 的 thread 中，後續非 mention 訊息不會被處理的問題
- 現在同一個 thread 內可以自然接續追問

### agent 收斂到 oneshot + native resume

- `claude` / `gemini` 預設也改成 `oneshot`
- 同一個 `thread + project + agent` 會記錄 CLI 原生 session id
- `codex` 優先用 `codex exec resume`
- `gemini` 優先用 `--resume`
- `claude` 預留 `--session-id` 路徑

原因：

- `codex` / `gemini` 的 PTY persistent 都曾實際噴出 terminal control sequence
- 對 Slack 來說，oneshot + 原生 resume 比 PTY TUI 穩定

### project 綁定模型改為 channel mapping

- 從「手動切換 channel project」改成 `projects[].channel_ids`
- thread 仍可做暫時覆寫

### Slack 回覆策略改善

- 啟用 quiet-first 模式
- 導入 noise cleanup
- 加入 Slack-friendly formatting
- 長回覆改為自動分段發送，不再單純 truncate

### 文件整理

- `README.md` 保留最新與最重要資訊
- 新增 `docs/architecture-notes.md`
- 新增 `AGENTS.md`
- 新增 `docs/changelog.md`

### store 選型決策補記

- 目前仍使用 JSON store
- 若未來 session/history 資料量變大，優先考慮切 SQLite

## 2026-03-12

### storage abstraction 拆分

- 新增 `StateStore` / `EventStore` 分離
- 結構化 state 預設維持 JSON backend
- append-only event 新增 `jsonl` backend
- 透過 `sqlite3` CLI 新增 SQLite backend，避免先引入額外 Go driver
- `store_path` 保留相容；新設定改用 `state_store` / `event_store`

### Remote command 白名單

- 新增 `projects[].commands`
- Slack 可用 `!cmd list` 與 `!cmd run <name>` 執行 project 白名單 command
- 目標是支援 remote 開發常用操作，但不開放任意 shell

### 基本 git 指令

- 新增 `!git status|diff|log|branch|show|fetch|pull`
- 只開放有限子集合與有限參數
- `pull` 固定為 `--ff-only`

### Gemini session exhaustion fallback

- 若 Gemini native session 命中 CLI 的 max session turns
- 系統會自動清掉舊的 native session id
- 並以新 session 重試一次，減少手動 `!session restart` 的需要

### DM read-only 延伸到命令

- 若 `dm_read_only = true`
- DM 中拒絕所有 `!cmd`
- DM 中拒絕所有 `!git`
- 明確把 project/OS scope 操作限制在對應 channel/thread
