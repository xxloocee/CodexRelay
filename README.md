# CodexRelay

CodexRelay 是使用 Go + Wails v3 开发的 Windows 桌面程序，同时运行一个只监听本机回环地址的透明 API 反向代理。Codex 始终请求固定的本地地址，CodexRelay 将新请求转发给当前选中的上游并替换 API 密钥。

程序默认不会读取或修改外部客户端配置；用户在高级设置中点击“配置”并确认后，才会按适配器写入对应配置，并为原文件创建 `.CodexRelay` 备份。程序不会修改 Windows 系统代理、路由、DNS、网卡或 VPN 设置。

## 运行与便携数据

双击 `dist\CodexRelay-<版本>.exe` 会显示 Wails 原生窗口。程序采用单实例模式，再次双击会恢复并置前已有窗口。

- 本地 API 地址：`http://127.0.0.1:8765/{codex|claude|gemini|grok|opencode|openclaw|hermes|image|other}`
- 配置文件：`C:\Users\<当前用户>\.CodexRelay\config.json`
- 用量统计：`C:\Users\<当前用户>\.CodexRelay\usage.json`
- 本地访问令牌：首次运行生成，以 `sk-` 开头

首次启动引导在用户点击“暂时跳过”或“绑定并开始使用”前只使用内存状态；直接关闭程序不会生成 `config.json` 或 `usage.json`，下次启动仍会显示引导。完成任一操作后，程序才会在当前数据目录创建并持续更新这两个文件。

高级设置可以选择其他 CodexRelay 数据目录。切换时会迁移两个 JSON 文件，目标已有同名文件时拒绝覆盖；默认目录中的 `.active-directory.json` 只保存当前自定义目录，确保重启后继续使用新路径。程序不会读取 EXE 同目录、`%APPDATA%` 或其他历史位置的配置。上游 API 密钥按产品约定明文保存在 `config.json` 并在编辑页直接显示，因此数据目录应由用户自行管理访问权限。

`config.json` 当前不包含版本字段。程序不迁移、不修补旧字段；配置只从当前数据目录读取，并按当前字段校验。

## 代理 API 与统计

添加代理 API 后选择来源和类别。每个类别最多启用一个代理 API；正在进行的请求继续使用原代理 API，新请求立即使用该类别的新代理 API。列表手柄支持鼠标拖动和键盘方向键排序，顺序会持久化到 `config.json`。备注说明显示在 API 地址下方。

本地路由按类别区分：`/codex`、`/claude`、`/gemini`、`/grok`、`/opencode`、`/openclaw`、`/hermes`、`/image`、`/other`。类别前缀只负责选择对应的启用项，后续路径原样转发；旧 `/v1` 地址不再支持。来源筛选包含“二狗子”和“自定义”；绑定二狗子后，令牌目录也会出现在“全部”来源中。首次绑定和手动同步发现新令牌时，会立即要求为每个令牌选择本地存放类别；后台自动同步只在主页右上角显示高亮“待导入”，用户完成类别选择后才创建本地 Profile 并加入故障顺序。远端分组只用于展示和可用性判断，不限制同一类别中的跨来源排序。

每个类别使用主页从上到下的统一故障顺序，自定义 API 与已导入的二狗子 Profile 可以相互拖动。通用设置可选择“手动提示”或“自动切换”，分别控制 401、403、5XX、连接异常、目录失效和目录消失，并可调整认证次数、异常次数、统计窗口及列表末尾是否循环。自动模式下可为单个 Profile 设置“跳过此令牌”；手动模式仍允许选择该项。自动切换在本次运行的一轮故障中只尝试每个 Profile 一次，全部失败后停止，当前令牌重新请求成功后才开始新一轮。

设置中的“高级”页面可以为 CodexRelay 选择数据目录，并为各外部客户端选择配置目录。首次启动只检查已知默认目录，不扫描整块磁盘；外部客户端路径选择只保存读取位置，不迁移外部软件文件。启用或切换类别时会即时检查本地请求地址和密钥；勾选“跳过配置文件替换”后，该类别直接切换，不检查也不改写外部配置。未勾选时可选择“跳过”直接切换，或选择“配置”自动备份并写入 Codex、Claude、Gemini、Grok、OpenCode、OpenClaw、Hermes 的已确认字段；生图和其他类别需手动配置。

设置“通用”中的“主页显示”可以隐藏不需要的类别；隐藏只影响主页列表和筛选，不删除配置或停止代理。新配置的主程序默认来源为“全部”，默认类别为“Codex”；主程序默认来源、默认类别决定启动时的主页筛选。“恢复窗口时显示”选择“当前”会保留最小化或托盘前的筛选，选择“默认”会在恢复时回到默认筛选。窗口恢复只读取本地缓存，不会额外请求上游 API。

设置“连接”中的二狗子自动同步默认每 3 分钟，可选择 1、3、5、10、15、30 分钟或 1 小时；同步失败只保留错误状态，不阻断本地代理转发。成功同步发现上游令牌已删除时，会同时删除对应本地 Profile、启用映射和故障顺序；解除绑定会清理全部二狗子 Profile，但保留自定义 API。

余额、套餐、公告和各 API 类别的令牌切换使用独立的右下角窗口，不依赖主窗口是否可见。同一提醒类型的多条内容合并显示：余额按账户 ID、套餐按套餐 ID 把确认状态写入 `config.json`，恢复到阈值以上后清理；套餐过期或进入 24 小时到期窗口时仅在剩余额度高于提醒阈值的情况下提示。绑定阶段只建立当前提醒基线，不弹出历史额度提醒，后续启动和同步按保存状态继续判断。

用量统计只旁路读取 Responses API 真实返回的 `usage`，不修改响应字节。上游未返回、响应压缩、事件过大或流提前中断时显示“未上报”，不会估算 Token。生图和其他类别只记录请求次数，不读取或保存 Token 用量。

- 今天、7 天、30 天和全部时间范围
- 按代理 API 筛选
- 永久累计总数与最近 90 天每日汇总
- 最近 300 条请求明细
- 不保存请求体、响应正文或 API 密钥

## 系统托盘与启动

- Windows 普通最小化仍保留在任务栏。
- 默认关闭窗口会隐藏到系统托盘，代理继续运行。
- 托盘可恢复窗口、切换代理 API、复制本地 Codex API 地址或退出。
- 普通双击启动始终显示主窗口。
- Windows 开机启动命令会附带 `--autostart`；只有这种启动方式才会应用“开机启动时隐藏”。

## 消息通知

“设置 > 消息通知”可为任务完成、任务异常中断、令牌请求故障、令牌自动切换、令牌自动切换失败、账户余额不足和套餐余额不足分别启用独立推送。所有已选事件都直接访问用户填写的完整 HTTP(S) 推送 URL；URL 的 token、标题、内容等参数由用户自行填写，可用于 Server 酱、PushPlus 等 URL 通知服务。任务事件经过 `pending -> outbox -> sent/dead` 耐久状态机，其他本地状态变化直接进入 `outbox`；网络失败按退避重试，首次启用只建立 rollout 扫描位置，不补发历史任务。

通知不位于透明代理链路，不修改 Codex 的 `config.toml`、`hooks.json`、`auth.json` 或系统网络设置。Webhook 不发送 prompt、最终回复、路径、turn 标识、令牌标识或认证材料；任务通知正文会读取 SQLite `threads.name` 和全局状态中的本地项目名称，包含任务名、项目名、耗时和时间，名称缺失时不会显示 UUID；余额通知会包含当前金额和提醒阈值，自动切换通知会包含前后分组。令牌请求故障在 401、403 连续达到阈值，或 5xx、网络故障在统计窗口内达到阈值时推送，与自动切换开关独立；任务异常中断当前只认本机 rollout JSONL 中的 `turn_aborted`，401、403、5xx 或网络故障本身不直接算作该事件；任务事件当前仅基于本机 rollout JSONL 的已验证字段判断，不承诺覆盖没有本地生命周期文件的任务。详见 [消息通知说明](docs/task-notification.md)。

## 网络出口

- `跟随系统`：读取 Windows 当前显式 HTTP/HTTPS 代理，VPN 和 TUN 路由照常生效。
- `直接连接`：不使用显式 HTTP 代理，但仍遵循 Windows 路由。
- `指定代理`：将出站请求交给指定 HTTP 代理，例如 `http://127.0.0.1:7890`。

程序拒绝把上游代理指向自己的监听端口，避免代理循环。

## 转发规则

请求方法、路径、查询、请求体、Content-Type、Content-Encoding 和响应流保持不变。代理只切换目标主机与路径、替换 `Authorization`、应用配置的额外请求头，并由 Go HTTP 栈处理逐跳请求头和连接管理。

上游必须原生兼容 Codex 使用的接口。CodexRelay 不转换 Responses API 与 Chat Completions API。

## 参数、构建与测试

```text
--autostart      标记为 Windows 开机自启动；不建议手工使用
-proxy-port      覆盖透明代理端口
```

```powershell
go test ./...
go vet ./...
node --check frontend\app.js
node --check frontend\api.js
.\build.ps1
```

构建脚本从 `internal\desktop\service.go` 的 `applicationVersion` 动态读取版本号，`dist` 只保留一个带版本号的 EXE。

Windows 版本启动后会检查官方 GitHub Release。发现新版本时，用户可以在“设置 > 关于”确认下载；程序只接受当前架构对应的独立 EXE，并使用 Release 中的 `SHA256SUMS` 校验后原子替换当前程序、自动重启。应用内更新替换程序本体，不重写 NSIS 卸载注册信息；macOS 当前不启用应用内更新。

## GitHub 发布

推送 `v` 开头的版本标签后，GitHub Actions 会自动构建并发布四个安装包、两个 Windows 应用内更新 EXE 和 `SHA256SUMS`。文件名中的
`x64` 和 `arm64` 都是 64 位架构，当前不提供 32 位 `x86` 包：

- Windows Intel/AMD：`CodexRelay-<版本>-windows-x64-setup.exe`
- Windows ARM：`CodexRelay-<版本>-windows-arm64-setup.exe`
- Windows 应用内更新（Intel/AMD）：`CodexRelay-<版本>-amd64.exe`
- Windows 应用内更新（ARM）：`CodexRelay-<版本>-arm64.exe`
- Windows 更新校验：`SHA256SUMS`
- macOS Intel：`CodexRelay-<版本>-macos-x64.dmg`
- macOS Apple 芯片（M1/M2/M3/M4）：`CodexRelay-<版本>-macos-arm64.dmg`

选择方法：Windows 在“设置 > 系统 > 系统信息”的“系统类型”查看；macOS 在“关于本机”中，
显示 Intel 处理器时选 `x64`，显示 Apple 芯片时选 `arm64`。

```bash
git tag v1.0.0
git push origin v1.0.0
```

发布工作流位于 `.github/workflows/release.yml`。当前工作流生成未签名安装包；面向公开用户发布时，还应在 GitHub Actions 中接入 Windows Authenticode 和 Apple Developer ID 签名、公证凭据。

源码边界和运行链路见 `docs\architecture.md`。功能图标采用 CC Switch 使用的 Lucide `0.542.0` 图标集，许可位于 `frontend\icons\LICENSE-lucide.txt`；品牌图标唯一来源是根目录 `logo.png`。
