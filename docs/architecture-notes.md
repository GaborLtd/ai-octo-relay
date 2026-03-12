# Architecture Notes

這份文件記錄目前專案的實際互動模型、設計決策、已知限制與後續修改時要注意的事項。

目標是讓之後修改功能的人，不需要重新從 log 與對話紀錄倒推整個系統。

## 1. 產品目標

這個專案不是要做多個 Slack bot，而是：

- 單一 Slack bot
- 多個本機 project
- 多個 AI CLI agent
- 透過 Slack 遠端操作家裡電腦上的 project

核心需求：

- `channel = project room`
- `thread = 單一問題 / 單一 session`
- 同一個 thread 中可以持續互動
- 可切換不同 AI agent
- 盡量使用本機已登入的 CLI 訂閱版，不依賴 API key

## 2. 互動模型

目前已確認的互動模型：

- Slack channel 固定綁定 project
- 使用者在 channel 根訊息提問時，bot 會自動在該訊息底下 thread 回覆
- 後續在同一個 thread 內的追問，會沿用同一個 session
- 不同 thread 與 command 不應被單一長時間 agent run 阻塞
- thread 建立時可決定 agent，建立後會鎖定
- 單一訊息可在新 session 建立時於開頭指定 agent name / alias
- thread 也可暫時覆寫 project，但這是例外，不是日常主流程

### 實際模型

```text
channel
  - Question A
    - thread replies => Session A
  - Question B
    - thread replies => Session B
```

### 設計理由

- channel 層只負責 project 入口，不承擔所有問題的共享上下文
- 每個 thread 是獨立問題，比較不會互相污染
- 這個模型比「channel 只有一個共享 session」更適合實際 Slack 使用

## 3. Project 綁定策略

目前專案已從「在 channel 手動切 project」改為：

- `channel_id -> project` 固定綁定

設定位置：

- `projects[].channel_ids`

例如：

```json
{
  "name": "backend-api",
  "path": "/Users/match/git/backend-api",
  "default_agent": "claude",
  "channel_ids": ["C1234567890"]
}
```

### 為什麼這樣改

原本使用 `!project use <name>` 切換 channel 預設 project，對 Slack 日常使用太複雜。

使用者真正想要的是：

- 進對的 channel
- 直接問問題

而不是每次先記得切 project。

### 目前規則

- channel 的 project 來自 config mapping
- thread 中仍可用 `!project use <name>` 暫時覆寫
- `!project clear` 只清 thread 覆寫，不會清 channel mapping

## 4. Agent 行為

目前支援：

- `codex`
- `claude`
- `gemini`

### codex

目前預設：

- `mode = oneshot`
- 走 `codex exec ...`
- 同一個 thread 續聊時優先走 `codex exec resume ...`

原因：

- `codex` 的互動 TUI 會讀取 cursor position
- 在某些 PTY / pseudo terminal 環境下，persistent 模式會因 terminal 相容性問題直接退出
- 曾實際遇到：
  - `The cursor position could not be read within a normal duration`

因此目前不建議對 `codex` 啟用 PTY persistent session，除非未來驗證新版 CLI 已可穩定運作。

### claude / gemini

目前主推：

- `mode = oneshot`
- 優先使用 CLI 原生 `resume` / `session-id` 能力延續上下文

理由：

- `gemini` 的 PTY/persistent 模式也曾實際噴出 terminal control sequence
- 對 Slack 這種聊天介面來說，machine-readable oneshot 更穩定

### Slack 訊息級 agent override

目前支援在新 session 建立時於訊息開頭指定 agent，例如：

```text
@bot claude: 幫我看這個問題
@bot sonnet: 幫我寫測試
```

規則：

- 第一個 token 主推 `agent:` 或 `alias:`
- 相容保留 `#agent` 或 `#alias`
- `selector` 會對應 agent 名稱或 `agents.<name>.aliases`
- 只在新 thread / 新 DM session 建立時生效
- 不會改掉 `projects[].default_agent`
- 若沒有指定，仍照 project default agent / global default agent fallback
- 裸字 `claude` / `gemini` 不會觸發 override
- 同一 thread / DM session 不能切換到別的 agent

## 5. Session 邊界

目前 session key 概念上由以下元素決定：

- channel
- thread
- project
- agent

這代表：

- 同一個 channel 的不同 thread 是不同 session
- 同一個 thread 如果切 agent，也會變成另一個 session
- 同一個 thread 如果覆寫 project，也會變成另一個 session

### 設計理由

這樣可以避免：

- 不同問題共用上下文
- 不同 agent 的上下文混在一起

### 同一 session 的並行限制

雖然不同 Slack event 現在可以並行處理，但同一個 `session key` 目前仍必須序列化：

- 同一個 thread / DM session 同一時間只允許一個 agent prompt 在跑
- 如果上一個 request 還沒完成，新的 request 會被直接拒絕並提示稍後再試

原因：

- `codex exec resume`
- `gemini --resume`
- `claude --session-id`

這些 native resume/session 機制都假設同一個 session 由單一路徑接續。
若同時把兩個 prompt 打到同一個 native session，很容易把上下文、回覆順序、或 session state 搞亂。

### 不同 session 的並行處理

bot 的 Slack event loop 現在不應再被單一長時間 agent run 卡住。

也就是說：

- `!help`
- 另一個 thread 的新問題
- 不同 DM / 不同 channel 的請求

都應能在另一個長時間 `codex` / `claude` / `gemini` 任務執行時繼續處理。

這是刻意設計，因為 Slack bot 作為入口不應被單一慢任務變成全域串行。

## 6. Slack 回覆策略

目前已做的整理：

- 預設 `quiet_by_default = true`
- 先回 `_收到，處理中..._`
- 不主動把中間 transcript 持續更新到 Slack
- 最終回覆前會做 noise cleanup
- 長回覆改成自動分段發送，不再直接 truncate

### 為什麼這樣做

直接把 CLI transcript 原封不動貼到 Slack，閱讀體驗很差，尤其：

- 會混入 tool logs
- 會混入 model banner / metadata
- 會有重複段落
- 太長時被截斷

### 現在的目標

Slack 應該更接近聊天工具，而不是 terminal 鏡像。

## 7. Slack Markdown 正規化

目前回覆在送到 Slack 前，會經過一層 Slack-friendly 處理：

- 移除常見 CLI 噪音
- 整理空行
- 儘量保留 code block 完整
- 避免回覆看起來像 raw markdown dump

### 不是完整 Markdown renderer

Slack 只支援有限的 markdown / mrkdwn。

因此系統目前做的是：

- 讓內容更適合 Slack 顯示
- 而不是完整保留 GitHub-flavored Markdown

## 8. Slack Event 注意事項

曾經踩到的重要問題：

- 在某些 channel 情況下，Slack 實際送進來的是 `message` event
- 而不是預期的 `app_mention`

因此現在的處理策略是：

- 支援 `app_mention`
- 也支援 `message` event 中包含 bot mention 的情況

否則會發生：

- bot 有連上 Slack
- DM 可用
- channel mention 卻完全沒反應

## 9. DM 與 Channel 差異

### DM

- 不強制 thread 模型
- 直接在 DM 中對話
- 可透過 `dm_read_only` 讓 DM 預設只做分析與回答，不做專案修改
- DM 也會鎖定單一 agent session；若要換 agent，應重開 session

目前的 DM read-only 實作不是只靠 prompt，而是會依 agent 注入對應的 CLI 限制：

- `codex`: read-only sandbox
- `claude`: plan permission mode
- `gemini`: plan approval mode + sandbox

### Channel

- 根訊息提問時，自動以該訊息的 `message_ts` 作為 thread root
- bot 應該回到該 thread 中

這是刻意設計，因為：

- DM 本身就是單一對話空間
- channel 則需要 thread 來隔離不同問題

## 10. Logging 設計

目前 log level：

- `trace`
- `debug`
- `info`
- `warn`
- `error`

## 11. Remote Command 模型

為了支援從 Slack 遠端做開發輔助，目前額外提供兩條能力：

- `!cmd`
- `!git`

### `!cmd`

`!cmd` 不是任意 shell。

它只會執行：

- `projects[].commands` 中預先定義的白名單 command

理由：

- 使用者需要遠端執行固定的開發動作
- 但不應直接把 bot 變成可任意執行 shell 的入口

### `!git`

`!git` 也不是完整 git wrapper。

目前只開放有限子集合：

- `status`
- `diff`
- `log`
- `branch`
- `show`
- `fetch`
- `pull`

並且限制可接受的參數，避免變成任意命令轉發。

### DM read-only 與 command

若 `dm_read_only = true`：

- DM 中不允許任何 `!cmd`
- DM 中不允許任何 `!git`

這是刻意設計，因為 project command、git、以及任何 project/OS scope 的操作，都應只在對應 channel/thread 中進行。

### 使用原則

- `info`: 日常運行可觀測性
- `debug`: 決策細節與排查資料
- `trace`: 最細的內部流程，例如 chunk preview

### 為什麼後來重調

一開始 `info` 太少，無法用來排查互動流程。
後來已調整為：

- 一般互動行為都應該能在 `info` 看到
- `debug` / `trace` 只負責更細節的診斷

## 11. Store 選型

目前 store 分成兩種：

- `StateStore`: 結構化資料，例如 channel/thread scope、thread active flag、native session id
- `EventStore`: append-only event/log，例如 session lifecycle、之後可能加入 usage / summary pipeline

原因：

- 結構化 state 目前保存的資料量仍小
- append-only 資料不應再硬塞回同一個結構化 state 檔
- 先用 JSON / JSONL 仍是實作與部署成本最低

但這不是長期綁死的決策。

如果未來開始加入以下資料，應優先考慮改成 SQLite：

- conversation history
- thread summary
- token / usage metadata
- 更大量的 session 清理、查詢、過期需求

建議方向：

- 保持 store abstraction 清楚
- `StateStore` 與 `EventStore` 分開演進
- JSON / JSONL 作為 MVP / fallback，SQLite 作為擴充型 store

### 目前實作

- `StateStore`
  - `json`
  - `sqlite`
- `EventStore`
  - `none`
  - `jsonl`
  - `sqlite`

目前預設配置仍建議：

- `state_store.type = json`
- `event_store.type = jsonl`

這樣部署最簡單，也足夠應付目前的 session / event 規模。

## 12. 已知限制

### 1. native session id 會跨 process restart 保留，但活體 process 不會

bot 重啟後，不會有背景 persistent process 恢復。
但同一個 `thread + project + agent` 已記錄的 CLI native session id 仍會保存在 state store。

### 2. PTY persistent 目前不建議當主流程

`codex` / `gemini` 都曾出現互動終端相容性或 terminal output 汙染問題。

### 3. Slack formatting 仍可再優化

目前已經比 raw CLI transcript 好很多，但仍不是最終型態。

未來仍可考慮：

- 更好的標題轉換
- 更精準的列表格式
- 更好的 code block 分段策略

### 4. 長文雖已分段，但仍可能有語意切點不完美

目前分段策略以安全送達為主，還不是語意最漂亮的切法。

## 13. 後續建議方向

如果之後要繼續演進，優先順序建議：

1. `!new` 指令
- 明確重開當前 thread session

2. `!diag` 指令
- 一次輸出目前 thread 的 project / agent / quiet / session 資訊

3. 更完整的 Slack mrkdwn normalizer
- 例如 `**bold** -> *bold*`
- heading 正規化

4. 改善 chunking
- 以語意段落切分，而不是只看長度

5. 視需要再評估新版 `codex` / `gemini` 是否值得恢復 persistent

## 14. 修改時優先看哪些檔案

### 使用者互動 / Slack 行為

- `internal/slackbot/bot.go`

### scope / project / agent / thread 決策

- `internal/app/service.go`

### project registry 與 channel mapping

- `internal/project/registry.go`

### agent 執行與 session

- `internal/agent/registry.go`

### config schema

- `internal/config/config.go`

### 狀態儲存

- `internal/store/json_store.go`
