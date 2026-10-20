# Verification — add-monitor-scheduler-worker

## Authorization and baseline

2026-10-20 用户正式授权两轮独立评审后的方案A全部47项Apply任务，确认全部产品决策；允许代码、独立Linux/PG18 fixture测试、证据和验收后的实现PR。生产真实互联网执行、生产Migration/部署、合并和Archive未授权。无需重复确认已批准的暂停/24h授权/弃用常量/全体g+1等决定。

实施开始于main `1d3b8266e494061beccc3ccdf47694ba4b0339c5`；HEAD/main/origin/main及实时远端main一致，工作区只有已批准规划目录未跟踪。PR10实时核实MERGED，merge `85402ab7877e67062e2d48b24e9ad9c31b721461`；十个归档与17份主规格保持。新分支 `codex/add-monitor-scheduler-worker`。完整planning/Migration1–7 SHA256、归档名单及工具版本见 `evidence/baseline.json`；基线提交就是证据所对应的已审SHA，不冒称后续代码已验收。

## Current checkpoint

当前为 **Checkpoint1：Schema/Authorization，仍在实施**。已创建Migration8、受限角色/函数、有限授权与status API、旧Activation固定弃用投影及隔离工具预检。还没有可执行Scheduler/Probe闭环，`cmd/schedulerfixture`目前失败关闭；不能将此检查点描述为方案A完成。

独立可销毁PG容器 `devpilot-scheduler-apply-pg18` 为PG18.6；不复用项目已有容器。Docker internal bridge禁止上游且本机端口发布不可用，因此在其控制网络中运行CGO=0 Linux测试二进制；每个破坏性套件先通过现有`internal/testdb.Open`再调用Goose3.26.0。工具/镜像/二进制哈希见 `evidence/toolchain.json` 及各命令receipt。

`ip netns add`在NET_ADMIN-only容器因mount权限不足失败。已实测可信外部Docker harness并发创建两个不同的Run namespace：无上游接口/路由、IPv6可用、双栈默认拒绝、nft内核限期许可到期能阻断既有TCP流后续数据，UID65534/caps0/no_new_privs不能改规则或路由。缺工具、NET_ADMIN、IPv6三个负向对照均非零失败，见 `evidence/preflight/manifest.json`。这些仅是工具预检，不是Supervisor完整终止回执、分布式故障或生产出口证明。

### 已执行的初步检查

| 测试/命令记录 | 实际结论 | 限制 |
| --- | --- | --- |
| `evidence/checkpoint1/pg-foundation.json`及.log | exit0；PG18 mixed enabled/disabled退役、up/up、可销毁down/re-up、两类g溢出/Trigger/后段DDL故障回滚；真实LOGIN拒绝秘密读取/直接DML/DDL/TEMP/owner role和授予函数；受限API LOGIN授权/status有效 | 当前未提交实现树，receipt记录准确HEAD/dirty/源码和二进制hash，不作为最终SHA通过 |
| `evidence/checkpoint1/unit-boundary.json`及.log | exit0；monitor/monitoring单元、HTTP/OpenAPI、Linux普通发布依赖/显式fixture构建标签检查 | 不是完整Go/race/PG/网络验收 |
| `evidence/checkpoint1/pg-spi-expiry.json`及.log | exit0；真实PG18十级SPI锁等待跨Session到期、Commit回滚/成功丢ACK对照、跨W mixed迁移及冻结schema7源hash | 尚不是Claim/Start锁序或网络证明 |
| `evidence/preflight/` | 两个不同并发netns正向通过；缺工具/IPv6/NET_ADMIN负向非零 | 尚无每Run完整Supervisor生命周期/跨容器周期 |

schema7历史fixture来自已审核基线`internal/monitor/activation_store.go`，仅重命名receiver/符号并置于integration构建；逆变换SHA256为`4fe9c60fcdb11901ba51b20af25e2a3682a0c54d4f00b87b883181ea79b7d591`。历史schema7测试固定up-to7，并执行真实旧值SQL；schema8生产Store固定新值，未修改前十阶段归档。故意失败的“下一迁移”已移至9且包含真实1–8，不以旧next8跳过M8。

### 已发现并修复中的历史回归适配

检查点提交`b2fdbbdd08b7400b550809794b915e727c1a6c3f`的首次全量Race和PG回归为exit1，不能记作通过：认证路由合同未登记两个已批准API；旧overflow fixture故意留下bigint最大g，导致M8 Down全体tag退役安全失败，后续清理级联失败。原始命令/源码与二进制hash/日志保留于`evidence/checkpoint1/failures/`。修复仅补精确路由合同及测试fixture清理；不削弱Down/g溢出保护、不改Migration1–7。污染的可销毁测试容器在核对容器ID、数据库名及PG18后重建；其他应用容器未触及。修复后重新执行，不隐去首次失败。

## Task evidence policy

tasks仅在对应验收通过后勾选。每项登记Requirement/Scenario、实现文件、测试名/完整命令/退出码、证据路径与提交SHA；初步检查receipt显式保留dirty，不冒称最终提交。1.1/1.2的授权及基线见`evidence/baseline.json`；1.4工具预检见上述实际正负控制。其余任务正在实施或待验收，未按代码存在就勾选。

## Acceptance status

- 当前仅基线/授权和隔离工具预检完成；其余任务未获完整验收。
- Migration8仅执行于新建guarded PG18；Migration1–7保持原字节，未对生产执行。
- 真实3周期、两Worker锁图、完整Supervisor物理终止、unknown/预算/容量/全部故障与原76项新SHA复验：未完成。
- 全量normal/race、最终同SHA Push/PR CI与交付PR：未完成；尚未创建PR。
- 生产网络/部署：未验证，保持硬关闭。
