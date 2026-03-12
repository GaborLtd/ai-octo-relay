# AGENTS.md

這份文件是本專案的維護規則與修改準則。

目標：

- 讓後續修改的人快速知道哪些行為是刻意設計
- 避免把目前已確認有效的互動模型改壞

## 1. 互動模型

本專案目前採用：

- `channel = project room`
- `thread = 單一問題 / 單一 session`

規則：

- channel 的 project 來自 `projects[].channel_ids`
- channel 根訊息第一次提問時，bot 會回到該訊息的 thread
- 同一個 thread 內的後續訊息，即使沒有再 mention bot，也應該被視為同一個 session 延續
- 不同 thread / 不同 command 不應被單一 agent CLI 長時間執行整體卡住
- 同一個 session 同一時間只允許一個 prompt 執行，避免 native resume/session-id 互撞
- thread 可以覆寫 agent
- thread 可以暫時覆寫 project，但這是例外用法

不要輕易改成：

- 一個 channel 共用單一 session
- 所有後續 thread 訊息都必須重新 mention bot
- 整個 bot 因為一個慢請求退化成全域串行

## 2. Project 規則

目前主模型是：

- `channel_id -> project`

也就是：

- 使用者進對的 channel 就對應到正確 project

日常使用不應依賴：

- 在 channel 根層手動 `!project use`

如果要修改 project 行為：

- 優先保留 `channel_ids` mapping
- thread override 可以保留，但不要把它變成主流程

## 3. Agent 規則

### codex

目前預設：

- `oneshot`
- `codex exec ...`
- thread 續聊時優先用 `codex exec resume`

原因：

- PTY/persistent 模式在目前驗證下不夠穩
- 已遇過 cursor / terminal 相容性問題

除非重新驗證新版 CLI，否則不要把 `codex` 預設改回 persistent。

### claude / gemini

目前主推：

- `mode = oneshot`
- 同一個 thread 優先靠 CLI 原生 `resume` 或 `session-id` 延續上下文

不要再把 PTY/persistent 當成主流程。

## 4. Slack 回覆規則

Slack 不是 terminal dump。

回覆應盡量：

- 簡潔
- 可讀
- 適合 thread 內持續閱讀

目前設計：

- `quiet_by_default = true`
- 先回「收到，處理中...」
- 不把整段 CLI transcript 即時灌進 Slack
- 最終回覆前做 noise cleanup
- 長回覆自動分段發送

不要輕易回退成：

- 直接把 CLI 原始輸出貼到 Slack
- 單純 truncate 長訊息

## 5. Slack Event 規則

Slack 不一定會對 channel mention 給 `app_mention`。

目前必須同時支援：

- `app_mention`
- `message` event 中含 bot mention

另外：

- 已建立 session 的 thread 中，後續非 mention 訊息也要能處理

## 6. Logging 規則

目前分級：

- `info`: 一般互動流程可觀測
- `debug`: 診斷細節
- `trace`: 最細節內部流程

修改 logging 時請保持：

- `info` 足以看懂 bot 正在做什麼
- `debug/trace` 才放大量內部細節

不要讓 `info` 過度安靜，否則遠端排查很困難。

## 7. 文件分工

- `README.md`
  - 最新、最重要、最常用的資訊
- `AGENTS.md`
  - 維護規則與修改準則
- `docs/architecture-notes.md`
  - 架構背景、設計理由、限制
- `docs/changelog.md`
  - 變更歷程與關鍵決策時間線

## 8. 修改前建議先看

如果要改：

- Slack routing / thread 行為
  - `internal/slackbot/bot.go`
- scope / session / project / agent 決策
  - `internal/app/service.go`
- project mapping
  - `internal/project/registry.go`
- agent execution / session
  - `internal/agent/registry.go`
- config schema
  - `internal/config/config.go`

## 9. 敏感資料

不要提交：

- `config.json`
- `.env`
- Slack tokens
- 本機狀態檔

公開樣板只使用：

- `config.example.json`

## 10. Remote Command 規則

目前允許兩種遠端命令：

- `!cmd`
- `!git`

維護原則：

- `!cmd` 只能執行 project config 內定義的白名單 command
- 不要把 `!cmd` 擴成任意 shell
- `!git` 只保留有限子命令與有限參數
- 若新增新的 `!git` 子命令，先確認是否會造成高風險或破壞性操作

`dm_read_only = true` 時：

- 應避免在 DM 中執行會改變專案狀態的命令
- 直接阻擋所有 `!cmd`
- 直接阻擋所有 `!git`
- project 與 OS scope 的操作只應在對應 channel/thread 中執行
