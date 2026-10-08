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
- `POST /api/app/devices`：`{label, platform}`（android/macos/windows），生成设备 ID 与随机令牌，只在本次响应返回明文。
- `POST /api/app/devices/{id}/rotate`：替换令牌并恢复授权；`.../revoke`：撤销访问，保留内容。
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
