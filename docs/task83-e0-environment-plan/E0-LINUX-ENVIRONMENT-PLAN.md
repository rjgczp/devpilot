# Task8.3 E0 — Linux环境方案收敛（审查稿，未执行）

日期：2026-11-05（Asia/Shanghai）。设计基线：`cb4b527af7e75c1ff85c3a49f7a51ee427ae55f0`。
用户已决定维持E0未通过，不批准Migration11或真实磁盘负载。本文件仅为环境规划和审查材料。
36/47、8.3未勾选；PR12保持Open/0462dc2，生产网络硬关闭。未修改正式设计分支或原E0材料。

## 1. 收敛选择与仍需绑定的资产

**首选：可销毁、单项目独占的原生Linux Runner，配新建的独立10GiB数据设备。**
优先能直接证明设备容量和底层预留的机器；不是默认在当前Mac上再开一层VM。
若可提供相同证据，专用远端Linux测试VM为备选。尚无目标主机、访问授权或容量凭据，
本轮没有连接远端、查询SSH秘密、创建云资源或估算未核实的费用。

| 选项 | 接受条件 | 本轮判断 |
| --- | --- | --- |
| 独占原生Linux Runner | 新数据盘与系统/证据盘分离，物理/块容量与底层余量可核验；允许受审namespace/ACL工具 | 首选候选，资产未绑定，不能宣称已可用 |
| 专用Linux测试VM | 独立guest内核；40/10/20GiB三个虚拟设备；hypervisor厚置备/容量预留证据和维护边界可见 | 备选；仅guest显示10GiB不足以证明底层保障 |
| 云VM＋容量保障块卷 | 提供实际卷ID/容量/服务承诺，产品明确接受提供方作为容量信任来源 | 条件候选，不把云卷逻辑容量称为物理独占；未知底层不能默认为healthy |
| 当前Mac/Docker Desktop/本机VM | 需要先解除宿主容量阻塞并独立审查底层 | 当前排除；已保留89.17%原始观测，不以guest低使用率换分母 |
| 普通共享托管Runner | 必须证明独立设备和底层空间，不只分配目录或临时volume | 不作为默认替代；工具可用不代表存储隔离成立 |

本轮PATH只读查到utmctl，未查到limactl/multipass/virsh/qemu/prlctl/VBoxManage/colima。
这不是VM资产清单；未调用任何VM启动/创建/快照命令，也不推断已有VM或远程Runner。
上次89.17%是2026-11-04的冻结证据，本轮未重新做磁盘测量或声称它是11月5日当前读数。

候选资源下限：Linux原生内核、4vCPU/8GiB RAM、双栈namespace/受控Supervisor能力；
既有PG容器512MiB/CPU1/PID128等限制不因换环境自动扩大。架构、内核、PG18镜像、Go1.24.3及
Goose3.26.0须在目标确定后冻结。当前arm64 digest不能冒充未来amd64 child digest的验证结果。

## 2. 容量、隔离与停止边界

候选三个设备：系统/工具盘S=40GiB，测试数据盘D=10GiB，证据盘E=20GiB。大小是待批准资源规格，
不是已创建资源。D的原始块容量必须为10737418240 bytes；格式化后的T/A以实测为准。

- 数据、WAL、PG日志、PG temp都在D；PG执行文件/镜像/工具在S；原始证据在E。
- VM/file-backed底层H需为三盘完整占用预留70GiB。分配前要求
  `A_before >= 70GiB + max(20GiB, 0.30*T_H)`，并在完整预留后复核H使用率低于70%。
  此为候选保守容量条件，不把fallocate成功、文件长度或thin-provision虚拟容量当成真实物理余量。
- 原生独立块卷也须给出各容量域/共享存储池保障；不能从不同UUID推断不同物理故障域。
  若不可见提供方底层，须明确产品接受的容量保障来源；没有该决定则E0仍不通过。
- S/D/E及必需H分别测块/inode；同一设备按UUID/major:minor去重，不把三个目录容量相加。
  初始各域须低于70%；任一已达80%或数据未知，停止环境放行。恢复需重新取得可信低水位证据。
- E候选证据写入预算8GiB，额外保留至少2GiB用于失败回执/停止记录；这是待批准的写入边界，
  不是已实现硬配额。超限必须停止并保留当前原件，不截断原断言/删首次失败/覆盖旧包。
- 即使停止新Claim，PG/Result/返槽/vacuum/日志仍可能写盘。不得承诺ENOSPC后安全写入必然成功；
  20%余量与最多32个在途的恢复开销仍留后续真实负载验收，E0不冒称已证明。

使用冻结系统/镜像离线准备工具后再进入隔离阶段；网络只留授权管理入口和现有无上游fixture控制网。
不继承生产盘/凭据，不连生产DB，不开放真实公网目标。下载/包安装与隔离测试必须分阶段留据。

## 3. 独立10GiB文件系统的具体创建流程（仅审查示意）

首版直接使用**产品明确绑定的新块设备**，不默认用loop文件、稀疏qcow2或共享Docker volume。
所有写入步骤须取得独立授权；本轮没有执行以下任何步骤，也没有生成可自动运行的安装脚本。

1. 人类绑定主机ID、架构、新卷ID、序列号、`/dev/disk/by-id/<approved-new-e0-volume>`及10GiB容量，
   明确它可被销毁、无业务数据、不是根盘/父盘分区/已有LVM/RAID成员。不猜/dev/vdb。
2. 先只读记录lsblk的NAME/TYPE/SIZE/FSTYPE/UUID/MOUNTPOINTS、blockdev --getsize64、
   findmnt、wipefs -n及容量预留凭据；任一签名、holder、挂载、身份或大小不符立即停止，不能wipe清理继续。
3. 在仅新设备上建立ext4，候选参数为4KiB块、5%保留块、标签DP83DATA，关闭lazy inode/journal初始化
   并禁止mkfs discard以便固定初始条件。示意命令为：
   `mkfs.ext4 -b 4096 -m 5 -L DP83DATA -E lazy_itable_init=0,lazy_journal_init=0,nodiscard <approved-new-device>`。
   不加-F，不自动回答已有签名覆盖提示；这是待批准破坏性操作，绝不用于现存容器卷。
4. 仅挂到新路径`/srv/devpilot83/data`，使用UUID固定来源，候选选项nodev,nosuid,noexec；
   不改全局fstab、不做开机自启。工具和PG二进制不从数据盘执行。原Supervisor/namespace协议不变。
5. 记录格式化后UUID、major:minor、mountinfo、T/A/inode、PG UID视角可用量，确认与S/E/H关系；
   取初始只读基线，不填盘、不做80%实验。文件系统T小于标称10GiB不等于容量被偷偷调整。
6. 若失败，停止环境、保存完整首次stderr/命令/退出码；不得重新格式化到通过。卸载/删除新卷属于
   单独清理授权范围，不能自动清掉仍有在途/未保存证据的环境，更不能使用全局卸载/设备清理命令。

如被迫使用loop，必须另审：O_EXCL新建＋完整分配/落盘证明、底层余量、单一映射与预留、
losetup --find --nooverlap的竞争保护；仅fallocate或truncate不能作为物理独立证明。它不是本稿默认执行路径。
参数依据见[mke2fs](https://man7.org/linux/man-pages/man8/mke2fs.8.html)、
[mount](https://man7.org/linux/man-pages/man8/mount.8.html)、[loop风险](https://man7.org/linux/man-pages/man8/losetup.8.html)。

## 4. 设备映射契约：不编造真实UUID

目标尚未创建，因此下表给出**必须实现并实测的映射**，不是当前真实设备结果。
每行都要在获准环境中填UUID/major:minor/st_dev/mount_id/realpath/容器mount源/PG实际GUC；
任一仍为UNBOUND则该行失败。环境变量、路径名字或相同父目录不能替代真实设备记录。

| 数据 | 宿主固定路径（候选） | 容器内/配置值（候选） | 必须匹配 |
| --- | --- | --- | --- |
| D根 | /srv/devpilot83/data | bind到/var/lib/postgresql | D/UUID_D；当前UNBOUND |
| PGDATA | D/18/docker | PGDATA与SHOW data_directory均为/var/lib/postgresql/18/docker | realpath在D，PG UID/GID已绑定，无外部覆盖 |
| pg_wal | D/18/docker/pg_wal | PGDATA/pg_wal | 非未知symlink，仍为D，WAL实际目录与子目录清单完整 |
| PG日志 | D/pglogs | logging_collector=on，log_directory=/var/lib/postgresql/pglogs，log_destination=stderr，log_file_mode=0600 | D；日志轮转/open-deleted来源另需验证 |
| temp工作文件 | D/18/docker/base/pgsql_tmp | temp_tablespaces为空、无外部tablespace；包含默认工作文件目录 | D；临时关系文件仍可能在base/<OID>，不能漏出F总量 |
| PostgreSQL其他关系/TOAST/索引 | D内已核实的实际数据目录 | 固定R关系只按total计一次 | 未知tablespace/分区/目录视为不完整，不默默忽略 |
| Docker启动/运行stderr | S上的daemon固定日志目录 | 显式json-file max-size=8m/max-file=4候选 | S；不混入PG文件日志L，不当作D占用 |
| 原始证据 | /srv/devpilot83/evidence/<case-id> | 只给可信收集者写；Reporter用stdout/私有IPC输出数字 | E/UUID_E；不与D关系大小相加 |
| 工具/镜像/源码 | S固定路径 | 只读挂载给受限测试驱动 | S；不占D的1GiB关系预算 |
| Authority私钥 | S独立受保护目录，原可信Supervisor身份拥有 | 不进入Reporter/Probe mount namespace | 不读内容、不改变原可信签名来源 |

GUC由可信fixture审计者只读收集：data_directory、log_directory、logging_collector、log_destination、
log_file_mode、temp_tablespaces、default_tablespace、archive_mode、temp_file_limit；
另核对外部tablespace/复制槽/归档/备份不存在或均受明确预算，不向Reporter授监控广权。
PG实际进程Uid/Gid和userns映射来自运行进程/镜像事实，Config.User为空不能当作root或999证明。

启用文件日志是新的E0配置提案；原fixture从docker logs取证的行为不能被静默替换。
未来获准准备环境时应同时收集PG文件日志和有界Docker日志，保留原断言；本轮未改原runner。
默认路径与覆盖依据：[PG文件布局](https://www.postgresql.org/docs/18/storage-file-layout.html)、
[data_directory](https://www.postgresql.org/docs/18/runtime-config-file-locations.html)、
[PG日志配置](https://www.postgresql.org/docs/18/runtime-config-logging.html)。

### 统计不重复计算

R仍按已审固定七表sum(total)计一次，total已含索引/TOAST；table/index分项只解释R，不再累加。
M11不存在时不能假装已有新增两表：E0映射取样不生成“正式七表R通过”结果。
W为当前WAL分配，累计wal_bytes只算增长；L为PG文件日志含可信open-deleted元数据；temp另计。
F为设备总量，已包含上述内容及metadata/预留；不做R+W+L+F总和。
1GiB关系阈值仍是858993460 bytes及以上拒绝；10GiB盘80%用实际T/A计算，不能靠更换设备分母或缩预算通过。

## 5. Reporter身份、目录metadata ACL与内容拒绝

候选独立UID/GID=20083、空附加组，须先在Linux目标证明未占用且与PG/Authority/userns不碰撞。
Reporter和独立Watchdog非root、cap effective/permitted/bounding全空、no_new_privs=1、只读rootfs，
无Docker socket、SYS_ADMIN、DAC override/read-search、ptrace、任意kill或设备内容权限。
E0 DAC阶段无数据库Reporter角色或连接凭据；正式Reporter身份及两个EXECUTE需后续产品授权。

一次性配置者在独立mount namespace准备只读metadata视图：

| 真实源目录 | Reporter视图 | 目录access ACL目标 |
| --- | --- | --- |
| D/18/docker/pg_wal及已枚举受审子目录 | /run/devpilot83/reporter-meta/wal | u::rwx,u:20083:r-x,g::---,m::r-x,o::--- |
| D/pglogs | /run/devpilot83/reporter-meta/pglog | 同上 |
| D/18/docker/base/pgsql_tmp | /run/devpilot83/reporter-meta/temp | 同上，仅已创建的受审工作目录 |

PGDATA根保持PG owner/0700，无Reporter traverse ACL；只映射三个子目录，不挂base/<OID>/配置/Authority。
只读bind由准备者在独立、private传播的mount namespace中完成并核对该namespace的mountinfo，之后才启动Reporter；
不能把PG正在使用的底层D整体remount为只读，也不能让中间的RW视图先暴露给Reporter。
PG文件仍0600且无Reporter ACL；没有default ACL，不递归给文件加r-x，不加入PG组。
新目录ACL缺失、symlink/rename替换、未知挂载或不完整扫描只触发unavailable，不自行修权/扩目录。
挂载只读保证不了不能读内容，必须独立测DAC。

正控制：UID20083可listdir/lstat/statvfs固定目录。负控制：已存在、PG owner已证明可读的受控文件，
Reporter执行O_RDONLY open应为EACCES，探针不读取/输出字节；禁止用ENOENT、工具出错或只读写拒绝冒充内容拒绝。
business/private-key路径不向Reporter暴露时可证明namespace不可达；另以同路径结构的无敏感sentinel
验证目录拒绝，不能声称读过真实业务文件/私钥。轮转/新建/重启后的同样拒绝必须再验证。

PGDATA mode与ACL继承的互动仍须真实Linux核验；原E0发现保留，不默认批准广泛default r-x。
规则参考[ACL](https://man7.org/linux/man-pages/man5/acl.5.html)、[setfacl](https://man7.org/linux/man-pages/man1/setfacl.1.html)。
已unlink日志若没有可信轮转器的数字化fd元数据，则采样不完整；不扩大Reporter到其他进程/proc内容读取。

## 6. 单调Watchdog最小可信路径与尚未授权能力

不把原Supervisor的35秒run截止当成5秒storage时效。新Watchdog独立于采样进程，
不得暂停整个API/PG/Supervisor/清理进程组来“停止新执行”。它只单向收紧新工作。

```text
sample process --> accepted sample receipt --> watchdog (own monotonic clock)
                                            |
                                            +--> deny-only local latch --> coordinator
                                            |                              (new-work path only)
                                            +--> storage_report(NULL, unavailable)
                                                 (requires separate DB capability approval)

existing run --> original Supervisor --> original signed proof/result/resource paths
```

候选协议细节：

- 同一offline登记incarnation/manifest。Watchdog只接受新seq对应的已确认接受回执；begin成功、重复对账、
  旧nonce或普通进程心跳不刷新deadline。用挑战发送前本地单调时刻t0建立保守截止，不把DB接收晚当成新鲜。
- 原设计仍每1s采集、2s发布截止、5s最大样本年龄。候选在t0+3s即触发失效处理，预留原2s数据库事务预算；
  先锁存本地deny，再尝试DB上报。候选本地deny ACK须在250ms内观测，否则失败，不延长现有锁预算。
- 本地单向、固定incarnation的有界pipe/Unix消息只允许deny，无allow/rearm/任意PID命令；关闭/损坏消息也deny。
  Coordinator在线性化锁存后不再发新Claim/Start/Target Permit/launch信封；旧异步allow响应不得清除此锁存。
- in-flight数据库调用仍按原2s结束/未知Commit对账；不得重放网络。暂停/PATCH、紧停、Result、可信物理释放/
  安全清理留在独立安全路径，不能因全局停整个进程而失去能力。
- 新DB上报仅拟议storage_report(incarnation,NULL,deny-only)，Commit确认才算storage unavailable持久化。
  本地IPC不能挡住其他受限Worker直接SQL；最终必须由另获批准的新DB helper在原各入口拒绝。
- DB不可达、ACK未知、墙钟冻结/回拨且不能确认失效时，保持本地deny和未知状态；不宣称已DB闭锁或全体Worker
  绝对5秒拒绝。这个条件未闭合就是验收失败，不能用模拟sink或本地日志替代数据库证明。
- 正常时钟下DB TTL仍是5s；单调时钟优先选择可感知Linux suspend的来源并验证，VM暂停/快照恢复/迁移默认禁止。
  不能借调宿主时钟或恢复旧VM快照延长样本。实例重建需新登记代次，旧latch/nonce不能复活。
- 本地deny在当前incarnation不可由“低水位”消息清除；人工恢复须经授权重建控制代次、可信新样本和所有原门禁。
  Reporter/watchdog无打开部署gate、延授权、重置冷却、签物理证明或返槽权。

维持已审的同一Reporter DB主体/两个EXECUTE方案：Watchdog实现使用deny分支，不等于它的DB ACL只有deny能力。
若产品要求身份级deny-only，必须另审新身份/入口，本稿不自动加第三个EXECUTE或扩大任何既有运行角色。
本地新IPC、Watchdog及可信日志元数据来源也都是新能力，不能因环境规划完成视为已批准实现。

## 7. 锁序与维护边界的收敛

普通DML仍按父链/Run/预算/Reservation到末段deployment；拟议Reporter按deployment→storage state→Reporter登记，
不回取业务行锁，helper只读同语句快照。不得修改M1–10修补或增加现有运行角色权力。

原E0关于pg_total_relation_size/索引/TOAST AccessShareLock和DDL队列的风险继续保留。
本环境首版选择**不支持DDL与采样/业务并行维护**：维护前人工关闭新执行入口、锁存deny、停止采样发布，
保留安全路径处理在途并排空短事务；确认无剩余活动后才开始已获单独批准的DDL。
不得一边持deployment UPDATE计R，一边让未知维护事务排队；不能靠250ms错误退出证明锁图正确。
若后续要求并行DDL/重建索引，另行提交完整关系/索引/TOAST锁序和真实PG屏障证明，不在E0默许。
原约125ms全局锁等待根因不因该维护约束而被称为已修复。

## 8. 本轮状态与阅读顺序

环境/命令/ACL/IPC均是未执行提案。真实UUID、PG UID/GID、Reporter DAC、Watchdog和DB失效链均未实测。
按[验收矩阵与授权清单](E0-ACCEPTANCE-AND-AUTHORIZATION.md)分阶段评审；E0环境子项通过也不批准Migration11。
此前只读E0原件及新材料的哈希在审查包内，首次失败不覆盖；正式仓库保持cb4b527、36/47、8.3未勾选。
