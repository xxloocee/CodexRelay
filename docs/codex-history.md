# Codex 会话 provider 迁移

CodexRelay 在接管 Codex 配置时使用固定的 `model_provider = "codexrelay"`，展示名称为 `ergouzi.life`。

如果接管前的配置使用了已知旧 ID `codex_local_access`，程序会只迁移会话 JSONL 的
`session_meta.payload.model_provider` 和 Codex `state_5.sqlite` 的 `threads.model_provider`。
官方 `openai` 会话及其他未知 provider ID 不会被改写。

每次实际迁移会在当前 CodexRelay 数据目录生成 `codex-history-backups/<时间戳>/`，其中保存
原始 JSONL、状态数据库（包括存在的 SQLite WAL/SHM 文件）和 `manifest.json`。迁移发生在
外部 Codex 配置写入之后、Relay 配置提交之前；后续配置事务失败时会尝试按会话 ID 和原始
文件内容回滚。若 Codex 正在占用状态数据库导致迁移失败，应先关闭 Codex 后重试。

## 切换诊断与手动修复

高级设置的 Codex 区域只保留“修复历史会话”按钮。切换诊断在后台记录，分别区分配置校验和请求进入 Relay；收到请求不代表上游响应成功。诊断日志位于数据目录的 `codex-switch-diagnostics.jsonl`，超过 2 MiB 后轮转到一个 `.previous` 文件。日志不记录密钥、配置正文、账号 ID 或原始错误正文；路径和上游域名仍会显示，分享前可以脱敏。迁移数据目录后旧日志留在原处，新日志写入新目录。

客户端目录按已保存路径、有效的绝对路径 `CODEX_HOME`、用户目录下 `.codex` 的顺序确定。保存后的目录保持固定，避免环境变量变化导致备份归属漂移，页面显示目录来源。

官方恢复会保留当前 MCP、插件、项目、界面等白名单用户设置（包括已删除的设置），账号、连接和模型配置使用官方备份。令牌模式会清理根级和 profile 中的模型、review model、模型目录及上下文长度等上游专属覆盖。两种模式都将根级 `model_provider` 写在文件第一行；TOML 表声明仍须位于根级字段之后。

点击“修复历史会话”后忽略原供应商，自动扫描所有来源，包括自定义、空值和缺失 Provider；确认后统一归入 Codex 当前实际选中的 Provider（包含所选 profile 的覆盖，未设置时使用 `openai`），不需要选择来源。不依赖 Relay 保存的模式或认证文件格式，官方 OAuth 的空 API Key、钥匙串登录和其他供应商均可使用。内置预览十分钟有效且只能使用一次；期间配置、目标或候选集合变化需重新点击。执行前必须退出 Codex/ChatGPT 及其相关进程。修复不会在后续切换配置时自动重复；更换 Provider 后若历史再次不可见，可再次点击修复。

修复前自动保存原始 JSONL、精确数据库变更记录及完整性信息。新备份使用 v3 清单，逐条保存原 Provider（包括 NULL、空字符串）、索引联合键、新建目录项和原隐藏标记，恢复接口兼容 v1/v2；排障时可按备份编号调用恢复接口，页面不要求用户管理编号。v2/v3 恢复可处理仅部分文件已改写、数据库尚未提交的中断状态，也可重复执行；只逆转已发生的对应修改，文件被后续修改或数据库记录冲突时拒绝覆盖。备份位于 `codex-history-backups/<编号>/`。系统文件锁在进程退出时自动释放，遗留锁文件不需要人工删除。该入口不支持外置 `sqlite_home`、符号链接或目录重定向；路径大小写不一致也可能被保守拒绝。

修复会同步 JSONL、`threads` 和本地 `local_thread_catalog` 的 Provider；带 `host_id` 的目录索引按本地主机筛选，不修改远程设备记录。对仍有 `threads` 记录且能匹配真实 JSONL 的普通用户会话，在目录 schema 支持、唯一确认本地主机时补建缺失的桌面目录项，并清除错误的 `missing_candidate` 标记、刷新目录版本。目录变更与 Provider 更新使用同一数据库事务并支持撤销。明确的子代理/内部任务跳过，归档状态与其他用户设置保留；不把归档、exec 或代理内部任务补入普通主列表。

预览仅保留元数据、路径及摘要，执行/恢复逐文件加载，数据库摘要采用流式读取，不把全部历史正文同时保存在内存。内存仍受单个最大 JSONL 文件大小影响。完全丢失 `threads` 记录或原始会话文件、外置数据库、远程会话及跨账号/供应商的加密续聊不在恢复保证内。

切换 CodexRelay 数据目录时一并复制 `codex-history-backups/`，拒绝覆盖已有目标；原目录中的历史备份保留为额外恢复副本。自动迁移仍仅处理已知旧 Relay provider，不自动修改官方历史。
