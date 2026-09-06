# Codex 会话 provider 迁移

CodexRelay 在接管 Codex 配置时使用固定的 `model_provider = "codexrelay"`，展示名称为 `ergouzi.life`。

如果接管前的配置使用了已知旧 ID `codex_local_access`，程序会只迁移会话 JSONL 的
`session_meta.payload.model_provider` 和 Codex `state_5.sqlite` 的 `threads.model_provider`。
官方 `openai` 会话及其他未知 provider ID 不会被改写。

每次实际迁移会在当前 CodexRelay 数据目录生成 `codex-history-backups/<时间戳>/`，其中保存
原始 JSONL、状态数据库（包括存在的 SQLite WAL/SHM 文件）和 `manifest.json`。迁移发生在
外部 Codex 配置写入之后、Relay 配置提交之前；后续配置事务失败时会尝试按会话 ID 和原始
文件内容回滚。若 Codex 正在占用状态数据库导致迁移失败，应先关闭 Codex 后重试。
