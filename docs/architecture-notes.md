# Architecture Notes

這份文件描述目前系統怎麼運作，不追溯過多歷史決策。

## 1. 系統目標

`ai-octo-relay` 是一個單一 Slack bot，負責把 Slack 訊息轉成：

- 指定 project 內的 AI agent prompt
- project 白名單命令
- 有限制的 git 命令

核心使用情境是：人在 Slack 上，遠端操作本機上的多個專案。

## 2. 互動邊界

目前的 session 模型：

- `channel` 決定 project 入口
- `thread` 代表單一問題
- `session key = channel + thread/channel + project + agent`

結果：

- 同一 channel 的不同 thread 是不同 session
- thread 建立後，後續訊息會沿用相同 session
- 同一 session 不能並發執行
- 不同 session 可以並行

## 3. Project 與 Agent 決策

project 決策順序：

1. thread override
2. `projects[].channel_ids`
3. 第一個可用 project

agent 決策順序：

1. 新訊息開頭的 agent selector
2. thread / channel state
3. project `default_agent`
4. global `default_agent`
5. 第一個可用 agent

agent selector 支援：

- `gemini:`
- `#gemini`
- alias，例如 `sonnet:`

## 4. Slack 處理流程

大致流程：

1. Slack event 進入 `internal/slackbot/bot.go`
2. 判斷是 command、一般 prompt、thread continuation、DM
3. 透過 `internal/app/service.go` resolve scope
4. 執行 agent / project command / git command
5. 將結果整理成 Slack 友善格式後回覆

目前重要行為：

- channel mention 可能來自 `app_mention` 或 `message`
- channel 根訊息提問時，bot 會在該訊息 thread 回覆
- quiet mode 下只顯示簡短狀態與最終結果

## 5. Agent 執行模型

目前 agent 由 `internal/agent/registry.go` 統一管理。

現況：

- 預設以 `oneshot` 為主
- 優先使用各 CLI 原生的 session / resume 能力
- session 資訊由 state store 保存
- 單一 session 加鎖，避免同時打到同一個 native session

目前三家 agent 的 session 策略：

- `codex`
  - `oneshot + native resume`
- `claude`
  - `oneshot + native session-id`
- `gemini`
  - `fresh oneshot`
  - 目前不再保存 / 重用 native session
  - 目前也不再注入 `maxSessionTurns`
  - 原因是先前整合邏輯與實際 CLI 行為不一致，容易誤判成 session 問題

目前內建 agent：

- `codex`
- `claude`
- `gemini`

### model list 來源

`!agent model list <name>` 目前不再只是靜態提示，而是依 agent 嘗試以下來源：

- `codex`
  - OpenAI API
  - `~/.codex/models_cache.json`
  - fallback 清單
- `claude`
  - Anthropic API
  - fallback 清單
- `gemini`
  - Google Gemini models API
  - fallback 清單

## 6. Remote Commands

除了 agent prompt 外，系統還支援兩種受限操作：

- `!cmd`: 執行 project 定義好的白名單命令
- `!git`: 執行固定子命令集合

這些命令都透過 `internal/app/service.go` 驗證與執行。

`dm_read_only = true` 時，只有 Slack 直接私訊 bot 的 DM（`channel_type = im`）會拒絕這兩類命令。

在一般 channel / private channel 中使用 `@bot` 提問，不算 DM。

## 7. 儲存層

儲存分成兩塊：

- `state_store`: thread / channel / session 狀態
- `event_store`: event 記錄

支援類型：

- `state_store`: `json`、`sqlite`
- `event_store`: `none`、`jsonl`、`sqlite`

相關實作在：

- `internal/store/factory.go`
- `internal/store/*.go`

## 8. 修改入口

常見調整位置：

- Slack routing / reply：`internal/slackbot/bot.go`
- scope / command / session 規則：`internal/app/service.go`
- project mapping：`internal/project/registry.go`
- agent 執行：`internal/agent/registry.go`
- config schema：`internal/config/config.go`
