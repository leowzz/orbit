# App API v1

Core 可选开启 `app.listen`；公网接入由反向代理提供 HTTPS。API 与 MQTT 连接独立，
所有条目在本人已授权设备间共享。每次请求（含图片与 SSE）携带
`Authorization: Bearer <device token>`；查询参数不接受令牌。身份只从令牌解析。

管理台「App 与收件箱」登记设备并生成独立令牌；令牌只显示一次。
首次启动时从 `app.devices` 导入已有设备，之后以持久化的设备登记为准。
令牌至少 32 字符，建议生成 32 随机字节；服务端配置仅保存 SHA-256 十六进制摘要。
在管理台撤销访问或更换令牌立即生效，并断开旧 SSE 连接；配置文件不会覆盖这些变更。
设备的状态路由使用 `overview-app`，缺少路由时仍可收发条目，状态为 `null`。

## 条目与操作

Item 字段：`id`、`kind`（text/todo/image）、`body`、`completed`、可选
`attachment_id`、`created_by_node_id`、`revision`、`created_at`、`updated_at`、可选
`deleted_at`。时间为 UTC RFC3339，**所有 revision、cursor、seq 均为十进制字符串**。
Item ID 和 operation ID 使用 UUID。正文最多 16000 UTF-8 字节，HTTP 操作体最多 32 KiB。

`POST /api/v1/operations` 成功返回 200 和持久化后的 Item。

```json
{
  "operation_id": "8bb52cb8-3e6e-4fd5-986a-f5ee18a8a031",
  "item_id": "706be0b2-a824-402c-b8c4-2bce075db8ab",
  "type": "create",
  "expected_revision": "0",
  "kind": "todo",
  "body": "整理周报"
}
```

| type | 额外字段 | 规则 |
| --- | --- | --- |
| create | kind、body；image 需要 attachment_id | 新 UUID，expected_revision 为 "0"；文本/待办正文非空 |
| update | body | 携带最新 expected_revision；不改变类型/附件 |
| set_kind | kind: text/todo | 仅文本和待办互转，保留正文；类型改变时清除完成状态 |
| set_completed | completed: true/false | 仅待办；目标值语义 |
| delete | 无 | 所有 App 隐藏，保留记录和附件，可在控制台恢复 |
| restore | 无 | 仅管理台接口可调用；携带已删除条目的最新 expected_revision，恢复后同步到所有 App |

除 create/set_kind 外不传 kind；除 create 外不传 attachment_id。正文不接受 node_id。
同一设备、同一 operation_id 的同内容重试返回原结果，先于版本校验；不同内容拒绝。
Item 修改、Change、Receipt 在同一 SQLite 事务提交。成功回执和变更历史当前永久保留。
冲突返回 409 `{"code":"conflict","current":<Item>}`。客户端保留草稿，由用户决定。

## 读取与同步

- `GET /api/v1/status`：`{node_id,label,view}`。view 使用现有 DeviceView 的 Protobuf JSON
  字段（camelCase、int64 字符串），不含 Agent 身份；freshUntil 决定是否过期。App 不提供
  打开主机会话操作，不把 HTTP 请求受理当作执行成功。
  `overview-app` 的 Codex 来源还可提供 `view.codex.weeklyLimit`：
  `remainingPercent`（double，proto3 省略时为 0）、`resetsAt`、`observedAt`、`freshUntil`。
  对象缺失表示没有周额度数据；额度新鲜度由自己的 `freshUntil` 和 Codex 来源的
  `freshUntil` 共同限制，不能用会话更新延长。`resetsAt` 到期后不得继续显示旧比例或
  自行恢复满额，应等待新的观察值。此字段是向后兼容的新增字段。
- `GET /api/v1/sync/snapshot?limit=100`：首次一致快照。
- `GET /api/v1/items`：与 snapshot 相同的浏览分页语义。初始按稳定 item UUID 排序；App
  同步到本地后按 created_at + id 倒序浏览，分批显示。服务端快照可含 tombstone。
- 后续快照页必须携带第一页面返回的 `generation`、`at=<cursor>`、`after_id=<next_id>`。
  所有页面在同一 high-water 边界从不可变历史重建，不受期间编辑/删除影响。
- `GET /api/v1/changes?generation=...&after=...&limit=100`：按 seq 升序返回完整条目。

统一分页返回：

```json
{"generation":"dataset-uuid","cursor":"12","items":[],"changes":[],"has_more":false,"next_id":"optional-item-uuid"}
```

快照填充 items；增量填充 changes：`[{"seq":"12","item":{...}}]`。
limit 为 1–200，默认 100。增量 cursor 是本页最后应用的 seq；只在本地事务成功后推进。
快照完整下载后原子替换缓存和游标，期间失败保留旧缓存与待发送队列。
正常 Core 重启不改变 generation；恢复旧备份后需执行 `-reset-sync-generation`。
跨 generation、负游标或超过当前 high-water 的游标返回 reset_required，重新取快照。
当前不裁剪历史，尚无增量过期窗口。

`GET /api/v1/events` 返回前台 SSE：连接后立即发送，写入后唤醒，每 15 秒心跳。

```text
event: sync
data: {"generation":"dataset-uuid","cursor":"12"}

```

SSE 仅提示追赶，不能推进本地 applied_cursor；相同 cursor 的心跳也可触发状态摘要查询。
客户端启动、SSE 重连、回前台和前台 15 秒巡检均追赶，关闭/后台停止订阅。

## 图片

`POST /api/v1/attachments` 直接发送二进制图片，成功返回 201：
`{id,mime,size,width,height}`。支持 JPEG/PNG/GIF，最大 10 MiB、2000 万像素；
服务端根据实际解码结果确定 MIME，生成最长边 480 像素的 JPEG 缩略图（GIF 取首帧）。

先上传完成，再 create image 引用该 ID。未引用附件仅上传者可读/引用；条目创建后本人
其他已授权设备可读。`GET /api/v1/attachments/{id}` 下载原图，加 `?thumbnail=1` 下载缩略图。
未被任何条目引用且超过 24 小时的附件每小时回收，崩溃遗留的孤立文件也会回收。已删除条目的附件保留，管理台仍可预览，恢复后 App 可重新读取。旧版本已经清理的附件无法恢复，恢复操作会返回 attachment_unavailable。
受有效条目引用的图片不会回收。正文编辑不替换图片；需要替换时创建新图片条目。

## 错误

统一返回 `{"code":"..."}`，不返回数据库错误或原始异常。

| HTTP | code |
| --- | --- |
| 401 | unauthenticated |
| 403 | device_revoked |
| 400 | invalid_fields、invalid_cursor、invalid_image、image_too_large |
| 404 | not_found、attachment_unavailable |
| 409 | conflict、operation_id_reused、reset_required |
| 503 | unavailable |

暂时失败保留同一 operation_id 重试；冲突/字段错误保留本地草稿供用户处理。
首版同一条目只允许一个未确认操作，等待保存或处理冲突后继续编辑。

## 管理台接口

以下路径仅在管理台监听器开放，复用管理台登录会话、Origin 和跨站请求检查；App 的 Bearer 令牌不能调用。

- `GET /api/app/devices`：启用状态、设备名称/系统/撤销状态，以及本次进程的连接数、最近访问、最近成功同步请求时间与返回游标；不返回令牌或摘要。
- `POST /api/app/devices`：`{label, platform}`（android/macos/windows/web），生成设备 ID 与随机令牌，只在本次响应返回明文。
- `POST /api/app/devices/{id}/rotate`：替换令牌并恢复授权；`.../revoke`：撤销访问，保留内容。
- `DELETE /api/app/devices/{id}`：删除设备登记并立即断开同步、使令牌失效；保留共享消息、附件与转发规则。重启不会再次导入 YAML 中的同名设备，再次使用需重新添加。
- `GET /api/app/items?after=...&kind=...&q=...&deleted=true`：默认未删除条目；deleted=true 仅列出已删除条目，支持同样的类型/正文筛选，每页 50 条，返回 `items` 和下一页 `next`；按稳定 ID 分页。
- `POST /api/app/operations`：复用 App 操作契约、版本冲突与幂等回执，以保留身份 `@console` 作为操作来源，触发相同同步通知。
- `POST /api/app/attachments` 与 `GET /api/app/attachments/{id}`：上传和读取共享图片，支持缩略图。未引用图片仍仅其上传来源可读。

连接状态只描述服务端观察，返回游标不等于设备已落盘游标。最近访问与同步请求时间在 Core 重启后重新统计。

### App 连接二维码

管理台在添加设备或轮换令牌后，本地生成 JSON 二维码，不调用外部二维码服务：

```json
{"type":"orbit-app","version":1,"server":"https://orbit.example.com","token":"<device-token>"}
```

`server` 使用当前管理台的 origin（部署时同域反代 App API）。二维码只在明文令牌
弹窗存续期间展示，不额外持久化。Android 校验类型、版本及与手动输入相同的地址/
令牌约束；识别后仅填入表单，用户点击连接才保存，不会直接访问二维码中的地址。

## 网页与同域访问

Core 的 App 监听器和管理台监听器均提供 `/inbox/` 网页和同一套 `/api/v1/` 设备接口；管理台会话不能代替设备授权。

- `GET /api/v1/browser` 返回 `{mode:"device"}`，不包含凭据。
- `POST /api/v1/session` 接受 `{token}`，验证设备授权后写入 HttpOnly、SameSite=Strict、远程 Secure Cookie（30 天）。不在响应中返回令牌。无效令牌不会被已有 Cookie 代替。
- `DELETE /api/v1/session` 清除当前浏览器 Cookie。
- Bearer 授权保持原契约；没有 Authorization 时才尝试 Cookie。Cookie 请求会校验 Origin 和 Sec-Fetch-Site，跨站请求返回 forbidden。
- 撤销或轮换设备后，已有 Cookie 和 SSE 连接同样失效。

Web Node 的可选 `web.inbox` 代理只转发消息、同步、状态和附件接口，使用自己的访问会话保护入口；不会代理管理台或上游会话接口。

### 统一展示规则

`ProjectionRoute.display_mode` 支持 `usage`、`codex`、`rotating`，与目的地 `profile` 分离。`rotation_seconds` 仅用于循环模式（1–3600 秒，省略默认 3 秒）。固定模式可以保留两种输入；Core 只投影当前页面，不修改来源配置。未指定模式时兼容原 OLED profile 和 App/Web/Android 综合视图。

`DeviceView.displayKind` 返回当前指标页 `usage` 或 `codex`；空值表示旧综合视图。账号周限同时返回格式化的 primary/secondary/footer 以及具有独立有效期的 `codex.weeklyLimit`。无数据时保留当前页标识并清空数值，不能继续显示上一页缓存。

Android 状态页、点阵屏和桌面组件共享 `/api/v1/status` 的设备授权。请求 `Accept: application/x-protobuf` 返回相同 DeviceView 的 protobuf 数据；无规则或无视图返回 204。JSON 响应保持兼容。消息同步、收件箱存储不受展示规则影响。

`ProjectionRoute.sessions_enabled` 独立控制 Session 信息，与 `display_mode` 无关。开启需要 Codex 数据来源，支持 Web、App 和 Android 会话组件；OLED 仅展示指标。开启时任一指标页均携带会话列表、项目、模型、状态及计数；关闭时不投影会话详情与计数。`DeviceView.sessionsEnabled`（可选 bool）明确标识该开关，即使来源暂无数据也保留。Web / App 按此字段显示或清空列表，切换指标不改变开关，收件箱不受影响。

兼容：未指定开关的旧综合视图沿用会话展示；旧固定/循环指标视图默认关闭。曾保存的 `display_mode=sessions` 按周限指标加会话详情解释，新界面不再提供该指标模式。

## 在 Mac 上打开会话

`POST /api/v1/sessions/{session_id}/open` 使用相同的设备 Bearer 令牌。
请求体：`{"core_epoch":"当前 view.coreEpoch","view_revision":"当前 view.metadata.revision"}`。
修订号沿用 Protobuf JSON 的字符串形式。客户端不能指定 node 或 agent；Core 根据认证设备的
`overview-app` 路由定位来源，要求会话信息开启、会话存在且来源数据未过期。
成功返回 HTTP 202：`{"intent_id":"…","status":"accepted"}`，仅表示指令已发出，
不代表 Mac 已完成打开。视图失效或会话不可用返回 409 `session_unavailable`；
指令通道不可用返回 503 `unavailable`。请求不进入离线收件箱队列。

## 工作台快捷导航

配置由桌面 Agent 管理；App 只读展示、点击打开。Core 根据设备现有工作台路由选取来源 Agent，
保留 Agent 中的入口顺序，并生成设备级快照；没有对应路由时返回空列表。多个来源按路由顺序
合并，同一 Agent 去重；任一来源启用标题时显示标题。

- `GET /api/v1/navigation`：`{revision,show_titles,items}`。条目包含 `id,title,kind,agent_id,shortcut_id,icon_url,icon_name`；`kind` 为 `agent`。`id` 是设备展示入口 ID，不能当作本机路径或 URL。
- `GET /api/v1/navigation/icons/{sha256}`：使用原设备授权下载 PNG。外部图标地址不得携带设备令牌。
- `POST /api/v1/navigation/{id}/open`：无需动作参数，Core 重新检查当前路由及目录，返回 HTTP 202 `{command_id,status:"sent"}`。入口已移除/来源已变更返回 409 `shortcut_unavailable`；202 仅表示指令已发送。

上述接口均复用设备授权；不提供 App 写配置、上传图标或浏览其他 Agent 目录的接口。
导航快照独立于收件箱数据，点击不进入离线队列。Agent 应用路径和网址目标仅保存在 Agent 本机。
