# ai-octo-relay

單一 Slack bot，多 project、多 AI CLI agent 的遠端控制 MVP。

## 最新狀態

目前專案的主模型已經確立：

- `channel = project room`
- `thread = 單一問題 / 單一 session`
- `projects[].channel_ids` 決定 channel 對應的 project
- 在 thread 中後續追問，不需要再 mention bot
- 不同 thread / 指令不應被單一 agent CLI 長時間執行整體卡住
- 同一個 session 同一時間只允許一個 agent request 執行，避免 native resume/session-id 互撞
- 所有 agent 預設使用 `oneshot`
- 同一個 thread 會優先沿用 CLI 原生 `resume/session-id` 記憶
- Slack 回覆會做清理與自動分段發送
- DM 預設可設為 read-only，只回答問題不修改專案

先看文件：

- 使用與設定：[README.md](/Users/match/git/ai-octo-relay/README.md)
- 維護規則：[AGENTS.md](/Users/match/git/ai-octo-relay/AGENTS.md)
- 架構背景：[architecture-notes.md](/Users/match/git/ai-octo-relay/docs/architecture-notes.md)
- 變更歷程：[changelog.md](/Users/match/git/ai-octo-relay/docs/changelog.md)

## 核心能力

- 單一 Slack bot 接收訊息
- 多個本機 project registry
- 以 `channel_id -> project` 固定綁定 channel 與 project
- thread 可覆寫 project；agent 會在 session 建立時鎖定
- 可在訊息開頭指定 agent，例如 `@bot #claude 幫我看這段 code`
- 透過本機 CLI 執行 `codex` / `claude` / `gemini`
- 同一個 thread 可透過 CLI 原生 session/resume 延續上下文
- Slack event 改為非同步處理，避免一個長時間任務把整個 bot 卡住
- 使用可切換的 state/event store 持久化狀態

## MVP 範圍

這一版刻意保持簡單：

- 不做 multiple bot relay
- 不做多平台
- native session id 會保存在本機 state store
- 不再主推 PTY persistent/TUI 模式
- 結構化 state 預設用 JSON；append-only event 可用 JSONL，兩者都可切到 SQLite

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
!cmd list
!cmd run <name>
!git status
!git diff [--stat|--staged|--cached]
!git log [count]
!git branch
!git show <rev>
!git fetch
!git pull
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
- 同一個 `thread + project + agent` 會優先沿用該 CLI 的原生 session/resume 能力
- 不同 thread、DM、以及 `!help` 這類 command 不應被別的長時間 agent 執行阻塞
- 同一個 session 若上一個請求尚未完成，新的 prompt 會被拒絕，避免同時打到同一個 native session
- 若訊息第一個 token 是 `agent:` / `alias:`，或相容的 `#agent` / `#alias`，只會在新 thread / 新 DM session 建立時決定 agent
- thread 或 DM session 一旦建立，就不能在同一 session 內切換 agent
- agent selector 只用來選 agent，不會當成 prompt 內容送進 CLI

## Remote Commands

目前支援兩種遠端命令能力：

- `!cmd`
  - 只能執行 `projects[].commands` 內預先定義好的白名單 command
- `!git`
  - 只開放一小組常用 git 子命令，不支援任意 git 參數

### `!cmd`

先列出目前 project 可用的 command：

```text
!cmd list
```

執行指定 command：

```text
!cmd run dev
!cmd run test
```

設計原則：

- 不提供任意 shell
- 只執行 config 中明確定義的 command
- command 會在目前 project 路徑下執行

### `!git`

目前只支援這些子命令：

```text
!git status
!git diff
!git diff --stat
!git diff --staged
!git log
!git log 10
!git branch
!git show HEAD~1
!git fetch
!git pull
```

限制：

- `diff` 只允許 `--stat`、`--staged`、`--cached`
- `log` 只接受 1-100 的筆數
- `pull` 固定使用 `--ff-only`
- 不支援任意 `git` 參數，避免變成遠端 shell

安全性：

- `!cmd run` 可能真的修改專案狀態
- `!git fetch`、`!git pull` 也會改變本機 git 狀態
- 若 `dm_read_only = true`，DM 中會直接拒絕所有 `!cmd` 與 `!git`
- project command、git、以及任何 project/OS scope 的操作，都應只在對應 channel/thread 中執行

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

另外可設定 DM 是否預設 read-only：

```json
"dm_read_only": true
```

開啟後，DM 內的 agent 只應做分析、解釋、review 與建議，不應修改檔案，也不應執行任何 project/OS scope 的操作。

目前內建策略：

- `codex`: 會改用 read-only sandbox
- `claude`: 會改用 `--permission-mode plan`
- `gemini`: 會改用 `--approval-mode plan --sandbox`
- 另外仍會附加 read-only prompt，作為第二層防線
- 同時 DM 會拒絕 `!cmd` 與 `!git`，避免 project/OS 行為落在錯的 scope

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
- [AGENTS.md](/Users/match/git/ai-octo-relay/AGENTS.md)
- [changelog.md](/Users/match/git/ai-octo-relay/docs/changelog.md)

## Config 結構

這個專案的設定檔是單一 JSON，啟動時用 `-config` 指定，預設讀 `config.json`。

重點欄位：

- `command_prefix`: Slack 指令前綴
- `default_agent`: 全域預設 agent
- `store_path`: 舊版 state store 路徑，相容保留
- `state_store.type`: `json / sqlite`
- `state_store.path`: 結構化 state store 路徑
- `event_store.type`: `none / jsonl / sqlite`
- `event_store.path`: append-only event store 路徑
- `log_level`: `trace / debug / info / warn / error`
- `quiet_by_default`: 是否預設安靜輸出
- `slack.app_token`
- `slack.bot_token`
- `projects`
- `agents`

### projects

`projects` 至少要有一個，每個 project 主要欄位：

- `name`: project 名稱
- `path`: 專案實際路徑
- `default_agent`: 這個 project 的預設 agent
- `channel_ids`: 綁定這個 project 的 Slack channel IDs
- `commands`: 可遠端執行的白名單 command

限制：

- 同一個 `channel_id` 只能出現在一個 project 裡
- 如果重覆，config 驗證會直接失敗

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

也可以加上白名單 command：

```json
{
  "name": "your-project",
  "path": "/path/to/your-project",
  "default_agent": "codex",
  "channel_ids": [],
  "commands": [
    {
      "name": "dev",
      "description": "start local dev server",
      "command": "make",
      "args": ["dev"]
    },
    {
      "name": "test",
      "description": "run project tests",
      "command": "make",
      "args": ["test"]
    }
  ]
}
```

### `slack.allowed_channels` 是否需要

一般情況下，不需要。

因為現在主要模型已經是：

- `projects[].channel_ids` 決定每個 channel 對應哪個 project

`slack.allowed_channels` 比較像額外的全域白名單，只有在你想做第二層限制時才需要。

所以建議：

- 日常使用：只用 `projects[].channel_ids`
- 需要額外限制：再另外加 `slack.allowed_channels`

### agents

`agents.<name>` 欄位：

- `adapter`: `codex` / `gemini` / `claude` / `generic`
- `aliases`: agent 別名，例如 `["c", "cc"]`
- `command`: CLI 指令名稱
- `aliases`: 可在 Slack 訊息開頭使用的 agent 別名
- `args`: 單次執行參數
- `interactive_command`: 若未來重新啟用 persistent session，可指定互動模式指令
- `interactive_args`: 若未來重新啟用 persistent session，可指定互動模式參數
- `env`: 額外環境變數
- `mode`: `oneshot` 或 `persistent`
- `transport`: `stdio` 或 `pty`
- `timeout_seconds`
- `max_session_turns`
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
- `{{native_session_id}}`
- `{{last_message_path}}`

限制：

- `agents` 的 key 與所有 `aliases` 經過正規化後不能重覆
- 例如 `codex`、`codex:`、`#codex`、`@codex` 會視為同一個 selector

Slack 訊息可直接指定 agent，例如：

```text
@bot codex: 幫我看這個 project 的 build error
@bot gemini: 幫我總結這個檔案
@bot sonnet: 幫我補測試
```

規則：

- 主推 `selector:` 格式，例如 `codex:`、`gemini:`、`claude:`
- 相容保留 `#selector` 格式，例如 `#codex`
- `selector` 可對應 agent 名稱或 `aliases`
- 裸字 `claude` / `gemini` / `sonnet` 不會觸發 override
- 只有新 thread / 新 DM session 建立時才會套用 selector
- 同一 thread / DM session 不可改用別的 agent

其中 `sonnet:` 是 `claude.aliases` 的例子。

### DM 模式

- DM 不強制 thread 模型
- DM session 一旦建立，也會鎖定 agent；若要改 agent，請先重開 session
- 若 `dm_read_only = true`，DM 預設只回答問題，不修改專案
- `!cmd` 與 `!git` 只允許在 project 對應的 channel/thread 中使用
- channel 內的行為不受影響，仍可正常執行修改任務

## 建議配置方式

建議只改這幾個地方：

1. 填入 Slack token，或改用環境變數
2. projects 加上你真正想遠端操作的 repo，並用 `channel_ids` 綁到對應 channel
3. 確認 `codex / claude / gemini` 這些 binary 在 `PATH` 裡
4. 如果你還想再加一層全域白名單，才另外使用 `slack.allowed_channels`

例如：

```json
{
  "log_level": "info",
  "quiet_by_default": true,
  "slack": {
    "app_token": "",
    "bot_token": ""
  },
  "projects": [
    {
      "name": "relay",
      "path": "/path/to/relay",
      "default_agent": "codex",
      "channel_ids": ["C1234567890"]
    },
    {
      "name": "my-app",
      "path": "/path/to/my-app",
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

- `codex` 支援 `codex exec` 與 `codex exec resume`
- `gemini` 支援 non-interactive 模式，且提供 `--resume`
- Claude Code 套件在本機的 binary 名稱是 `claude`
- `claude` 是否可用取決於你的本機安裝、登入方式與 PATH

目前預設建議：

- 三個 agent 都先使用 `oneshot`
- 同一個 thread 的上下文優先靠各 CLI 原生 `resume/session-id` 能力維持

原因是 `codex` / `gemini` 的互動 TUI 在 PTY/session 環境下都曾出現終端控制輸出污染或相容性問題。

Gemini 額外建議：

- 設定 `max_session_turns`
- 例如 `8`

這會限制 Gemini CLI 在單次 one-shot 內的 tool / agent 迴圈次數，避免長時間卡住不回。

另外目前對 Gemini 有一層保護：

- 若沿用的 native session 已達 CLI 的 max session turns
- 系統會自動清掉該 thread 的 Gemini session id，並以新 session 重試一次

如果 `claude` 不在 PATH，請直接把 `command` 改成完整路徑。

## 驗證

```bash
GOCACHE=$(pwd)/.gocache GOMODCACHE=$(pwd)/.gomodcache go test ./...
```
