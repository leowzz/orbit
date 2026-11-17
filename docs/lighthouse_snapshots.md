# 腾讯云轻量应用服务器自动快照方案 (Wiki)

## 背景
腾讯云轻量应用服务器 (Lighthouse) 原生控制台不支持定期自动快照策略。
免费快照名额有限（通常每个地域实例数 × 2，最大 20 个）。
为防止达到配额限制且保留重要备份，需实现“定时自动备份 + 滚动删除最旧快照”的自动化流程。

## 方案架构
1.  **最小权限管理**：创建一个专门的 CAM 子用户 `lighthouse-snapshot-bot`，通过策略 `LighthouseSnapshotAutoRotatePolicy` 仅赋予 `lighthouse:DescribeInstances/DescribeSnapshots/CreateInstanceSnapshot/DeleteSnapshots` 权限。
2.  **自动化脚本**：使用 Python SDK (`tencentcloud-sdk-python-lighthouse`) 编写滚动脚本，逻辑为：
    -   查询实例现有快照。
    -   若快照数达到 quota 上限，删除最旧的一份。
    -   创建一份 `auto-YYYYMMDD-HHMMSS` 的新快照。
3.  **定时调度**：部署在目标服务器 `tx` (`62.234.71.244`) 上，通过 `crontab` 每 3 天执行一次。

## 配置详情 (主机 `tx`)

| 项目 | 内容 |
| :--- | :--- |
| **工作目录** | `/opt/lighthouse-snapshot/` |
| **虚拟环境** | `/opt/lighthouse-snapshot/venv/` |
| **环境配置** | `/opt/lighthouse-snapshot/env` (权限 600) |
| **执行脚本** | `/opt/lighthouse-snapshot/run.sh` (封装了 `rotate_snapshot.py`) |
| **日志路径** | `/var/log/lighthouse-snapshot.log` |
| **调度频率** | `30 3 */3 * *` (每 3 天凌晨 3:30) |

## 操作指南

### 1. 查看自动快照日志
```bash
ssh tx 'cat /var/log/lighthouse-snapshot.log'
```

### 2. 手动演练/检查 (Dry Run)
检查脚本运行是否正常，不进行删除和创建操作：
```bash
ssh tx '/opt/lighthouse-snapshot/run.sh --dry-run'
```

### 3. 立刻触发滚动
```bash
ssh tx '/opt/lighthouse-snapshot/run.sh'
```

## 注意事项
- **非 NORMAL 状态保护**：若脚本运行时有快照处于 `CREATING` 或其他非 `NORMAL` 状态，会自动跳过本次执行，防止误删或冲突。
- **手动快照兼容性**：脚本仅自动处理符合数量限制的逻辑。如果除了自动快照，你还手动创建了永久快照（不带 `auto-` 前缀），这些快照会占用配额，可能导致自动脚本因为名额不够而无法创建新快照。如需保留永久备份，建议手动删除旧的自动快照腾出空间。
