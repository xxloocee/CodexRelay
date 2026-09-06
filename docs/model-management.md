# 模型目录与外部客户端配置契约

## 目录来源

编辑页的“获取模型列表”调用当前代理 API 的模型发现接口。地址按固定候选顺序构造：

1. `{BaseURL}/v1/models`
2. `{BaseURL}/models`
3. 当 `BaseURL` 的最后一段是明确版本（例如 `/v2`）时，再尝试去掉版本段后的 `/models` 和 `/v1/models`。

`404` 和 `405` 只表示当前候选地址不可用，会继续尝试下一个地址；其他非 2xx 状态直接返回错误。响应必须是 OpenAI 兼容的 JSON 对象，模型条目使用 `data[].id`，可选读取 `data[].owned_by`。模型 ID 会去重并按字典序排序，未知结构或空目录不会覆盖本地缓存。

候选地址和字段形状参考 `E:\Code\Codex\cc-switch-main\src-tauri\src\services\model_fetch.rs` 的通用 `fetch_models_for_config` 实现。CodexRelay 请求使用当前代理 API 的 Bearer 密钥和已校验的额外请求头，不按类别猜测认证格式。

## 本地缓存

`config.json` 的每个 `profiles[]` 保存：

- `models[]`: `id`、可选 `name`、可选 `ownedBy`、可选 `contextWindow`；
- `defaultModel`: 模型 ID，可为空，但非空时必须存在于 `models[]`。

模型获取只更新编辑器内存，点击“保存”后与 API 地址、密钥和请求头一起原子写入 `config.json`。删除模型、修改名称或默认项也只在保存时生效。程序启动和定时同步不会自动刷新模型目录。

## 外部客户端写入

外部客户端配置只使用本地缓存，不在切换时请求上游模型接口。每次覆盖前读取原文件并生成：

`<Relay 数据目录>/client-backups/<category>/<原文件>.<YYYYMMDD-HHMMSS>.CodexRelay`

同一秒发生冲突时追加序号。Codex 的 `auth.json` 按产品约定直接写入 `OPENAI_API_KEY`，不保留 OAuth 分支。

- Codex：写入固定 provider ID `codexrelay`、展示名 `ergouzi.life`、地址和默认模型（若已设置），并覆盖 `auth.json`。
- OpenCode：写入 `provider.codexrelay.models`，每个键为模型 ID，值保存显示名称。
- OpenClaw：写入 `models.providers.codexrelay.models`，同时更新 `agents.defaults.model.primary` 和允许目录；没有缓存模型时不写入空模型数组。
- Grok：写入 `[models]` 默认项及每个 `[model."<id>"]`；`context_window` 取模型条目的明确值，否则使用 cc-switch-main 适配器要求的 `500000`。
- Hermes：写入 `custom_providers` 下的 CodexRelay provider 和模型字典；没有模型时不生成 `models` 或 `model` 字段。
- Claude：写入 `ANTHROPIC_MODEL` 以及四个默认角色模型环境变量，均使用缓存的默认模型。
- Gemini：在 `.env` 中写入 `GEMINI_MODEL`，并继续更新地址、密钥和认证开关。

JSON5 客户端仍使用现有解析和 JSON 输出路径；原文件会先备份到 Relay 数据目录的 `client-backups/<category>/`，写入失败不会删除备份文件。

## 未验证项

本地 fixture 已覆盖地址候选、404 回退、非 2xx 分支、去重排序和配置写入结构。真实 Claude、Gemini、Grok、OpenCode、OpenClaw、Hermes 版本对模型字段的最终接受情况仍需在对应客户端安装版本中启动验证；上游接口不可达时不能用本地测试替代真实接受证据。
