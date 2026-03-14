# AGENTS.md

這份文件只保留目前仍重要的維護規則。

## 1. 互動模型

本專案的主模型是：

- `channel = project room`
- `thread = 單一問題 / 單一 session`

維護時不要破壞以下行為：

- channel 的 project 主要來自 `projects[].channel_ids`
- channel 根訊息提問時，bot 會回到該訊息的 thread
- 同一個 thread 內的後續訊息，不需要再次 mention bot
- 同一個 session 同時間只能跑一個 prompt
- 不同 thread / command 應可並行，不要退化成全域串行

## 2. Project 規則

- `channel_id -> project` 是主流程
- `!project use` 只應視為 thread override，不是日常主要操作
- `!project clear` 只清 override，不應影響 channel mapping

主要檔案：

- `internal/project/registry.go`
- `internal/app/service.go`

## 3. Agent 規則

- 目前預設以 `oneshot` 為主
- 同一個 thread 優先沿用 CLI 原生 `resume` / `session-id`
- agent selector 只在新 session 建立時決定 agent
- 已建立 session 的 thread / DM 不應在中途切換 agent

主要檔案：

- `internal/agent/registry.go`
- `internal/app/service.go`

## 4. Slack 行為

- 要同時支援 `app_mention` 與 `message` event 內的 bot mention
- 已建立 session 的 thread，後續非 mention 訊息也要能處理
- Slack 回覆應偏向簡潔可讀，不要回退成 terminal transcript dump
- `quiet_by_default` 模式下，先回簡短處理中訊息，再送最終結果

主要檔案：

- `internal/slackbot/bot.go`

## 5. Remote Commands

目前只保留兩種遠端命令：

- `!cmd`
- `!git`

維護原則：

- `!cmd` 只能執行 project config 內的白名單命令
- 不要把 `!cmd` 擴成任意 shell
- `!git` 只保留有限子命令與有限參數
- 新增任何會改變專案狀態的命令前，要先評估風險

`dm_read_only = true` 時：

- DM 中應阻擋 `!cmd`
- DM 中應阻擋 `!git`

## 6. Logging

- `info` 要足夠看懂 bot 正在做什麼
- 大量內部細節放在 `debug` / `trace`
- 不要把 `info` 降到幾乎無法排查

主要檔案：

- `internal/logx/logger.go`

## 7. CLI / Daemon 規則

- 正式 CLI 入口是 `cmd/ai-octo-relay` 與 `internal/relaycmd`
- `cmd/bot` 只保留相容舊用法，不要把新功能只加在 `cmd/bot`
- `serve` / `start` / `stop` / `restart` 的語意要一致，不要讓文件、help、實作彼此漂移
- 若調整 daemon 啟停、pid state、log path 或 config 搜尋順序，要同步檢查 `README.md`、`CONFIG.md` 與 `internal/relaycmd/cli_test.go`
- 若新增常用開發入口，可放在 `Makefile`，但應維持為薄封裝，不要繞過正式 CLI 行為

主要檔案：

- `cmd/ai-octo-relay/main.go`
- `cmd/bot/main.go`
- `internal/relaycmd/cli.go`

## 8. 敏感資料

不要提交：

- `config.json`
- `.env`
- Slack tokens
- 本機 state / event store 檔案

公開樣板使用：

- `config.example.json`

CLI 注意事項文件：

- `docs/codex-cli-notes.md`
- `docs/claude-cli-notes.md`
- `docs/gemini-cli-notes.md`
