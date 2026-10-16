# Orbit Core Console

独立的 React + TypeScript 前端。只通过 `/api` 与 Core 通信，独立管理依赖和构建，
不依赖 Go 源码即可开发界面。生产构建由 Go 嵌入，开发时使用 Vite 代理 Core。

## 开发

要求 Node 22.12+（CI / Docker 使用 Node 24）与 pnpm 11.9.0。

仓库根目录一条命令启动 Core 后端与 Vite，不执行前端生产构建：

```sh
make dev-core
```

访问 `http://127.0.0.1:5173`，修改 React / TypeScript / CSS 即可热更新。
交互终端顶部固定显示前端地址和进程状态，下方按 `[Core]` / `[Vite]` 显示滚动日志。
Ctrl-C 停止两个服务及其子进程；任一服务退出时也会停止另一个服务。Go 代码修改仍需重启。
日志重定向、`TERM=dumb`、`ORBIT_DEV_PLAIN=1 make dev-core` 或同时启动 Agent 的 `make dev` 使用普通日志输出。`7620` 是后端及嵌入页面入口，
开发时请访问 `5173`，避免看到旧的构建产物。

也可以在两个终端分别启动：

```sh
# 仓库根目录：仅后端
make dev-core-api

# web/core-console 目录：仅前端
pnpm install --frozen-lockfile
pnpm dev  # 等价于 npm run dev，项目依赖仍由 pnpm 管理
```

API 代理目标在 `vite.config.ts`，默认为 `http://127.0.0.1:7620`，需与 Core YAML
的监听地址一致。Vite 固定使用 5173，端口占用时直接报错。
登录使用 Core YAML 的 `console.password`，不在前端存储密码或 Token。
会话有效期通过 `console.session_hours` 配置（正整数，单位小时），默认 72 小时（3 天）；
修改后重启 Core 生效。「系统信息」显示实际时长；退出登录或 Core 重启仍会使原会话失效。

## 构建

```sh
pnpm check
pnpm test
pnpm build
```

产物为 `dist/`，根目录 `make build-core` 自动先执行这里的构建，再生成包含页面、
脚本和样式的 Core 二进制。`embed.go` 仅提供 Go 构建桥接，前端无需导入它。
`dist/.gitkeep` 保留目录用于尚未构建时的 Go 包加载，其他产物不提交。

## 页面与数据

- `/`：总览、数据流向和全部已发现/已授权设备。
- `/agents`：电脑、Source 健康、数据新鲜度。
- `/nodes`：统一的 MQTT / App 设备列表、App 接入。
- `/nodes/:id`：设备详情，集中管理内容与显示、快捷操作、连接与访问。
- `/messages`：共享文本、待办和图片。
- `/system`：Core 身份、存储、会话和观测策略。

旧 `/routes?node=...` 链接跳转到设备详情；旧 `/apps` 跳转 `/messages` 收件箱；`/inbox/` 保留给个人设备网页。
数据投影仍由 Core 执行。快捷配对单独持久化为每台设备的执行端列表，支持多个 Agent 和
具备执行能力的 App Node；它不随数据来源变化。快捷栏标题、位置和工作台布局也由设备配置持有，支持用量区显隐、横屏宽度与所有 App 设备的「全面屏适配-左侧空隙宽度」。

API 类型集中在 `src/api.ts`，页面在 `src/pages/`，公共组件在 `src/components.tsx`。
设备详情标题旁的「改名」可修改 App 和 MQTT 设备名称，列表、总览与数据流向优先显示名称；设备 ID 和授权不变。App 复用已有名称字段，MQTT 名称保存在 Core 数据库，重启后保留。
设备详情标题旁的「归档」会将设备从默认列表、总览和数据流向隐藏；在「已归档」中可查看和恢复。归档只影响管理页展示，数据配置、同步、名称和访问权限均保留，重启或设备重新上报后也不会自动恢复展示。
设备详情直接选择显示内容和数据来源，保存后同步，无需再次选择设备 ID 或接收方式。
设备接入后自动发现；已有的预配置仍可在设备列表中管理，不再提供手动新建设备规则的旧表单。
快捷操作点击来源即可添加，默认跟随全部入口；展开「自定义」可挑选入口和排序。
切换内容与快捷操作标签会保留未保存的修改，支持撤销本次修改；自动刷新不会覆盖草稿。
保存使用服务器 revision 防止覆盖其他会话，发生冲突时保留修改，并可载入最新配置。
全局状态轮询每 5 秒进行一次，认证过期回到登录页。
