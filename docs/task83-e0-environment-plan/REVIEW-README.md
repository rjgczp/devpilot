# Task8.3 E0 环境阻塞处理 — 审查材料

日期：2026-11-05。当前36/47，8.3未实施、未勾选，E0继续未通过。
正式设计基线为`cb4b527af7e75c1ff85c3a49f7a51ee427ae55f0`。
本次按人类《Task 8.3 E0 环境方案交付》授权，仅将本目录三份既有规划文档提交/推送到
`codex/task83-storage-design`；提交SHA以Git记录为准，不是8.3实施或运行受测SHA。
PR12保持Open/0462dc2；main仍5905f518。未执行环境配置、权限修改、Migration11或负载。
方案/矩阵中的“本轮”“仓库保持cb4b527”描述原规划完成时点；本次仅将该快照入库，不将原只读记录改写为新实测。

## 审查顺序

1. [Linux环境方案](E0-LINUX-ENVIRONMENT-PLAN.md)：原生Runner/专用VM取舍、40/10/20GiB容量方案、
   独立10GiB块设备创建步骤、S/D/E/H真实映射契约、Reporter目录metadata ACL、单调Watchdog与维护边界。
2. [验收矩阵与人类授权](E0-ACCEPTANCE-AND-AUTHORIZATION.md)：P1环境、P2本地协议、P3实际DB、P4负载
   分阶段，A1–A7逐项批准。全部实测仍NOT RUN，环境子项不能冒充完整storage拒绝链。
3. 原E0失败包：本机原证据目录`e0-readonly-20261104T100055Z`保持原样；此前完整审查包ZIP中的
   `prior-e0/`保留其副本，含只读报告、OS ACL清单、读取失败、metadata及哈希。本次仅提交三份文档，
   不复制原始证据到Git。原125ms/IPv6/管理确认超时风险继续保留。

## 必须由后续人类决定的绑定

- 原生Linux目标或VM/提供方、生命周期/资源预算、管理方式及底层容量信任；当前没有目标资产。
- 40/10/20GiB卷的具体ID/UUID及宿主容量保障；不接受当前Mac的共享空间作为已通过依据。
- 实际PG UID/GID/userns与候选Reporter UID20083的无碰撞绑定；不凭Config.User猜测。
- 是否仅批准P1准备，还是另批准精确的fixture-only Watchdog/日志metadata原型；M11仍单独审批。

本包没有安装脚本、SQL/迁移、GRANT、ACL实际变更或磁盘/负载结果。
参考命令都是审查示意，不得直接运行到未绑定设备。资料中的真实设备字段UNBOUND不是已实现映射。
方案提交不代表任何资源/费用/权限已经批准；完成材料后停止。

## 当前证据适用范围

以下文件位于此前交付的独立证据包，不包含在本次三份文档提交中：

- `readonly-planning-receipts.json`：原规划阶段Git/OpenSpec只读事实与本地工具PATH清单，不是目标Runner能力证明。
- `verification.json`：原规划阶段仓库/原E0字节不变及本地链接检查；未执行E0实测或完整测试。
- `SHA256SUMS`及ZIP的`.sha256`：原材料与附带副本的校验索引。

原完整审查包为`task83-e0-environment-plan-20261105.zip`，SHA256为
`e9a645cb9d7fbd559facf8fbd86c8705d255655666ec6868f1f191c89266fda2`。
本次入库不覆盖或重写该包及原失败证据。方案和验收矩阵原样复制；README仅适配入库说明与证据引用。
