# Checkpoint1 最终独立审查包

Checkpoint1的14项任务已通过对应验收，整体 **14/47**。本轮新增完成1.3、2.3、2.4、2.5、3.3、3.4、3.5；第4–10组33项仍全部未勾选，没有自动进入Checkpoint2。生产执行硬关闭，不创建PR、不合并或部署。

实际冻结实现SHA：`d781d7560904ee81c4939d9520604ff2240766a4`。全部`final-*`运行回执记录这个SHA、clean工作区、相同源码清单hash `1cae1483aefa7fce5ad5983aa25fa42b6fab6bf84e8b45773d78fa2db66605fb`。最后的提交只登记验收文档/任务/证据，不把后续文档HEAD冒称运行时SHA。实现对应关系见`implementation-binding.json`。

## 实现、实测和设计的边界

| 层次 | 本次结论 |
| --- | --- |
| 已实现 | Migration8对象/Trigger/受限函数、有限期Owner授权API、Activation弃用提示与status基础代码；这些业务实现和Migration8相对首轮审查HEAD未改。本轮收紧已有Probe测试为Linux-only，其余为测试、夹具、协议参考和文档 |
| 已实测 | 全部131项PG18 normal及同样131项integration-race；含迁移生命周期/异常回滚/跨版本旧写者、权限、Owner最终门控、真实60秒期限、HTTP及零目标边界。Go unit/race、vet、Linux fixture单元/race、工具正负预检及Change strict均通过 |
| 仅协议参考 | 测试中的客户端消费逻辑和guarded-PG18维护SQL；不声称外部客户端、HTTP实例清单或生产回退已经部署。运行/unknown/completed投影的管理夹具不是真实GET或Result Commit。预检不是完整Supervisor |
| 仍未交付/验收 | 完整周期Controller/Worker/Probe桥、target-permit、独立Supervisor物理终止、Result/unknown恢复、完整目标预算及保留机制、三个真实周期、完整故障矩阵、当前SHA的原76项网络复验及最终CI。现有Claim/Start初步代码未扩展，相关回归通过不完成后续任务 |

## 冻结提交的实际结果

| 回执（均有同名原始.log） | 退出码 / 结果 |
| --- | --- |
| `final-build-normal.json` / `final-build-race.json` | 0 / 固定Go1.24.3 Linux normal、audit及CGO/race二进制 |
| `final-unit.json` / `final-unit-race.json` | 0 / 全量Go unit、全量Race Detector |
| `final-static.json` | 0 / gofmt、普通及integration vet、Change strict、diff check |
| `final-pg-normal-{0,1}.json` | 0、0 / 130项64.90s，加真实60秒到期60.47s |
| `final-pg-race-{0,1}.json` | 0、0 / 130项105.09s，加真实60秒到期61.42s |
| `final-fixture-unit-race.json` | 0 / 无网络Linux fixture包normal/race |
| `final-preflight.json`、`preflight/` | 0 / 两个不同无上游namespace及缺依赖/权限/IPv6负向控制 |
| `final-audit-classifiers.json` | 0 / 原精确白名单、CGO来源及NSCD/未知目的反例 |
| `final-zero-target-audit.json`、`network/` | 0 / 真实配置/Activation/授权/status工作负载child0/audit0，25个批准的DB/本机HTTP尝试、0个未批准尝试；故意越界校准child0/audit1 |
| `final-all-strict.json` | **1** / 四份历史规格七项长度警告，与先前记录逐字节一致，无新增警告；不修改无关历史规格 |

`final-pg-*-coverage.json`记录两个完整串行分区；每种构建的131个测试恰好覆盖一次，清单一致，无skip。单独分出新增真实60秒测试以保留原120秒进程预算，不是延长超时或减少测试；事务2秒/锁250ms及所有安全阈值保持。包括全部既有前十阶段集成回归，不能把原76项网络旧证据算作本SHA通过。

## 七项剩余任务的对应验收

| 任务 | 核心实现/测试 |
| --- | --- |
| 1.3 | 普通Linux API实际二进制和依赖图不含执行包；显式测试构建Probe、fixture-only命令边界；带执行样式环境开关的真实API审计仍零目标 |
| 2.3 | `TestCheckpointOneLegacyAndAuthorizationSerialize`新授权/旧意图双顺序；`TestCheckpointOneOldWritersStraddleMigration`三种迁移交错；旧角色PATCH及授权/撤销Trigger失败整体回滚 |
| 2.4 | `TestSchedulerRuntimeRolesHaveOnlyNarrowCapabilities`、`TestCheckpointOneControllerPublicAndIncarnation`、Supervisor单输入权限及旧角色新能力拒绝；真实LOGIN、固定search_path、无PUBLIC执行/动态SQL/秘密读取/owner角色借用 |
| 2.5 | `TestSchedulerMigrationRetiresAllLegacyTags`、`TestSchedulerMigrationRollback`、`TestCheckpointOneDownRefusesUnsafeOrExhausted`、Schema7安全失败、HTTP全体旧tag失效与历史源码hash |
| 3.3 | `TestCheckpointOneHTTPIntentStatusContract`真实HTTP/PG矩阵、14字段/固定提示/intent-v2/原ETag及权限；OpenAPI/历史Schema7契约和API独立零目标审计 |
| 3.4 | `TestCheckpointOneStartOwnerLossBothOrders`、`TestCheckpointOneStartExpiryAfterFinalWait`、`TestCheckpointOneSessionIsNotBackgroundIdentity`及真实60秒/显式续期；最终P0001拒绝不由锁超时冒充 |
| 3.5 | `TestCheckpointOneHTTPCutoverAllTagsAndRollback`、`TestCheckpointOneProjectionRetirementKeepsSchemaEight`、HTTP实例清单门控及可执行客户端契约：状态不可用、v/g失配刷新、后台更新不改Activation表示 |

完整 Requirement → Scenario → 实现 → Test → 命令/退出码/日志/SHA 映射见上级`task-index.json`。逐任务勾选和CLI进度核验见`task-completion-record.json`。`review-code.diff`是相对首轮独立审查HEAD `bd729b232e6eb206e44ca0f7cb63e31d95f55d30`的代码/测试/运行文档差异。

## 失败记录与修复

所有失败保留，不用后一次通过覆盖：

- `development-gaps*`：新测试夹具的Session时间约束、HTTP实例必填身份和同名账号错误；只改夹具。旧Handler仅去除新增协议头后，SHA256与已审Schema7源码一致；Store历史hash仍验证。
- `frozen-unit.json`：宿主沙箱拒绝loopback bind，进程级测试未具备运行条件；同源码允许测试本机监听后通过。不是PG18/Supervisor权限矛盾。
- `frozen-pg-normal-0.json`：首次完整串行验收发现原授权测试未清理。`isolation-root-cause.{json,log}`独立证明其结束后仍是schema8及1条Schedule。仅改用既有workspaceDB清理，新增组合隔离回归；当前131项normal/race已通过。容量上限与最后槽竞争断言未改。
- 首次Supervisor工具审核超时的内部原因仍未知；它发生在进程启动前，不是PG或物理Supervisor超时。本次正常/race的数据库权限通过不消除该历史风险，也不证明完整物理终止。

## 原始证据与完整性

`.json`回执保留命令、实际HEAD/dirty、源码和二进制hash、时间、退出码及日志hash。为避免重复存同一大清单，`sources`以`source_inventory_ref`指向共享文件；**未加工原始回执字节**另存在`raw-receipts/*.json.gz`，解压SHA256见`original_receipt_sha256`。按`json.dumps(sources,sort_keys=True)`重新计算源码清单hash即可核验；压缩不是重写运行状态。

`network/*.trace`是独立strace原始尝试记录；`postgres-raw.log.gz`为完整原始PG日志，含已知故障/权限拒绝，解压hash和采集命令在`postgres-log-provenance.json`。日志只来自专用测试库，不能作为生产证据。`manifest.json`列出全部文件hash，不提交测试可执行文件或镜像。

`final-postflight.json`确认PG18.6、Migration0，无Schedule、临时角色、活动事务或锁等待。`final-environment.json`记录固定镜像/原容器和内部网络；Migration1–7 SHA256仍与已审main相同。实际main仍`1d3b8266e494061beccc3ccdf47694ba4b0339c5`。

历史部署LOGIN/ACL没有真实清单，不能从测试角色推导或扩大生产GRANT；生产迁移/部署继续禁止。本次完成后只推送开发分支供第二轮独立审核，不创建正式PR、不合并、不Archive、不开放生产出口。
