# App API 运行与备份

Core 启用 app 后仍只需一个进程。`app.data_dir` 相对 Core YAML 文件目录解析，内部保存
inbox.sqlite（WAL/SHM）与 attachments/。不要把可写数据放进只读配置挂载。

`deploy/docker-compose.yml` 已为 Core 增加 `orbit-core-data:/app/data` 和仅绑定回环的
7622 端口；配置可用 `data_dir: ../data/app`、`listen: 0.0.0.0:7622`。
镜像准备的 /app/data 归属 uid/gid 65532；若改用宿主机绑定目录，需赋予该 UID 写权限。
未启用 app 配置时不监听 API，不改变既有 MQTT Node 行为。

## HTTPS

示例 Caddy 配置（替换域名，并按现有部署配置 DNS）：

```caddyfile
orbit.example.com {
    reverse_proxy 127.0.0.1:7622 {
        flush_interval -1
    }
}
```

HTTPS 终止于反向代理，不直接暴露 Core 明文端口。代理必须转发 Authorization，禁用
SSE 缓冲，并允许长连接；上传请求上限至少 10 MiB。不要记录 Authorization 或请求正文。

## 备份与恢复

首版使用停机备份：停止 Core，**整体备份 app.data_dir**（数据库、WAL/SHM、附件），
同时通过私有渠道保存设备配置。不要只复制仍在写入的 inbox.sqlite。

恢复：停止 Core，整体还原同一批备份，确认文件权限，执行：

```sh
go run ./cmd/orbit-core -config configs/core.local.yaml -reset-sync-generation
```

命令修改数据集 generation 后退出，不连接 Broker。随后启动 Core。App 下次前台同步
将重建权威缓存，待发送队列和本地草稿仍保留；过旧的编辑可能需要处理版本冲突。
正常重启不运行此命令。不要在运行中的 Core 上修改数据库或 generation。

设备撤销、令牌替换、路由调整通过修改配置并重启 Core 完成。每个物理 App 安装使用
独立 node_id/令牌；一个人所有已授权 App 共享条目。暂不提供多用户权限隔离。

## 与管理台共存

Core 管理台继续使用 `console.database`，App 使用 `app.data_dir`，二者放在同一持久卷的不同路径。
必须设置 `console.password`；App 的设备令牌保持独立。状态摘要路由通过管理台新增或编辑，
选择 `App · 收件箱`（`overview-app`），设备 ID 与 `app.devices` 的键一致。
YAML 路由仅在首次创建管理台数据库时导入；已有部署修改 YAML 不会覆盖已保存路由。
