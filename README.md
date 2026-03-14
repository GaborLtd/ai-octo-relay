# ai-octo-relay

用 Slack 遠端操作本機專案的單一 bot。

目前重點是穩定支援：

- `channel -> project` 綁定
- `thread -> 單一問題 / 單一 session`
- 透過本機 AI CLI 執行 `codex`、`claude`、`gemini`
- 在同一 thread 延續 agent 的原生 session / resume
- 以白名單方式執行 `!cmd` 與有限的 `!git`

## 目前互動模型

- 每個 channel 對應一個 project，設定在 `projects[].channel_ids`
- 在 channel 根訊息提問時，bot 會回到該訊息的 thread
- 同一個 thread 內的後續訊息會沿用同一個 session，不需要再次 mention bot
- 新 thread 建立時可在訊息開頭指定 agent，例如 `gemini: 幫我看這個錯誤`
- 同一個 session 同時間只允許一個執行中的請求
- 不同 thread、不同 command 可並行處理

## 主要功能

- Slack channel / DM 收訊與回覆
- project registry 與 channel mapping
- agent registry 與 alias 解析
- thread / DM session 狀態保存
- `quiet` 模式與 Slack 訊息清理
- project 白名單命令 `!cmd`
- 有限制的 git 命令 `!git`
- 可切換 state store / event store

## CLI 注意事項

- [Codex CLI Notes](/Users/match/git/ai-octo-relay/docs/codex-cli-notes.md)
- [Claude CLI Notes](/Users/match/git/ai-octo-relay/docs/claude-cli-notes.md)
- [Gemini CLI Notes](/Users/match/git/ai-octo-relay/docs/gemini-cli-notes.md)

## 支援命令

```text
!help
!status
!project list
!project current
!project use <name>
!project clear
!agent list
!agent model list <name>
!agent use <name>
!agent clear
!cmd list
!cmd run <name>
!git status
!git add [path...]
!git commit <message>
!git checkout <branch>
!git checkout -b <new-branch>
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

限制：

- `!cmd` 只執行 `projects[].commands` 內定義的白名單命令
- `!git` 只開放固定子命令與少量參數
- `!git add` 與 `!git commit` 已拆開；不會自動一起執行
- `dm_read_only = true` 時，Slack 直接私訊 bot 的 DM（`channel_type = im`）會拒絕 `!cmd` 與 `!git`

## 設定方式

先建立設定檔：

```bash
cp config.example.json config.json
```

詳細的設定選項與範例請參考 [CONFIG.md](./CONFIG.md)。

Slack token 可直接放在 `config.json`，也可用環境變數覆寫：

```bash
export SLACK_APP_TOKEN=xapp-...
export SLACK_BOT_TOKEN=xoxb-...
```

最重要的設定欄位：

- `projects[].channel_ids`: channel 與 project 的綁定
- `projects[].default_agent`: 該 project 的預設 agent
- `projects[].commands`: `!cmd` 可執行的白名單命令
- `default_agent`: 全域預設 agent
- `language`: 預設回覆語言；例如 `zh-TW` 會自動要求 agent 使用繁體中文
- `quiet_by_default`: 是否先用安靜模式回覆
- `prompts.default.channel_write`: 一般 channel / thread 模式的預設 prompt
- `prompts.default.dm_read_only`: 直接私訊 bot 時的預設 read-only prompt
- `prompts.agents.<name>.channel_write`: 針對特定 agent 的 channel / thread 客製 prompt
- `prompts.agents.<name>.dm_read_only`: 針對特定 agent 的 DM 客製 prompt
- `agents.<name>.prompt_engineering`: 是否對該 agent 注入 prompt engineering；設為 `false` 時會停用語言提示與 prompt template
- `channel_write_prompt`: 一般 channel / thread 模式下，提示 agent 優先直接修改 project 檔案，而不是只在 Slack 貼草稿
- `dm_read_only`: 是否限制 Slack 直接私訊 bot 的 DM（`channel_type = im`）為唯讀；不影響一般 channel / private channel 內的 `@bot` 對話
- `state_store`: session / scope 狀態儲存
- `event_store`: event 紀錄儲存，可設為 `none`

`state_store.type` 支援：

- `json`
- `sqlite`

`event_store.type` 支援：

- `none`
- `jsonl`
- `sqlite`

## 執行

```bash
go run ./cmd/bot -config ./config.json
```

## Slack App

專案內提供：

```text
slack-manifest.yaml
```

建立流程：

1. 在 Slack 建立 App
2. 選擇 `From an app manifest`
3. 貼上 `slack-manifest.yaml`
4. 安裝到 workspace
5. 填入 `xapp-...` 與 `xoxb-...` token

## 文件

- [README.md](/Users/match/git/ai-octo-relay/README.md)：使用方式與現況
- [AGENTS.md](/Users/match/git/ai-octo-relay/AGENTS.md)：維護規則
- [docs/architecture-notes.md](/Users/match/git/ai-octo-relay/docs/architecture-notes.md)：目前架構
- [docs/changelog.md](/Users/match/git/ai-octo-relay/docs/changelog.md)：精簡變更紀錄
