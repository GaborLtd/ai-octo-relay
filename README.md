# ai-octo-relay

單一 Slack bot，多 project、多 AI CLI agent 的遠端控制 MVP。

目前支援的核心能力：

- 單一 Slack bot 接收訊息
- 多個本機 project registry
- 以 `channel_id -> project` 固定綁定 channel 與 project
- thread 可覆寫 project / agent
- 透過本機 CLI 執行 `codex` / `claude` / `gemini`
- persistent session，可在同一個 thread 持續互動
- 使用 JSON 檔持久化狀態

## MVP 範圍

這一版刻意保持簡單：

- 不做 multiple bot relay
- 不做多平台
- session 狀態不跨 process restart 保留
- 長駐 session 使用「輸出 idle」判斷單回合結束

這樣可以先滿足「人在外面，用手機從 Slack 控制家裡電腦上的專案」。

## 指令

在 DM 或提及 bot 後使用：

```text
!help
!status
!project list
!project current
!project use <name>
!project clear
!agent list
!agent use <name>
!agent clear
!session status
!session restart
!session close
!quiet
!quiet on
!quiet off
!reset
```

規則：

- channel project 建議直接在 config 用 `projects[].channel_ids` 綁定
- 在 thread 內執行 `!project use`，只更新該 thread 覆寫
- 在 channel 中直接提問時，bot 會自動用那則根訊息建立 thread session
- 同一個 thread 內的後續互動會沿用同一個 session
- 若 agent 設為 `persistent`，同一個 `thread + project + agent` 會共用同一個長駐 session

## 設定

複製範例設定：

```bash
cp config.example.json config.json
```

可用環境變數覆寫 Slack token：

```bash
export SLACK_APP_TOKEN=xapp-...
export SLACK_BOT_TOKEN=xoxb-...
```

log 等級可在設定檔控制：

```json
"log_level": "info"
```

可用值：

- `trace`: 最細，包含 chunk preview、分段處理、非常細的內部流程
- `debug`: 診斷等級，包含 command/args、內部決策細節
- `info`: 預設，會輸出一般互動流程，適合平常觀察 bot 在做什麼
- `warn`
- `error`

另外可設定是否預設走安靜輸出模式：

```json
"quiet_by_default": true
```

`quiet` 模式比較接近 `cc-connect` 的聊天平台體驗：

- Slack 先只顯示「收到，處理中...」
- 不持續推送中間 transcript / tool progress
- 最終回覆前會清理常見 CLI 噪音
- 根訊息提問會自動在 thread 內回覆，閱讀會比較集中

## 執行

```bash
go run ./cmd/bot -config ./config.json
```

## Slack App

已提供 manifest：

```text
slack-manifest.yaml
```

建立方式：

1. 到 Slack 建立新 App
2. 選擇 `From an app manifest`
3. 貼上 [slack-manifest.yaml](/Users/match/git/ai-octo-relay/slack-manifest.yaml)
4. 安裝到 workspace
5. 取得 `xapp-...` 與 `xoxb-...` token

## 維護筆記

如果之後要修改互動模型、thread 行為、project mapping、agent session 或 Slack formatting，先看：

- [architecture-notes.md](/Users/match/git/ai-octo-relay/docs/architecture-notes.md)

## Config 結構

這個專案的設定檔是單一 JSON，啟動時用 `-config` 指定，預設讀 `config.json`。

重點欄位：

- `command_prefix`: Slack 指令前綴
- `default_agent`: 全域預設 agent
- `store_path`: 狀態檔路徑
- `log_level`: `trace / debug / info / warn / error`
- `quiet_by_default`: 是否預設安靜輸出
- `slack.app_token`
- `slack.bot_token`
- `slack.allowed_channels`
- `projects`
- `agents`

### projects

`projects` 至少要有一個，每個 project 主要欄位：

- `name`: project 名稱
- `path`: 專案實際路徑
- `default_agent`: 這個 project 的預設 agent
- `channel_ids`: 綁定這個 project 的 Slack channel IDs

建議用 `channel_ids` 直接把 channel 綁到 project，例如：

```json
{
  "name": "backend-api",
  "path": "/Users/match/git/backend-api",
  "default_agent": "claude",
  "channel_ids": ["C1234567890"]
}
```

這樣你在 `C1234567890` 那個 channel 問問題時，就會直接使用 `backend-api`。

### agents

`agents.<name>` 欄位：

- `adapter`: `codex` / `gemini` / `claude` / `generic`
- `command`: CLI 指令名稱
- `args`: 單次執行參數
- `interactive_command`: persistent session 使用的指令
- `interactive_args`: persistent session 使用的參數
- `env`: 額外環境變數
- `mode`: `oneshot` 或 `persistent`
- `transport`: `stdio` 或 `pty`
- `timeout_seconds`
- `response_idle_ms`
- `first_chunk_timeout_ms`
- `session_idle_ms`
- `startup_wait_ms`
- `prompt_suffix`

placeholder：

- `{{prompt}}`
- `{{project_name}}`
- `{{project_path}}`
- `{{channel_id}}`
- `{{thread_ts}}`
- `{{slack_user_id}}`
- `{{session_key}}`
- `{{last_message_path}}`

## 建議配置方式

建議只改這幾個地方：

1. 填入 Slack token，或改用環境變數
2. projects 加上你真正想遠端操作的 repo，並用 `channel_ids` 綁到對應 channel
3. 確認 `codex / claude / gemini` 這些 binary 在 `PATH` 裡
4. 如果只想讓特定 Slack channel 用，填 `slack.allowed_channels`

例如：

```json
{
  "log_level": "info",
  "quiet_by_default": true,
  "slack": {
    "app_token": "",
    "bot_token": "",
    "allowed_channels": ["C1234567890", "C2345678901"]
  },
  "projects": [
    {
      "name": "relay",
      "path": "/Users/match/git/ai-octo-relay",
      "default_agent": "codex",
      "channel_ids": ["C1234567890"]
    },
    {
      "name": "my-app",
      "path": "/Users/match/git/my-app",
      "default_agent": "claude",
      "channel_ids": ["C2345678901"]
    }
  ]
}
```

如果某條 thread 想暫時切別的 repo，再在那條 thread 裡執行：

```text
@Gemini !project use frontend-web
```

查看目前這條 thread / channel 用的是什麼：

```text
@Gemini !project current
```

## CLI 建議

目前本機已確認：

- `codex` 支援 `codex exec ... -` 的 non-interactive 模式
- `gemini` 支援 `gemini -p "<prompt>"` 的 non-interactive 模式
- Claude Code 套件在本機的 binary 名稱是 `claude`
- `claude` 是否可用取決於你的本機安裝與 PATH

目前預設建議：

- `codex` 用 `oneshot` 模式，較穩定
- `gemini` / `claude` 可用 `persistent` 模式

原因是 `codex` 的互動 TUI 會讀取 cursor position；在某些 PTY/session 環境下會直接退出。
如果你之後升級到較新的 `codex` 版本，再來重新評估是否恢復 persistent。

如果 `claude` 不在 PATH，請直接把 `command` 改成完整路徑。

## 驗證

```bash
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```
