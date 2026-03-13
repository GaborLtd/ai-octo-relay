# Changelog

這份文件只保留目前版本仍有參考價值的變更摘要。

## 2026-03

- 確立 `channel = project room`、`thread = 單一問題 / 單一 session`
- thread 後續訊息可直接延續，不必再次 mention bot
- agent 執行以 `oneshot + native resume/session-id` 為主
- Slack event 改為可並行處理，但同一 session 仍維持單飛保護
- 新增 `!cmd` 與受限的 `!git` 遠端命令
- state store / event store 拆分，支援 `json`、`jsonl`、`sqlite`
- 補上 `dm_read_only` 對命令與 agent 的限制
- `!agent model list <name>` 改為可列出三家 agent 的 model 清單來源（API / cache / fallback）
- `gemini` 改為 fresh oneshot，不再保存 / 重用 native session
- `gemini` 不再注入 `maxSessionTurns`，避免單次請求也被 CLI turn limit 打斷
- `gemini` 新增 thread summary continuity，只保存精簡摘要，不回放完整 thread transcript
- Gemini 常見錯誤摘要改為較保守，避免把所有 `code 53` 都誤導成 session resume 問題

若要看更細的歷史，直接查 git log。
