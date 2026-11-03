# Orbit Core Console

独立的 React + TypeScript 前端，基于 [shadcn-admin](https://github.com/satnaing/shadcn-admin) 的侧栏、UI 组件和主题，保留 Orbit 的业务页面和 React Router 路由。模板版本和许可证见 [THIRD_PARTY.md](THIRD_PARTY.md)。

## 开发

要求 Node 22.12+ 与 pnpm 11.9.0（CI / Docker 使用 Node 24）。

```sh
# 仓库根目录：同时启动 Core API 与 Vite
make dev-core
```

访问 `http://127.0.0.1:5173`。也可以分两个终端运行 `make dev-core-api` 和 `make dev-console`。前端热更新，Go 修改后需重启；Ctrl-C 停止组合启动的两个服务。

Vite 固定使用 5173，通过 `/api/` 代理到 `http://127.0.0.1:7620`。自定义后端地址：

```sh
ORBIT_CORE_TARGET=http://127.0.0.1:8620 pnpm dev
```

Core 的 7620 端口只提供 API。登录使用 Core YAML 的 `console.password` 与 HttpOnly Cookie；前端不保存密码或 Token。`console.session_hours` 默认 72 小时，修改后重启 Core 对新会话生效，已有持久化会话保持原有效期。

## 构建与部署

```sh
pnpm install --frozen-lockfile
pnpm build
# 本地查看生产构建，仍通过 /api 代理 Core
pnpm preview
# 本目录下构建独立前端镜像
docker build -t orbit-console .
```

产物为 `dist/`，包含预压缩资源。Go 构建、测试和 Core 镜像均不依赖前端产物。

前端镜像在 8080 端口提供页面，运行时通过 `ORBIT_CORE_UPSTREAM` 指定后端，格式为 `host:port`，默认 `orbit-core:7620`。Nginx 同源代理全部 `/api/`，保留 Host、Cookie、协议和 SSE，维持 Core 现有会话与 Origin 校验。上游 HTTPS 入口应传递原始 Host 和 `X-Forwarded-Proto`。

仓库的 `deploy/docker-compose.yml` 同时提供 `orbit-core` 和 `orbit-console`；浏览器访问主机 `127.0.0.1:7621`。Core YAML 需设置 `console.listen: 0.0.0.0:7620`，供容器网络内的前端访问。原生设备继续使用原有 App API 端口。

## 页面与组件

- `/`：总览与数据流向。
- `/box`：共享文本、待办和图片；旧 `/messages`、`/apps`、`/inbox/` 跳转到这里。
- `/agents`、`/nodes`、`/nodes/:id`：电脑来源、设备及内容、快捷操作和接入配置。
- `/mqtt`、`/audio`、`/reviews`、`/system`：MQTT 账号、音频记录、每日回顾与系统信息。

业务页面按路由懒加载。收件箱保留本地草稿、离线队列与 SSE 同步；配置保留自动保存和 revision 冲突处理。API 类型集中在 `src/api.ts`；模板基础组件在 `src/components/ui/`，布局在 `src/components/layout/`，主题在 `src/theme.css`，现有业务样式通过 `src/domain.css` 适配主题。

侧栏支持折叠、移动端抽屉与 `⌘/Ctrl+B`；深浅色偏好沿用原有保存位置。

## 验证

```sh
pnpm check
pnpm format:check
pnpm test
# 要求 Docker 与 Chromium；首次运行先安装浏览器
pnpm exec playwright install chromium
pnpm test:browser
```

浏览器检查要求本机 Docker daemon；可用 `DOCKER_CONTEXT=colima` 指定本机上下文，已有 Chrome 可通过 `ORBIT_BROWSER_CHANNEL=chrome` 使用。检查使用实际 Nginx 配置、独立的临时 Core API 和数据库，端口为 17622、17623，不读取部署配置。覆盖登录、深链接、收件箱编辑、附件、离线重试、退出、移动端导航、资源缓存与跨站请求拒绝。截图、失败 trace 与加载数据位于忽略提交的 `test-results/`。
