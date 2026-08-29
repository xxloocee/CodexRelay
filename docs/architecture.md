# CodexRelay 架构

## 目录与职责

```text
CodexRelay/
|- cmd/
|  |- main.go                       # 参数解析和不可恢复启动错误
|  `- resource_windows_amd64.syso   # 构建生成的 Windows 资源
|- internal/
|  |- desktop/                      # Wails 应用、绑定服务、窗口与托盘
|  |  `- clientconfig/               # 外部客户端配置适配器、格式辅助与备份写入
|  |- config/                       # 当前配置模型、校验与 config.json
|  |- relay/                        # 运行时快照、认证和反向代理
|  |- network/                      # 出站 Transport 与系统代理只读检测
|  |- usage/                        # usage 旁路观察、聚合与 usage.json
|  |- tasknotify/                   # 本机 Codex rollout、SQLite idle gate 与耐久 Webhook 队列
|  |- storage/                      # 原子 JSON 读写和平台文件替换
|  `- platform/                     # 开机启动与致命错误平台实现
|- frontend/                        # 嵌入 Wails 的前端与生成 bindings
|  `- vendor/                       # 随程序嵌入的固定第三方前端资源
|- build/windows/                   # ico、manifest 构建输入
|- docs/                            # 架构和长期设计事实
|- test/                            # 独立的脱敏界面与集成冒烟测试
|- dist/                            # 本机构建产物与便携数据
|- embed.go                         # 映射 frontend 与唯一 logo.png
|- logo.png                         # 品牌图标唯一源文件
`- build.ps1                        # 测试、bindings、资源和 EXE 构建
```

包依赖保持单向：

```text
cmd -> desktop
desktop -> config, relay, usage, platform, desktop/clientconfig
desktop -> tasknotify
desktop/clientconfig -> config, storage
tasknotify -> storage
tasknotify -> SQLite（仅只读查询 Codex 本地状态）
relay -> config, network, usage
config -> network, storage
usage -> storage
```

`network`、`storage` 和 `platform` 不依赖业务包。前端只通过 `frontend/api.js` 间接引用 Wails 自动生成的 bindings，生成目录不手工维护。根包只负责嵌入静态资源，`logo.png` 不复制到其他源码目录。

## 请求链路

1. `cmd` 解析 `--autostart` 和 `-proxy-port`，调用 `desktop.Run`。
2. `desktop` 只从用户数据目录加载 `config.json` 和 `usage.json`，装配 Wails、托盘和本地 HTTP 服务；程序运行目录不参与配置定位。
3. `relay.Runtime` 将当前配置编译成不可变快照；切换类别下的代理 API 后，新请求立即读取新快照，已开始的请求继续使用原目标。
4. `relay.Proxy` 校验本地访问令牌，保留方法、路径、查询和请求体，只替换上游目标、`Authorization` 与明确配置的额外请求头。
5. `network` 根据跟随系统、直接连接或指定代理创建独立 Transport，不修改 Windows 系统代理、DNS、路由或 VPN。
6. `usage.Observer` 只观察流经客户端的响应字节副本。记录失败或未发现真实 usage 时写入 `unreported`，不改变代理响应。

消息通知不进入上述请求链路。`tasknotify.Manager` 由 desktop 生命周期独立托管：任务完成和中断只在设置开启后扫描本机 Codex rollout JSONL，先写入 `pending/`；随后以 Codex 本地 SQLite 的 root/subagent、active goal、`thread_spawn_edges` 和子代理 rollout 生命周期作为二次 idle gate，确认静默窗口内没有更晚生命周期事件后，才原子提升到 `outbox/`。SQLite 只读查询失败时保留 rollout-only 降级行为。令牌自动切换结果及已有二狗子低余额提醒状态由 desktop 在已确认的本地状态变化后直接写入 `outbox/`。后台 HTTP worker 访问用户配置的完整 HTTP(S) 推送 URL，只替换 `{title}`、`{content}` 两个占位符并进行 URL 编码。字段证据、状态机与不支持范围见 [task-notification.md](task-notification.md)。

## 持久化契约

`config.Store` 和 `usage.Store` 共用 `storage.WriteJSONAtomic`：在目标目录创建临时文件，完整写入并同步，再用平台实现替换目标。读取或校验失败不会覆盖原文件。默认数据目录是当前用户目录下的 `.CodexRelay`（Windows 默认形如 `C:\Users\<用户>\.CodexRelay`），不再读取程序运行目录的配置文件。用户切换目录时，桌面服务在运行时锁内把两个 JSON 快照写入目标目录，拒绝覆盖目标同名文件，提交后切换两个 Store 并清理旧目录中的这两个文件。

自定义目录通过默认 `.CodexRelay\.active-directory.json` 路径指针定位，因此重启时无需扫描运行目录或猜测历史位置。指针损坏会直接报告错误；切回默认目录时删除指针。路径指针不包含 API 密钥或用量正文。

首次启动且任一便携数据文件缺失时，桌面服务使用延迟持久化存储：配置、公告同步和用量记录只保留在内存，不会因启动、定时同步或关闭窗口前的运行产生 `config.json`、`usage.json`。用户点击“暂时跳过”或绑定令牌成功后，当前内存快照才写入两个文件，并恢复后续正常保存；写入失败会保留引导状态，避免下次启动误认为初始化已完成。

`config.json` 当前不包含版本字段；未知字段会被忽略，程序只校验当前实际使用的配置字段，不迁移、不修补旧字段，也不读取 `%APPDATA%` 等历史位置。API 密钥明文存于 `Profile.apiKey`，这是便携产品的明确选择；`usage.json`、日志和错误信息不得包含密钥。

消息通知配置位于 `config.taskNotification`；扫描光标、候选、outbox、收据和失败记录位于相同数据目录的 `task-notifications/`。数据目录切换时，该私有目录先复制，目标已有同名状态会拒绝覆盖；主配置迁移成功后才删除旧副本。队列中的本机 thread/turn 标识、rollout 路径和本地事件身份只用于去重和二次确认；任务正文使用 SQLite `threads.name` 与全局状态中的本地项目名称，名称缺失时使用未记录文案，不把 UUID 作为用户可见内容；其他正文按事件类型发送耗时、分组或余额等允许展示的信息，不发送 rollout 路径、turn 标识、API 密钥或原始请求内容。

代理 API 配置同时记录 `source`（`doge` 或 `custom`）和 `category`（`codex`、`claude`、`gemini`、`grok`、`opencode`、`openclaw`、`hermes`、`image`、`other`）。每个 profile 还可以保存用户从上游获取或手动维护的 `models` 与 `defaultModel`，用于编辑页管理和外部客户端写入；模型目录不参与代理转发格式转换。`activeProfiles` 以类别为键保存启用项，每个类别最多一个。`failoverOrder` 按类别保存跨来源的统一 Profile 顺序，`tokenSwitch` 保存手动/自动模式、各异常触发开关、阈值、统计窗口和列表末尾循环策略；单个 Profile 的 `skipAutoSwitch` 只在自动模式中排除该候选。

`config.preferences` 保存桌面展示偏好：`visibleCategories` 是主页允许展示的类别列表，隐藏类别不会删除配置、令牌或停止代理；新配置的 `defaultSource` 默认为空字符串（全部来源），`defaultCategory` 默认为 `codex`，两者只决定主界面首次加载及“默认视图”恢复时的筛选；`restoreViewMode` 为 `current` 或 `default`，分别表示恢复窗口保留当前筛选或应用上述默认筛选。主页至少保留一个可见类别，默认类别必须属于可见类别。托盘、第二实例和任务栏恢复只读取本地快照并更新前端筛选，不触发上游同步。

`config.doge` 保存二狗子 New API 的绑定状态、当前用户分组、分页令牌目录、令牌顺序、同步间隔和最近同步状态。自动同步默认每 3 分钟，可选 1、3、5、10、15、30 分钟或 1 小时。`tokenOrder` 使用令牌接口返回的远端令牌 ID 作为稳定身份，不依赖可能变化的 API 密钥、名称或分组；同步会保留仍存在的旧顺序并追加新令牌。分组接口返回的对象键作为令牌 `group` 匹配键，`display_name` 保存为 `groupDisplayName`，`ratio` 保存为 `groupRatio`，主页和设置只展示展示名与倍率。每个令牌的 `category` 保存本地存放类别，`note` 保存首次同步生成的 `sk-掩码 · 额度摘要`；用户没有自定义备注时，同步会更新旧的自动备注，手工备注则保留。首次绑定和手动同步发现待分配令牌时，前端立即弹窗批量保存用户选择；后台自动同步只设置主页高亮“待导入”，未选择类别前不创建 Profile，也不进入故障顺序。远端 `group` 参与当前权限目录判断，但不作为同类别候选的额外分组筛选条件。主窗口、托盘和令牌切换提示共用“当前类别、状态正常、分组有权限且本地有完整密钥”的可用集合。成功同步确认远端令牌消失时，同时删除其本地 Profile、启用映射和故障顺序；解除绑定执行相同清理并移除全部二狗子 Profile，自定义 API 不受影响。管理访问令牌按产品的便携明文配置策略保存，但不会通过 `DesktopState` 返回；主页只显示令牌掩码。首次绑定和用户点击手动刷新时，桌面服务会为当前所有令牌调用密钥接口并把完整密钥保存到本地；启动恢复和设置中的定时同步只刷新用户、分组、令牌元数据、套餐与额度，已有令牌沿用本地密钥，只有新令牌或本地缺失密钥的令牌才调用密钥接口。编辑、启用和切换只读取本地完整密钥，缺失时提示先手动同步，不隐式访问上游；完整密钥统一保存为 `sk-` 前缀。已写入本地的二狗子密钥在编辑页只读，名称、地址、备注、类别和请求头仍可修改。同步失败只更新状态，不阻断本地代理转发。

二狗子管理请求使用 `/api/user/self`、`/api/user/self/groups`、分页 `/api/token/`；绑定和手动同步通过 `POST /api/token/{id}/key` 获取完整密钥，定时同步仅为新增或本地缺失密钥的令牌调用该接口。实际代理连通性仍通过令牌自身访问 `/v1/models` 探测。代理路由覆盖所有已知类别；生图和其他类别只记录请求次数，不旁路解析 Token 用量。

同步性能约束：账户、分组、令牌目录、套餐、兑换配置以及公告基础接口并行获取；基础阶段完成后，令牌完整密钥最多使用 8 个并发请求。自动同步沿用本地完整密钥，只为新增或缺失密钥的令牌进入密钥阶段。

外部客户端配置由 `config.clientConfigs` 保存每个类别的配置目录和主配置文件名。启动时只检查各适配器定义的默认位置，不遍历磁盘；发现目录后写入当前数据目录，高级设置可以通过原生目录选择器覆盖目录，选择外部路径不会迁移外部软件文件。每个支持自动配置的类别还保存 `skipConfigReplacement`；勾选后主页启用或切换直接执行本地切换，不读取或改写外部配置。未勾选时，CodexRelay 只读检查本地配置是否同时包含当前类别的本地请求地址和本地访问令牌，已配置则直接切换，未配置才显示简化提示。选择“跳过”只执行本地切换，选择“配置”先为每个将要改写的文件创建 `<原文件>.<时间戳>.CodexRelay` 备份，再用原子写入更新已确认的适配字段，最后执行本地切换；配置失败不会改变启用映射。外部写入只使用 profile 已保存的模型目录，不会在切换时隐式请求上游。Codex 的地址写入 `config.toml`，密钥写入 `auth.json`，状态检查会同时读取两者。当前适配器覆盖 Claude、Gemini、Codex、Grok、OpenCode、OpenClaw 和 Hermes；OpenClaw 读取标准 JSON 和参考项目使用的 JSON5，写回为合法 JSON；生图、其他类别保留手动配置。模型获取路径、字段和客户端模型结构见 `docs/model-management.md`。

公告同步在账户同步的基础阶段中调用公开的 `/api/status` 和 `/api/notice`，两个接口并行请求；未绑定账户时仍可独立同步公告。公告内容、当前公告、未读公告 ID 和提醒确认 key 保存到 `config.doge.notifications`。余额按账户 ID、套餐按套餐 ID 保存提醒记录和确认状态；恢复到配置阈值以上时清理对应记录，过期超过 7 天或已从上游删除的套餐不再保留。套餐在过期或进入 24 小时到期窗口时，只有剩余额度高于套餐提醒阈值才提示，避免低余额套餐重复产生到期提醒。首次绑定只建立当前额度基线，后续启动和同步继续按持久化状态判断。

余额、套餐、公告和每个 API 类别的令牌切换是相互独立的提醒窗口；同一类型的多条内容合并在一个窗口，不同类型或不同令牌类别可以同时显示。窗口生命周期不依赖主窗口，主窗口可见、最小化或隐藏到托盘时都按自身状态显示；启动同步早于窗口就绪时，会在窗口就绪后重放当前状态。窗口固定为 `410x280`，从屏幕右下角向上排列，空间不足时换列并限制在工作区内。公告正文进入界面前由随程序嵌入的 `marked 18.0.10` Markdown 解析器转换，再经过允许标签过滤，正文超出窗口时在正文区域内部纵向滚动，不截断内容。HTTP(S) 外链由后端校验并交给系统默认浏览器，打开失败时在当前提醒窗口显示错误。提醒状态不影响代理请求。

代理转发完成后只旁路观察上游 HTTP 状态和传输错误，不记录请求正文或认证材料。所有来源的活动 Profile 共用故障设置：`401` 与 `403` 分别连续计数，`5xx` 与连接错误按配置窗口累计；默认认证阈值为 5 次，上游异常阈值为 3 分钟内 5 次。被关闭的触发类型不累计，开关变化时清理该类型旧统计，重新开启不会立即复用关闭期间的错误。后台目录同步另行比较当前活动二狗子令牌与最新 `/api/token/` 目录：令牌消失、`status` 不再为 1 或所属分组不再可用时，也按对应开关进入同一故障流程；状态或分组失效的令牌恢复后，若原 Profile 仍在本地且类别仍有活动 Profile，会提示恢复项，自动切换已离开原 Profile 时以当前活动项作为切换前置。

手动模式显示当前类别按 `failoverOrder` 排列的可用候选；自动模式从当前项向下切换，并按 `loop` 决定到达末尾后是否回到第一项，`skipAutoSwitch` 只在自动模式过滤。目录消失上下文只保留同步前当前项的位置锚点，候选在每次提示和执行前按最新模式、循环、跳过状态、Profile 顺序和目录状态重算。自动模式的故障轮次只存在本次运行内，每个 Profile 最多尝试一次；全部耗尽后停止，新增可用候选可以继续该轮，当前活动 Profile 收到成功响应后清理轮次并允许开始下一轮，同时刷新已显示的手动故障提示。切换成功会清零原 Profile 的健康统计，用户取消后同一失败状态持续期间不再提示，并在恢复后继续抑制 5 分钟。

统计永久保留累计聚合、最近 90 天每日聚合和最近 300 条请求明细。明细不保存请求正文、响应正文或认证头。

## 启动与窗口

普通进程启动创建可见窗口。主页首次加载使用 `preferences.defaultSource` 与 `preferences.defaultCategory`；恢复策略为 `default` 时，托盘、第二实例和任务栏恢复会重新应用相同筛选，`current` 则保留当前筛选。上述恢复路径不申请二狗子同步。代理默认监听 `127.0.0.1`，开启 `config.listenOnAllInterfaces` 后监听 `0.0.0.0`，WSL2 使用 Windows 主机地址访问；Codex 的默认地址为 `http://127.0.0.1:8765/codex`，其他类别分别使用对应的类别前缀。Windows Run 注册表值为：

```text
"<绝对路径>\CodexRelay-<版本>.exe" --autostart
```

窗口只有在同时满足 `--autostart` 和 `preferences.startHidden=true` 时初始隐藏。关闭到托盘后代理继续运行；第二实例、托盘单击或“打开”都会恢复、显示并聚焦已有窗口，但不会因此请求上游同步。首次启动绑定账户后立即执行一次目录元数据同步，之后仅由配置的定时任务执行；后台同步更新状态后，原生托盘刷新会发出 `relay-state-changed`，主界面监听该事件读取最新快照，避免隐藏窗口停止轮询后恢复时继续显示旧目录。

## 构建边界

`build.ps1` 固定构建 `windows/amd64`，从 `internal/desktop/service.go` 动态读取 `applicationVersion`，输出唯一的 `dist/CodexRelay-<版本>.exe`；脚本从根 `logo.png` 生成 `build/windows/app.ico` 与 `cmd/resource_windows_amd64.syso`，重新生成 `frontend/bindings`，运行 Go 测试、vet 和前端语法检查，最后构建 GUI 子系统 EXE。生成的 EXE 嵌入前端、bindings 和品牌资源，不依赖源码目录。

Windows 发布同时保留 NSIS 安装包和按架构命名的独立 EXE。应用内更新通过 GitHub 公共发布页的 `/releases/latest` 重定向识别稳定版本，不调用有匿名配额限制的 GitHub API；随后按固定规则下载 `CodexRelay-<版本>-<架构>.exe`，并要求 `SHA256SUMS` 中存在匹配摘要。Wails updater 负责下载、校验、当前程序备份、原子替换和重启；它不更新 NSIS 卸载注册信息，macOS 不初始化该更新器。
