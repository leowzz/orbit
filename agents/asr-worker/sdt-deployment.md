# SDT 部署与验收

2026-10-23，目标 SSH `sdt`，Windows 用户 `leo`。运行目录 `C:\work\orbit-asr-worker`；`C:\Users\leo\work` 是到 `C:\work` 的目录联接。

## 环境

- Windows 11 Pro 10.0.26100，AMD Ryzen 7 9700X（8 核 16 线程），31.1 GiB 内存。
- NVIDIA RTX 4060 Ti 8 GB，驱动 616.92。
- uv 管理的 Python 3.11.15，独立 `.venv`；不改系统 Miniconda 环境。
- PyTorch 2.11.0+cu128，CUDA runtime 12.8；Qwen-ASR 0.0.6，Transformers 4.57.6，PyAV 18.1.0，PySide6-Essentials 6.12.0。
- 本地模型 `models\Qwen3-ASR-0.6B`，来自 Qwen 官方 Hugging Face 仓库。默认 `device=cuda`，GUI 可停止后切换 CPU。
- MQTT `mqtt.wleo.cn:8883`，启用 TLS 与证书校验；专用 worker 账号 `orbit-sdt-asr`，参与者 `sdt-asr`。密码保存在该用户 Windows 凭据库，配置和计划任务不保存明文密码。

## 日常使用

桌面快捷方式为 **Orbit ASR**，运行中从系统托盘打开窗口。计划任务 **Orbit ASR Worker** 使用当前用户的交互桌面，在用户登录后执行 `pythonw.exe -m orbit_asr.gui --start --minimized`。本次已直接运行该任务并验证它在线处理音频；没有通过注销或重启机器触发登录验收。

在界面中点击「停止」后可以修改 MQTT、模型和 CPU/GPU，再点击「启动」。关闭窗口收起到托盘；托盘「退出」会停止子进程并退出。持久化队列在 `data\asr.sqlite3`，配置在 `config.json`，真机测试结果与截图在 `verification\`。

取消登录自动启动可在 PowerShell 执行：

```powershell
Unregister-ScheduledTask -TaskName 'Orbit ASR Worker' -Confirm:$false
```

## 真机验收

使用 Windows System.Speech 生成不含私人内容的英文、中文语音，编码为 16 kHz 单声道、20 ms 裸 Opus 帧，经真实 MQTT 5 和线上 Core 投递给 GUI 启动的 worker，再由真实 Qwen 模型转写。没有 mock 模型。

| 设备 | 音频 | worker 转写耗时 | Core segment ID |
| --- | --- | --- | --- |
| CUDA | 英文 7.44 秒 | 1,297 ms | `cc11d12c-c859-4f96-b898-cff166486052` |
| CPU | 中文 8.32 秒 | 2,155 ms | `ba777c07-ac40-4bec-b165-41c1c59a8918` |
| CUDA（正式登录任务） | 中文 8.32 秒 | 1,250 ms | `4f698639-1d5e-44ac-8c15-d50e7b7a8dd2` |

英文：Hello Orbit. This is a speech recognition test. The meeting starts at nine tomorrow morning.

中文：你好，这是语音识别测试。明天上午九点开会，请记得带上笔记本。

临时验收节点账号 `orbit-sdt-audio-test` 及其 MQTT 授权已撤销，三条验收记录保留。

三条任务均经 Core API 与 SQLite 交叉确认 `completed`，包含文本、语言、模型、计算设备、耗时及覆盖音频范围的 segment。worker 收到 Core 应用确认后 outbox 清零。GPU 首条任务曾在部署等待期间重发，worker 实际推理一次。

独立模型测试：GPU 加载 5.12 秒、推理 1.067 秒、峰值 PyTorch CUDA 已分配内存约 1.75 GiB；CPU 使用 4 线程加载 4.968 秒、推理 2.379 秒。表中耗时来自实际队列 worker，包含解码等处理，均为单次测试值。

GUI 在已登录的 Windows console session 1 中运行。已验证设置加载、系统凭据读写、启动/停止、CPU/GPU 切换、状态与日志显示；关闭窗口后 native 托盘图标仍可见，英文任务在窗口隐藏期间完成；重新打开窗口后显示最新状态。退出窗口及其 worker 子进程已实际完成。没有重启整台机器。

Windows 自动化测试：**87 passed, 1 skipped**；跳过项是需显式启用的本地独立 broker 集成测试，线上真实模型链路已另行通过。GUI 原生入口连续 10 次启动/退出均正常，并断言 Qt 对象和配置锁被释放。

本次修复 Windows 实际问题：QLockFile 必须显式传入超时；等待 stdin 的阻塞读取会让 NumPy 导入停住，改为 PeekNamedPipe 检查后只读取已有字节；退出时 Qt closeEvent 必须接受最终关闭；显式在解释器退出前销毁窗口并清理 QApplication，消除偶发的退出崩溃。超时停止另增加 Windows 进程树清理，覆盖 venv launcher 与真实 Python 后代进程。

验收音频由合成语音生成，因此不代表 Android 麦克风、系统后台限制或手机网络环境已真机验收。当前时间段是整段音频范围，未启用词级对齐。

## Core 升级

管理页：[音频数据](https://orbit-core.wleo.cn/audio)。Core 音频页面部署内容与本地构建一致，认证和音频查询接口正常，五张音频表存在且 SQLite integrity_check 为 ok。

- 当前镜像：`orbit-core:audio-20261023T142540Z`。
- 一致性备份：Core 主机 `/srv/orbit-deploy/backups/audio-20261023T142540Z`，含 Core/inbox SQLite、配置与 compose。
- 回滚镜像：`orbit-core:mqtt-admin-20261023T035244Z`，保留在主机。
- 仅升级 `/srv/orbit-deploy/docker-compose.yml` 的 `orbit-core` 服务；保留原有路由、卷和端口配置。

回滚时将 compose 中 Core 镜像改为上述旧版本，再在 Core 主机运行：

```sh
docker compose -f /srv/orbit-deploy/docker-compose.yml up -d --pull never --no-deps orbit-core
```

普通镜像回滚保留现有数据库与 MQTT 账户，避免覆盖升级后数据；恢复备份数据库前应停止服务并另外备份当前数据。
