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
Ctrl-C 停止开发进程；Go 代码修改仍需重启。`7620` 是后端及嵌入页面入口，
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

## 构建

```sh
pnpm check
pnpm build
```

产物为 `dist/`，根目录 `make build-core` 自动先执行这里的构建，再生成包含页面、
脚本和样式的 Core 二进制。`embed.go` 仅提供 Go 构建桥接，前端无需导入它。
`dist/.gitkeep` 保留目录用于尚未构建时的 Go 包加载，其他产物不提交。

## 页面与数据

- `/`：总览与实际规则数据流向。
- `/agents`：主机、Source 健康、数据新鲜度。
- `/nodes`：设备分类、身份与关联规则。
- `/routes`：规则列表、侧边编辑器、版本冲突保护。
- `/system`：Core 身份、存储、会话和观测策略。

API 类型集中在 `src/api.ts`，页面在 `src/pages/`，公共组件在 `src/components.tsx`。
页面刷新不会覆盖编辑器草稿；修改规则用服务器 revision 防止其他会话的覆盖。
全局状态轮询每 5 秒进行一次，认证过期回到登录页。
