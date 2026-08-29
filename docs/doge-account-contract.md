# 二狗子账户数据契约

本文件记录 CodexRelay 当前实现使用的二狗子 New API 账户接口字段。字段依据用户提供的官方接口响应样本；未列出的字段不进入本地账户快照。

## 请求与认证

账户接口使用绑定的 User 权限令牌；公开公告接口不带令牌。认证请求使用：

```text
Authorization: Bearer <访问令牌>
```

当前实现读取：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| GET | `/api/user/self` | 当前用户身份和钱包 quota |
| GET | `/api/subscription/self` | 当前套餐；只保存 `subscriptions` 中 `status=active` 的项目 |
| GET | `/api/user/topup/info` | 兑换开关和 `topup_link` 购买入口 |
| POST | `/api/user/topup` | 使用 `{ "key": "兑换码" }` 兑换额度 |
| GET | `/api/status` | 公开站点状态、公告列表和 `announcements_enabled` |
| GET | `/api/notice` | 公开当前公告正文 |

`GET /api/user/billing/redemption/self` 是兑换历史记录接口，不作为当前余额来源，因此当前版本不调用它。

令牌目录同步还会读取 `/api/token/` 返回的令牌 `id`、`status`、`group`、`name` 和掩码密钥。依据用户提供的脱敏接口样本，`status=1` 作为当前可用状态；后台同步发现当前已启用令牌从最新目录消失，或状态不再为 1、所属分组不再出现在当前权限目录时，会复用右下角令牌切换窗口提示用户。该提示、托盘和主窗口的切换入口共用当前类别下的可用集合：类别相同、状态为 1、分组仍在当前权限目录且本地已有完整密钥；候选不再按远端分组二次筛选。

令牌因状态或分组失效后再次恢复可用时，若原 Profile 仍在本地且该类别仍有活动 Profile，右下角会提示恢复的令牌；自动切换已经离开原 Profile 时，提示以当前活动 Profile 作为切换前置，并将恢复令牌放入可用候选。

## 公告接口证据

2026-08-22 通过 `https://api.ergouzi.life` 实际请求确认：

- `/api/status` 返回标准响应封装 `{"data": ..., "success": true}`；`data.announcements` 是公告数组，每项包含 `id`、`content`、`extra`、`publishDate`、`type`，同时返回 `data.announcements_enabled`。
- `/api/notice` 返回标准响应封装；`data` 是当前公告的 Markdown/HTML 混合字符串，可能为空或为 `null`。
- 公告正文是上游公开内容，不作为 HTML 直接注入页面。前端使用随程序嵌入的 `marked 18.0.10` 解析器处理 Markdown/HTML 混合内容，只保留允许的排版标签，并为 `http`/`https` 外部链接添加新窗口和 `noreferrer noopener`。主窗口和右下角公告提醒都完整保留正文；提醒窗口固定为 `430x300`，超出部分在正文区域内部纵向滚动，不追加省略号。
- 本地首次成功同步只建立公告缓存并将现有公告标记为已读，后续同步出现的新 `id` 才触发公告提醒，避免首次安装时一次性弹出历史公告。

未验证项：上游没有在本次公开响应中声明公告排序和保留期限；当前实现按接口返回顺序展示，并以 `id` 作为本地已读和提醒状态的稳定标识。

## 金额换算

当前二狗子实例的接口样本确认 `500000 quota = 1 美元`。配置文件保留上游整数 quota，桌面公开状态只返回美元：

```text
钱包美元 = user.quota / 500000
套餐剩余美元 = (subscription.amount_total - subscription.amount_used) / 500000
总剩余美元 = 钱包美元 + 所有有效套餐剩余美元之和
```

套餐状态、标题和到期时间来自订阅对象及其 `plan.title`；历史套餐不会计入总额。

## 安全边界

- 绑定访问令牌只写入便携 `config.json`，不返回到 `DesktopState`。
- 兑换码只存在于兑换请求体和本次调用栈，不写入配置、日志、fixture 或错误正文。
- 兑换成功后重新读取用户、套餐和充值配置，不通过余额差值推算兑换结果。
