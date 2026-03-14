# ai-octo-relay 設定指南

這份文件以目前程式實作為準，說明 `config.json` 的結構、欄位意義、優先順序，以及三個 agent 的實際行為差異。

如果只想快速上手，先看：

1. `config.example.json`
2. 本文件的「最小可用範例」
3. 本文件的「Agent 差異與注意事項」

## 先講重點

- `channel = project room`
- `thread = 單一問題 / 單一 session`
- project 主要由 `projects[].channel_ids` 決定
- 新 thread 的 agent 主要由 `project.default_agent` 或 `default_agent` 決定
- 同一個已建立 session 的 thread，後續訊息會沿用原本的 agent
- 直接私訊 bot 的 Slack DM 才算 `dm_read_only` 範圍；在 channel 內 `@bot` 不算 DM

## 最小可用範例

```json
{
  "command_prefix": "!",
  "default_agent": "codex",
  "language": "zh-TW",
  "state_store": {
    "type": "json",
    "path": "data/state.json"
  },
  "event_store": {
    "type": "jsonl",
    "path": "data/events.jsonl"
  },
  "quiet_by_default": true,
  "dm_read_only": true,
  "slack": {
    "app_token": "xapp-...",
    "bot_token": "xoxb-..."
  },
  "projects": [
    {
      "name": "my-project",
      "path": "$HOME/projects/my-project",
      "channel_ids": ["C0123456789"],
      "default_agent": "codex"
    }
  ],
  "agents": {
    "codex": {
      "adapter": "codex",
      "prompt_engineering": false,
      "model": "gpt-5.2-codex",
      "command": "codex"
    },
    "claude": {
      "adapter": "claude",
      "prompt_engineering": false,
      "model": "claude-sonnet-4-5",
      "command": "claude"
    },
    "gemini": {
      "adapter": "gemini",
      "prompt_engineering": true,
      "model": "gemini-2.5-flash",
      "command": "gemini",
      "args": ["--output-format", "stream-json", "-p", "{{prompt}}"]
    }
  }
}
```

## 啟動方式

1. 複製 `config.example.json` 成 `config.json`
2. 填入 `slack.app_token` 與 `slack.bot_token`
3. 確認 `projects[].path` 指到本機 repo
4. 啟動：

```bash
go run ./cmd/ai-octo-relay serve
```

若未指定 `-c` / `-config`，CLI 會依序搜尋：

1. `./config.json`
2. `~/.config/ai-octo-relay/config.json`
3. `~/.ai-octo-relay/config.json`

背景模式可用：

```bash
ai-octo-relay start
ai-octo-relay stop
ai-octo-relay restart
```

若要明確指定設定檔：

```bash
ai-octo-relay restart -c ./config.json
```

若尚未安裝 binary，可用：

```bash
go build -o ./bin/ai-octo-relay ./cmd/ai-octo-relay
```

若要讓 Slack `!cmd run relay-restart` 可重啟本 bot，可在 `projects[].commands` 加入：

```json
{
  "name": "relay-restart",
  "description": "restart ai-octo-relay daemon",
  "command": "/absolute/path/to/ai-octo-relay",
  "args": ["restart", "-c", "/absolute/path/to/config.json"]
}
```

## Root 欄位

| 欄位 | 型別 | 說明 | 預設值 |
| :--- | :--- | :--- | :--- |
| `command_prefix` | `string` | Slack 指令前綴，例如 `!help`。 | `!` |
| `default_agent` | `string` | 全域預設 agent。若 project 沒指定 `default_agent`，會退回這裡。 | 空 |
| `language` | `string` | 語言提示。目前 `zh-TW` 會附加「請一律使用繁體中文回覆。」 | 空 |
| `store_path` | `string` | 舊欄位。若 `state_store.path` 沒填，會用它當預設值。 | `data/state.json` |
| `log_level` | `string` | `trace` / `debug` / `info` / `warn` / `error`。 | `info` |
| `quiet_by_default` | `bool` | 是否預設先回簡短處理中訊息，再補最終結果。 | `false` |
| `dm_read_only` | `bool` | 是否將 Slack 直接私訊 bot 的 DM 視為唯讀模式。 | `false` |
| `channel_write_prompt` | `string` | 舊欄位。channel/thread 可寫模式的預設 prompt。 | 內建預設 |
| `dm_read_only_prompt` | `string` | 舊欄位。DM 唯讀模式的預設 prompt。 | 內建預設 |
| `prompts` | `object` | 新版 prompt 設定。可全域或 per-agent 覆寫。 | 空物件 |
| `slack` | `object` | Slack token 與 channel 限制。 | 必填 |
| `projects` | `array` | 可操作的本機專案清單。 | 至少一個 |
| `agents` | `object` | agent 定義表。 | 至少一個 |

## Prompt 設定

目前 prompt 來源分三層：

1. `prompts.agents.<agent>.*`
2. `prompts.default.*`
3. 舊欄位 `channel_write_prompt` / `dm_read_only_prompt`

可用欄位：

| 欄位 | 說明 |
| :--- | :--- |
| `prompts.default.channel_write` | 一般 channel/thread 模式的預設 prompt |
| `prompts.default.dm_read_only` | DM 唯讀模式的預設 prompt |
| `prompts.agents.<name>.channel_write` | 指定 agent 的 channel/thread prompt |
| `prompts.agents.<name>.dm_read_only` | 指定 agent 的 DM prompt |

### `prompt_engineering`

每個 agent 可用 `agents.<name>.prompt_engineering` 控制是否套用：

- `true`: 會附加 `language` 與對應 prompt template
- `false`: 不附加這些 prompt

若未明確設定，目前預設是 `true`。

## Storage

### `state_store`

用來儲存：

- channel scope 狀態
- thread scope 狀態
- session 狀態
- Gemini 的 thread summary

目前支援：

- `json`
- `sqlite`

欄位：

| 欄位 | 說明 |
| :--- | :--- |
| `state_store.type` | `json` 或 `sqlite` |
| `state_store.path` | 狀態檔或資料庫路徑 |

### `event_store`

用來紀錄 bot 事件與互動記錄。

目前支援：

- `none`
- `jsonl`
- `sqlite`

欄位：

| 欄位 | 說明 |
| :--- | :--- |
| `event_store.type` | `none` / `jsonl` / `sqlite` |
| `event_store.path` | 事件檔或資料庫路徑；當 `type != none` 時必填 |

### 路徑解析規則

- `store_path`、`state_store.path`、`event_store.path` 若為相對路徑，會以 `config.json` 所在目錄解析
- `projects[].path` 支援環境變數展開，例如 `$HOME/projects/app`
- `projects[].path` 若為相對路徑，也會以 `config.json` 所在目錄解析

## Slack 設定

| 欄位 | 說明 | 可被環境變數覆蓋 |
| :--- | :--- | :--- |
| `slack.app_token` | Slack app-level token (`xapp-...`) | `SLACK_APP_TOKEN` |
| `slack.bot_token` | Slack bot token (`xoxb-...`) | `SLACK_BOT_TOKEN` |
| `slack.allowed_channels` | 選填；限制 bot 只處理特定 channel | 無 |

## Project 設定

`projects` 決定 bot 可以操作哪些本機目錄，以及 channel 會映射到哪個 project。

| 欄位 | 說明 |
| :--- | :--- |
| `name` | 專案唯一名稱 |
| `path` | 本機專案路徑 |
| `channel_ids` | 綁定此 project 的 Slack channel ID 清單 |
| `default_agent` | 這個 project 的預設 agent |
| `commands` | `!cmd run <name>` 可執行的白名單命令 |

### `commands`

| 欄位 | 說明 |
| :--- | :--- |
| `name` | Slack 指令名 |
| `description` | 指令說明 |
| `command` | 實際執行檔 |
| `args` | 參數陣列 |

### Project 決策順序

新 session 建立時，project 主要依序決定：

1. thread override：`!project use <name>`
2. `projects[].channel_ids`
3. 第一個 project

`!project clear` 只會清掉 thread override，不會改掉 `channel_ids` 映射。

## Agent 設定

### 基本欄位

| 欄位 | 說明 |
| :--- | :--- |
| `adapter` | 內建 adapter 類型，例如 `codex` / `claude` / `gemini` |
| `aliases` | 別名，例如 `["o3", "openai"]` |
| `prompt_engineering` | 是否套用語言提示與 prompt template |
| `model` | 傳給 CLI 的 model 名稱 |
| `command` | 執行檔名稱 |
| `args` | oneshot 模式的參數 |
| `interactive_command` | persistent 模式時的替代 command |
| `interactive_args` | persistent 模式時的替代 args |
| `env` | 額外環境變數 |

### 進階欄位

| 欄位 | 說明 | 預設值 |
| :--- | :--- | :--- |
| `mode` | `oneshot` 或 `persistent` | `oneshot` |
| `transport` | `stdio` 或 `pty`；若 `mode=persistent` 且未設定，會偏向 `pty` | `stdio` |
| `timeout_seconds` | 整體 request timeout，單位秒 | `1800` |
| `response_idle_ms` | 已開始輸出後，等待下一段輸出的上限，單位毫秒 | `1800` |
| `first_chunk_timeout_ms` | 等待第一段輸出的上限，單位毫秒 | `30000` |
| `session_idle_ms` | session 閒置多久後視為過期，單位毫秒 | `900000` |
| `startup_wait_ms` | 啟動後等待初始化的時間，單位毫秒 | `1200` |
| `prompt_suffix` | 永遠附加在 prompt 後方的文字 | 空 |
| `max_session_turns` | agent 層設定值；是否真的被 adapter 使用，取決於各 agent 實作 | `0` / 未限制 |

### `args` 可用樣板

目前常用樣板：

- `{{prompt}}`
- `{{project_path}}`
- `{{model}}`
- `{{native_session_id}}`
- `{{last_message_path}}`

不是每個 adapter 都會用到所有樣板，但文件或自訂參數時可以用這些 placeholder。

### Agent 名稱與別名正規化

agent 名稱與 aliases 會先做 normalize：

- 轉小寫
- 去掉前綴 `@`
- 去掉前綴 `#`
- 去掉結尾 `:`

例如：

- `Gemini`
- `@gemini`
- `gemini:`

都會被視為同一個 selector。

## Agent 差異與注意事項

### Codex

- 預設走 `oneshot`
- 同 thread 會優先沿用 Codex native resume
- 即使自訂 `args`，目前也會補上 resume 路徑
- 若要自訂 `args`，建議保留 `{{last_message_path}}` 與 `{{project_path}}` 相關行為

典型設定：

```json
{
  "adapter": "codex",
  "model": "gpt-5.2-codex",
  "command": "codex",
  "args": [
    "exec",
    "--skip-git-repo-check",
    "--color",
    "never",
    "--output-last-message",
    "{{last_message_path}}",
    "-C",
    "{{project_path}}",
    "-"
  ]
}
```

### Claude

- 預設走 `oneshot`
- 同 thread 透過 `--session-id` 延續 session
- 若 session 不存在，服務層會先產一個 UUID

典型設定：

```json
{
  "adapter": "claude",
  "model": "claude-sonnet-4-5",
  "command": "claude",
  "args": [
    "-p",
    "--output-format",
    "text",
    "--session-id",
    "{{native_session_id}}",
    "{{prompt}}"
  ]
}
```

### Gemini

- 目前預設走 `fresh oneshot`
- **不使用 Gemini native session resume**
- 目前 continuity 依賴服務層保存的 Gemini thread summary
- 建議使用 `stream-json`
- channel/thread 會補 `--approval-mode auto_edit`
- DM 且 `dm_read_only=true` 時會補 `--approval-mode plan --sandbox`

典型設定：

```json
{
  "adapter": "gemini",
  "prompt_engineering": true,
  "model": "gemini-2.5-flash",
  "command": "gemini",
  "args": [
    "--output-format",
    "stream-json",
    "-p",
    "{{prompt}}"
  ]
}
```

> `max_session_turns` 目前不建議拿來控制 Gemini；本專案已避免再把它注入 Gemini CLI。

## Session 與 Agent 決策

### 新 session 的 agent 決策順序

1. 訊息開頭的 agent selector，例如 `gemini:`
2. 已存在的 thread state
3. project 的 `default_agent`
4. root `default_agent`
5. 第一個已設定 agent

### 已建立 session 的規則

- 同一個 thread / DM session 一旦綁定 agent，後續不應在中途切換
- 若要切換 agent，應開新 thread，或使用 `!reset` / `!session restart`

## DM 與 Channel 的差異

### Channel / thread

- 一般可寫模式
- 若 agent 支援，應直接修改 workspace
- `@bot` 在 channel 裡不算 DM

### Slack DM

- 只有 `channel_type = im` 才算 DM
- 當 `dm_read_only = true`：
  - `!cmd` 會被擋
  - `!git` 會被擋
  - agent 會被導向 read-only / plan 模式

## 範例

### 範例 1：單一 project，Codex 當主力

```json
{
  "default_agent": "codex",
  "language": "zh-TW",
  "quiet_by_default": true,
  "slack": {
    "app_token": "xapp-...",
    "bot_token": "xoxb-..."
  },
  "projects": [
    {
      "name": "my-app",
      "path": "$HOME/projects/my-app",
      "channel_ids": ["C071ABCDEFG"],
      "default_agent": "codex"
    }
  ],
  "agents": {
    "codex": {
      "adapter": "codex",
      "prompt_engineering": false,
      "model": "gpt-5.2-codex",
      "command": "codex"
    }
  }
}
```

### 範例 2：Codex / Claude / Gemini 混用

```json
{
  "default_agent": "codex",
  "language": "zh-TW",
  "prompts": {
    "default": {
      "channel_write": "請優先直接修改 workspace，最後簡短摘要主要變更與檔案路徑。",
      "dm_read_only": "只做分析、review、規劃與文字 diff，不要修改檔案。"
    },
    "agents": {
      "gemini": {
        "channel_write": "優先直接更新檔案，不要只貼草稿；最後列出主要檔案路徑。",
        "dm_read_only": "保持 read-only，提供精簡計畫即可。"
      }
    }
  },
  "agents": {
    "codex": {
      "adapter": "codex",
      "aliases": ["o3", "openai"],
      "prompt_engineering": false,
      "model": "gpt-5.2-codex",
      "command": "codex"
    },
    "claude": {
      "adapter": "claude",
      "aliases": ["sonnet"],
      "prompt_engineering": false,
      "model": "claude-sonnet-4-5",
      "command": "claude"
    },
    "gemini": {
      "adapter": "gemini",
      "aliases": ["flash"],
      "prompt_engineering": true,
      "model": "gemini-2.5-flash",
      "command": "gemini",
      "args": ["--output-format", "stream-json", "-p", "{{prompt}}"]
    }
  }
}
```

### 範例 3：啟用白名單命令

```json
{
  "projects": [
    {
      "name": "api-server",
      "path": "./api-server",
      "channel_ids": ["C112233"],
      "commands": [
        {
          "name": "lint",
          "description": "Run golangci-lint",
          "command": "golangci-lint",
          "args": ["run"]
        },
        {
          "name": "test",
          "description": "Run unit tests",
          "command": "go",
          "args": ["test", "./..."]
        }
      ]
    }
  ]
}
```

### 範例 4：使用 SQLite

```json
{
  "state_store": {
    "type": "sqlite",
    "path": "data/state.db"
  },
  "event_store": {
    "type": "sqlite",
    "path": "data/events.db"
  },
  "log_level": "debug"
}
```

## 常見誤解

### `@bot` 算不算 DM？

不算。只有 Slack 直接私訊 bot 的對話視窗才算 DM。

### `prompt_engineering = false` 會怎樣？

該 agent 不會自動附加：

- `language`
- `prompts.default.*`
- `prompts.agents.<agent>.*`

### Gemini 會不會沿用 thread 歷史？

不會沿用 Gemini native session。現在是透過服務層保存的 Gemini summary 來提供有限 continuity。

### 自訂 `args` 會不會破壞 session？

- `codex`：目前已處理 custom `args` 下的 resume
- `claude`：若你移除 `--session-id {{native_session_id}}`，就會失去 session continuity
- `gemini`：目前本來就不依賴 native resume

## 驗證規則

載入設定時，會檢查：

- `slack.app_token` / `slack.bot_token` 是否存在
- 至少有一個 project
- 至少有一個 agent
- `log_level` 是否合法
- `state_store.type` 是否為 `json` 或 `sqlite`
- `event_store.type` 是否為 `none` / `jsonl` / `sqlite`
- 若 `event_store.type != none`，則 `event_store.path` 必填
- `default_agent` 是否存在於 `agents`
- `projects[].channel_ids` 不可重複指向多個 project
- `projects[].commands[].name` 在同一 project 內不可重複
- `agents` 名稱與 aliases 正規化後不可互相衝突

## 安全提醒

- 不要把真實 `config.json` 提交到 git
- 公開樣板請使用 `config.example.json`
- 不要把 Slack token、state/event store 檔案提交到 repo
