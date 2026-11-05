# E0 — 可复现验收矩阵与人类授权清单（未执行）

日期2026-11-05；设计cb4b527，36/47。配套[环境方案](E0-LINUX-ENVIRONMENT-PLAN.md)。
本轮只规划，所有下列实测行均为NOT RUN，不因文档检查通过改为PASS。

## 1. 阶段与验收口径，避免循环授权

- **当前P0：环境规划审查。** 无环境创建、ACL、新角色、M11、Watchdog实现或负载许可；本轮到此停止。
- **拟议P1：E0环境准备与小型验证。** 人类明确批准后，才创建专用Linux设备、目录、PG空集群及metadata
  隔离，做设备/身份/内容拒绝验证。仅P1环境子项可通过，不代表E0全部或Task8.3完成。
- **拟议P2：隔离Watchdog/IPC验证。** 需另行批准fixture-only原型、有限进程/消息故障；可证明本地deny协议。
  模拟sink只验证传输，不能证明实际storage unavailable已经Commit。
- **拟议P3：数据库集成验证。** 实际storage_report/helper/受限Worker拒绝依赖正式新能力、角色/ACL与M11；
  必须单独产品授权和实现审查。先验收P1/P2，不要求“先偷偷实现M11才能证明环境可用”。
- **后续P4：8.3真实周期/阈值负载。** 还需独立批准；本稿不许可填盘到80%、1GiB关系膨胀或数小时运行。

总体E0继续未通过，直到人类审查实际证据并明确哪些门禁闭合；P1/P2局部通过不能替代P3。
P3缺失不允许登记“所有Worker已失败关闭”；也不自动推进8.3任务勾选。

## 2. 可复现矩阵

每行固定基线SHA、主机/设备/UID/manifest、工具版本、命令/退出码和原始证据；首次失败停止相应阶段。
PG调用仍单事务<=2s、lock_timeout<=250ms，不加预算、重试到绿、跳过依赖或改断言。
E0只做有限预检，不跑原完整矩阵/一小时冷却/数小时周期；E0时长候选上限20分钟，超时保存失败而非续时。

| ID / 阶段 | 方法与正负控制 | PASS必须具备 | FAIL / 不允许替代 |
| --- | --- | --- | --- |
| P1-01 主机/底层 | 记录Linux/kernel/arch、三个新设备ID、物理或hypervisor容量预留及S/D/E/H statvfs | 产品绑定资产、D精确10737418240 bytes、初始各域<70%、底层余量符合公式 | 仅路径/文件长度/guest df、底层未知或>=80%、当前Mac89.17%被冻结证据被忽略 |
| P1-02 新设备保护 | 对已批准设备用lsblk/blockdev/findmnt/wipefs -n检查身份/签名/holders；负控制用已有签名的无关设备metadata样本，仅检查不写 | 新设备空、无挂载/使用者、大小/序列号一致；不符即拒绝格式化步骤 | 猜/dev/vdb、-F、wipe已有签名、自动yes或选择现有容器卷 |
| P1-03 格式化/挂载 | 仅获批新D按方案mkfs/mount，记录UUID/major:minor/完整参数和T/A/inode | ext4及预期mount options、初始PG UID可用量可信；S/E/D映射分离 | 把格式化成功当完整预检；隐含共享空间/新盘不在指定设备 |
| P1-04 PG实际映射 | 新空集群启动后用可信审计者读GUC、PG进程Uid/Gid/userns、mountinfo/realpath/lstat | PGDATA/WAL/PG文件日志/temp/E各行实际UUID和设备绑定；未知symlink/tablespace不存在 | 只用PGDATA环境、Config.User为空推root、猜UID999、漏Docker日志设备 |
| P1-05 metadata正控制 | Reporter UID20083、cap0/NNP/无附加组，在三个只读bind视图listdir/lstat/statvfs | 仅指定目录元数据成功，无外部父目录遍历 | 报告工具失败为成功、Reporter进入PG组或有DAC override |
| P1-06 内容/秘密负控制 | 同一已存在文件由PG owner先证可读；Reporter O_RDONLY open失败且不读字节；私钥/业务namespace不可达另证 | WAL/日志/temp已有文件EACCES；真实敏感内容未读出；sentinel结构与创建mode一致 | ENOENT当EACCES、只证明不能写、公开假文件、输出业务/WAL/私钥内容 |
| P1-07 生命周期ACL | 有限新建/轮转/rename/unlink/目录重建，分别记录PGDATA0700/文件0600与getfacl | 新文件仍不可读；缺目录ACL返回unavailable，未自动修权 | default/递归r-x、PGDATA权限漂移、只测老文件、放宽0600换通过 |
| P1-08 日志完整性 | 有界受控日志＋open-unlink无敏感sentinel；可信logger数字化fd元数据与目录扫描/设备量对照 | 无重复inode、open-deleted有来源；无来源即不完整 | Reporter广读/proc、把目录和当完整日志量、删除原日志、绕过预算 |
| P1-09 无上游/权限 | 核对无生产路由/凭据、Reporter无Docker/socket/privileged、准备者mount能力不继承 | 原Probe/Worker/Controller/Supervisor/API权力未扩；E0 probe无生产目标执行 | 为预检开放公网探测、Runtime带SYS_ADMIN/root、生产卷共享 |
| P2-01 本地单调失效 | 受审fixture原型停采样/断心跳/坏消息/EOF/旧seq回执，Watchdog记录独立单调deadline；协调器正控制计数 | t0+3s候选失效点后deny ACK<=250ms；无新Claim/Start/Permit/launch；旧异步allow不能重开 | 只有日志“已停止”、杀单Worker当全局拒绝、begin/replay续命、停整个安全进程组 |
| P2-02 本地恢复与安全支路 | 当前incarnation持续deny，低水位消息/重放不能解锁；有限模拟安全消息仍能处理 | latch单向；Pause/PATCH/Result/Supervisor/清理各自路径未被全停；无重发旧执行 | dummy sink结果冒称真实PG/网络路径成功、自动打开gate或恢复旧nonce |
| P3-01 真失效Commit | 另获M11/角色/函数授权后用真实受限登录提交NULL nonce失败信封；与实际状态/受限入口交叉核对 | 真实Commit确认且后续受限SQL拒绝，全部账本无意外变动 | 42883/权限不足冒称成功，管理员UPDATE旧disk数字，模拟JSON回执代替数据库 |
| P3-02 不确定与时钟 | DB断连/ACK未知/新实例、墙钟异常仅在隔离fixture注入，不改宿主时钟；Worker直接SQL负控制 | 本地deny、原样未知Commit；不能证明DB失效则该子项失败保持关闭 | 靠SQL墙钟宣称任意冻结时绝对5s、用旧成功替代、延迟/错误后自动重试网络 |
| P3-03 锁与安全写入 | 原PG18独立连接屏障，control/父序等待后样本失效；维护采用排空、禁止并行DDL的受支持协议 | pg_locks/阻塞PID/时间线；安全路径在2s/250ms内仍可用，无新增反向行锁 | 仅无deadlock/sleep-only；大小函数表锁遗漏；超时后加预算；把维护约束说成125ms修复 |
| P4 阈值与真实负载 | 另审后才运行原8.3 E1–E12的真实1GiB/10GiB/80%与周期写入 | 测量→发布→真实拒绝链及在途恢复余量的完整证据 | E0不填盘，不用小预算/假disk值代替，不把本表设计写成实测PASS |

## 3. “真实设备映射”回执必填字段

每条采样/映射记录包含：case_id、UTC与单调时间、设计/二进制SHA、host/VM/volume ID、namespace ID、
源realpath、容器目标、mount_id、major:minor、UUID/FSType、st_dev、block大小/T/A/inode、PG UID/GID与
Reporter UID/GID/附加组/cap/NNP、实际PG GUC、镜像platform digest、源manifest摘要。
不同mount_id可指向同一设备；F按设备去重，子目录按inode去重。缺字段即UNBOUND/FAIL，不填推测值。
提供方不开放物理容量时，明确记录所接受容量保障的来源/时效/失效行为，需人类确认该信任假设。

## 4. 需要的人类授权清单（当前均未获本轮批准）

| 授权 | 必须明确的范围 | 不能隐含包含 |
| --- | --- | --- |
| A1 资产/预算 | 具体可销毁Linux主机或VM、架构/生命周期、40/10/20GiB卷ID、底层容量信任和费用/资源额度、管理访问方式 | 不创建云资源/付费、不查询或索取聊天秘密、不扩大生产访问 |
| A2 一次性OS准备 | 指定新D格式化/mount、目录与UID20083创建/ACL、只读bind namespace、工具安装及新空PG18启动，P1有限验证范围/20分钟上限 | 不改现存盘、全局fstab、现有PG数据或生产角色；不跑填盘/长负载 |
| A3 原schema/安全夹具 | 若需实际旧安全路径，明确允许在新可销毁testdb应用已审M1–10及其既有角色/GRANT、运行精确短用例 | 不改M1–10，不把“环境创建许可”自动扩为数据库DDL许可，不使用生产库 |
| A4 fixture-only原型 | Watchdog、deny-only IPC、有限故障注入和可信日志metadata来源的具体新代码/进程/权限；只实现经审查范围 | 不扩大Supervisor信任、不提供任意PID/文件操作，dummy证明不等于DB生效 |
| A5 M11正式能力 | 产品对两新表、Reporter身份/两个EXECUTE、私有helper/API owner调用权、七个原门控位置的精确批准 | 原则设计认可≠产品授权；不扩现有运行角色、不跳过独立技术审查 |
| A6 8.3真实负载 | 环境证据认可后，具体冻结SHA、有限磁盘/故障/周期矩阵、真实时间/空间预算与首次失败停止规则 | 不借P1/P2同意填盘，不反复CI重跑到绿，不部署/公网执行 |
| A7 结束/回收 | 指定新资源的保存期限、证据导出/哈希、独立可信停止/未确认资源处理和仅新资源的卸载/删除 | 不rm既有卷/旧失败证据，不以删VM/容器当已可信返槽 |

A2/3/4可以分次批准，不能将全部选项默认为一次授权。若产品只批准A1/A2，完成环境子项后仍停在P1，
不自行进入M11。若需要数据库身份级deny-only Watchdog，必须另列新增身份/入口申请；目前同主体两EXECUTE
不提供这种ACL隔离，不悄悄增加第三项能力。

## 5. 停止与恢复规则

预检依赖缺失、真实映射不符、任一容量域未知/到80%、ACL内容可读或来源不完整，停止放行；保存原失败。
安全控制和物理证明继续用原独立Supervisor，不返还未经证明的槽；所有原子账/冷却/隔离保留。
不得因失败延长2s事务/250ms锁预算，不删除quarantine、重发GET、自动repair账或降低80%门槛。
恢复需人类批准的控制代次、真实低水位与可信新样本，不把暂时通过一次测量当长期磁盘保证。

原E0失败包、125ms锁等待、IPv6及管理确认超时未知根因均保留；目录ACL与表级锁关注项也未被本稿“解决”。
本轮未执行P1–P4、未连接或创建目标环境；E0仍未通过，8.3仍未实施，至此提交审查后停止。
