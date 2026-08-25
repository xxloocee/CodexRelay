# 消息通知

## 目的与证据

消息通知由 `tasknotify.Manager` 管理，不在 CodexRelay 的 HTTP 透明代理请求链路中。所有已勾选的本地事件都进入同一个耐久队列，并访问同一个用户完整填写的 HTTP(S) URL。

任务事件读取当前用户可见的 Codex rollout JSONL，扫描路径为 `.codex/sessions` 下名称匹配 `rollout-*.jsonl` 的文件。当前实现只依赖已验证的 JSONL 字段：

- `session_meta.payload.id`：本地会话标识；
- `event_msg.payload.type`：`task_started`、`task_complete`、`turn_aborted`；
- `event_msg.payload.turn_id`：本地 turn 标识；完成和中断时间优先读取事件中的 `started_at`、`completed_at`，缺失时使用事件顶层 `timestamp`。

为补偿 rollout 观察无法单独确认 root 任务空闲的问题，服务只读查询当前环境已经存在的 Codex SQLite：最高版本的 `state_*.sqlite` 和 `goals_*.sqlite`。数据库以 `mode=ro` 打开，并设置 `PRAGMA query_only = ON`，不会写入、迁移或锁定 Codex 数据。当前使用的本机字段来自真实 schema 和样本核对：

- `state_*.sqlite.threads.thread_source`、`threads.source`：root/subagent 分类；
- `state_*.sqlite.thread_spawn_edges`：递归查找 root 的子代理；
- `state_*.sqlite.threads.rollout_path`：读取子代理 rollout 的本地路径；
- `goals_*.sqlite.thread_goals.status`：active goal 门控。

因此，只有 root thread 才可能通过任务通知门控；active goal 或仍在活动窗口内的子代理会让候选继续留在 `pending/`。已识别为 subagent 的候选会写入 `suppressed/`，不单独推送。子代理 rollout 最近是 `task_started`、仍在写入、格式不完整或无法确认时，在新鲜状态窗口内视为活动；超过 30 分钟孤立后不再阻塞 root。SQLite 文件缺失、锁竞争、查询超时或 schema 不匹配时，状态读取结果为不可用，当前实现保留原有 rollout-only 行为，不把未知状态误判成“空闲已证实”。

Codex hook/notify 配置和云端任务生命周期不是当前实现的输入；服务不会修改 `config.toml`、`hooks.json` 或 `auth.json`。这些 SQLite 表和 rollout 字段是当前本地实现细节，不是稳定的公开协议；换环境或 Codex 版本后需要重新核对 schema 和脱敏样本。

## 事件范围

设置页可以独立勾选以下事件：

- 任务完成：本机 rollout 的最近生命周期事件为 `task_complete`，并且静默确认窗口内未继续变化。
- 任务异常中断：本机 rollout 的最近生命周期事件为 `turn_aborted`，并且静默确认窗口内未继续变化。
- 令牌请求故障：活动令牌的 401/403 连续故障，或 5xx/网络故障在现有统计窗口内达到健康阈值；同一故障轮次只推送一次。
- 令牌自动切换：现有自动切换流程成功完成本地 Profile 切换。
- 令牌自动切换失败：现有自动切换流程耗尽候选项。
- 账户余额不足：二狗子现有账户低余额提醒记录首次出现。
- 套餐余额不足：二狗子现有套餐提醒首次进入 `low_balance` 状态。

令牌和余额事件不依赖 rollout，不等待静默确认，直接写入 `outbox/`。余额事件只复用已经存在的二狗子同步和低余额提醒状态，不解析或假设新的上游字段；同一低余额状态不会因后续同步重复投递。

任务完成和中断只是本机 rollout 记录可验证的近似，不证明任意 Codex 任务在所有环境中均已完成。没有本地 rollout 文件、字段变化、云端运行或本机记录不完整的情况不在保证范围内。

## 异常中断判定

“任务异常中断”不是对所有失败请求的统称。当前实现只把 rollout JSONL 中匹配到的 `event_msg.payload.type = "turn_aborted"` 作为异常中断候选，并且还要通过静默确认和生命周期二次确认，才会发送通知。

- 手动中断：如果 Codex 在本机 rollout 中写入 `turn_aborted`，会计入；没有写入该事件时，当前实现无法仅凭用户操作推断。
- 自动中断：只有自动流程最终同样写入 `turn_aborted`，才会计入。当前代码没有把“自动”作为单独的判定来源。
- 401、403、5xx、网络故障：这些首先由现有令牌健康和自动切换流程处理，本身不会直接生成“任务异常中断”通知。自动切换成功或失败分别属于独立的令牌事件；只有同时观察到 `turn_aborted`，才可能另外生成任务异常中断通知。

因此，当前无法从源码证明所有手动中断、自动中断或上游故障都会留下 `turn_aborted`。没有本地 rollout、rollout 没有该生命周期事件，或本地生命周期记录不完整时，不在这个通知事件的保证范围内。

令牌请求故障是独立事件，不依赖是否开启自动切换。达到健康阈值后，即使当前使用“手动提示”，也会按事件选择进入通知队列；自动切换成功或失败仍分别生成各自事件。未达到阈值的单次 401、403、5xx 或网络故障不会单独推送。

首次观察到 rollout 时只建立扫描光标，避免启用功能后补发历史任务。任务事件 ID 使用 `thread_id + turn_id` 的 SHA-256 派生值；外部本地事件使用其本地状态变化的身份派生 ID。两者只保存在本地，用于去重和二次确认。

## 状态机

所有状态均位于 CodexRelay 数据目录的 `task-notifications/`：

```text
rollout watcher -> pending -> outbox -> sent
                         \\-> suppressed
本地状态变化 -------------> outbox
outbox (失败) -> outbox 重试 -> dead
```

- `pending/`：观察到任务终态事件，等待静默确认。
- `outbox/`：确认后可发送的耐久记录。
- `sent/`：成功投递后的收据。
- `suppressed/`：等待期间发现 rollout 更新，或用户后来关闭对应事件的记录。
- `dead/`：记录格式无效、不可恢复 HTTP 失败，或超过最大尝试次数的记录。
- `watch/cursors.json`：每个 rollout 的读取位置及最近生命周期状态。

记录写入采用共享原子 JSON 存储；网络失败保留 `outbox`，按指数退避重试，最大等待不超过 15 分钟。HTTP 3xx、除 408/429 外的 4xx 会进入 `dead`；其他失败可按配置重试。切换数据目录时，消息通知状态会和 `config.json`、`usage.json` 一并迁移，目标已有同名状态会拒绝覆盖。

## 推送 URL 边界

投递只访问用户在设置中完整填写的 HTTP(S) URL，不允许 URL userinfo，也不支持界面配置认证头。CodexRelay 固定使用一次 `GET` 请求，不生成 JSON、不添加额外查询参数。URL 中的字面量 `{title}` 和 `{content}` 会在发送前分别替换为通知标题和单段正文，并进行 URL 编码；消息中的普通空格编码为 `%20`，字面加号编码为 `%2B`，例如 `https://www.pushplus.plus/send?token=xxx&title={title}&content={content}`。没有占位符的 URL 仍按填写内容直接访问。

测试通知和所有消息事件访问相同的 URL；请求均不发送 prompt、最终回复、路径、rollout 内容或 API 密钥。任务完成和异常中断正文使用 SQLite `threads.name` 作为任务名称，并使用全局状态中的本地项目名称；名称缺失时显示“未记录”文案，不显示 thread UUID。正文同时包含本地可确认的耗时、时间和异常原因，自动切换正文包含类别及前后分组，余额正文包含当前金额和提醒阈值。URL 本身可能含有第三方服务 token，因此不会显示在消息通知运行时错误或队列状态中。正文始终是单段文本，不使用 `\n`。

## 设置与运行

在“设置 > 消息通知”中启用功能，填写推送 URL，并勾选需要的事件。三个投递参数含义如下：

- 静默确认（秒）：任务完成或中断后，rollout 必须保持不变的时长；只作用于两类任务事件。
- 请求超时（秒）：每次访问推送 URL 最长等待时间；超时后按重试规则处理。
- 最大尝试次数：单个失败事件最多访问 URL 的次数；`0` 表示持续按退避策略重试。

启用后每两秒扫描一次本机 rollout；关闭时 watcher 不扫描或投递，已有队列状态会保留，重新启用后继续处理。

该功能不会修改 Codex 的 `config.toml`、`hooks.json` 或 `auth.json`，也不会读取或改写系统代理、DNS、路由、VPN 配置。
