# 本地 MVP 启动与交付

统一入口（仓库根目录；不需要先合并分支）：

```
./scripts/mvp.sh manage   # 默认：普通管理，持久化本地开发库，不执行检查
./scripts/mvp.sh demo     # 隔离执行演示，临时库，固定fixture，须真实Owner确认
./scripts/mvp.sh --help
```

| 模式 | 页面 | 用户操作 | 执行/数据生命周期 |
| --- | --- | --- | --- |
| manage | http://127.0.0.1:5173 | 注册/登录→创建Workspace→Project→Monitor→授权/暂停→状态/历史 | 专用PG18开发库持久化；gate关闭，无Worker；任意URL仅保存配置，不会探测 |
| demo | http://127.0.0.1:5174 | 临时Owner登录→选择预置Workspace/Project/固定Monitor→核对目标并勾选授权→三周期→历史/耗时 | 原9.1内网双Worker/Supervisor；仅固定无上游fixture；60秒周期最多3次，最长15分钟；停止销毁测试库 |

共用 React Dashboard、Go业务处理器、Cookie/CSRF/ETag、历史API与原调度/物理释放链；**两个模式的数据不互相复制**，普通用户创建的任意URL不接入演示执行器。演示无需注册，登录资料在启动终端指明的本机0600临时文件，结束后删除，不在聊天或证据中输出密码。

前置：Docker/Compose、Node24+/npm、Python3、OpenSSL、IPv4/IPv6回环；manage另需Go1.24.3+，demo复用原9.1 Linux容器、NET_ADMIN/IPv6工具。首次构建依赖需要网络；实际fixture没有公网出口。旧两个启动脚本仍可直接使用。Ctrl-C只停止本次脚本拥有的进程/资源；manage保留数据卷，demo销毁本次独立库。端口被占用时报告阻塞，不抢占未知进程。

**并行登录注意**：当前两个端口沿用同一loopback host/Cookie名；Cookie不按端口隔离。并行使用请选独立浏览器配置或无痕会话，避免覆盖另一模式的登录Cookie。账号/数据库仍独立，跨库Cookie不能通过实时身份鉴权；本轮记录为P2，不引入新认证体系。

细节：[普通管理](web-dashboard.md)、[历史接口及游标](monitor-history.md)、[隔离演示边界](isolated-web-demo.md)。原60秒、2秒事务、250ms锁预算、Fence、unknown和可信物理释放条件不变；没有新生产迁移、权限或出口。

## 当前分支依赖（2026-11-06实时核实）

| PR | 状态 | head（完整SHA） | 当前base |
| --- | --- | --- | --- |
| #11 | 已合并；main为5905f518632025c32b2a02c6f21ab675bf45bb2c | 765f15edc77381ec08cb3c74ed48e73043cb6952 | main |
| #12 | Open | 0462dc2e65c8ea9edd3948cf6a72d483e93f7510 | main |
| #13 | Open | 00569ebc82fceb41f76d984c02c64c13cd8b53ef | codex/task83-storage-design（e969fafa272d1d33bab3a303bfaa1619f552ffc5） |
| #14 | Open | 52156a2045ce8cb2c2ff57417644e208eb98d2ed | codex/task91-isolated-periodic |
| #15 | Open | c2c46abccbcccf14b38701d79a9e2ad274d89bb2 | codex/mvp-web-dashboard |
| #16 | Open，用户已通过隔离演示范围审查 | f576d3dfc8ad8088a13423054ba6c1967341594e | codex/mvp-monitor-history |

现checkout以#16为祖先，已包含整个功能链。本轮仅从该已验收HEAD继续最小交付改动，未merge/cherry-pick/rebase/retarget任何分支。main仍只有#11阶段；“本地可运行”不代表已合入main。

Migration固定顺序：**1–8（#11）→9–10（#12）→12（#13，已独立批准的完成调度修复）→13（#15，最小历史只读函数）**。编号11刻意缺席，8.3仍延期，不补空号、不重编号、不改已验收迁移。Goose按现有版本顺序应用，普通启动仅操作专用本地库，演示只操作新可销毁库。生产应用12/13需要另行批准；13既有的受限函数EXECUTE已在#15审查，本轮不增加权限。

## 最小发布前缺口

| 类别 | 缺口/结论 | 下一步 |
| --- | --- | --- |
| 本地MVP必须修复（本轮完成） | 启动入口分散；演示注册入口必然403；错误来源提示误指普通端口；管理模式可继承演示环境变量 | 统一mvp.sh复用原脚本；隐藏演示注册并提示临时Owner；通用正确入口提示；管理模式显式关闭演示flag；通过实际入口做两条必要整合流程 |
| 合入main前必须授权 | #12–#16仍Open，#13还依赖两份已审查8.3设计提交 | 按下面精确范围批准后才能合并；本轮不执行 |
| 公开/生产执行前必须单独完成 | 当前只有本地Vite/有界fixture，普通Worker命令仍拒绝生产启动；生产HTTPS/部署流程与出口、运行预算/存储准入尚未验收 | 单独产品授权和对应验证；不把本地演示当作生产能力，也不把这些强化全部作为无依赖Web功能的前置 |
| 可以延期（P2） | 同host Cookie使用限制；删除/告警；常驻执行、自动恢复体验；8.3与9.2–10.5未完成项 | 明示范围，按产品优先级推进；本轮不扩框架或重复历史矩阵 |

### 待批准的合并范围（本轮不执行）

1. 将#12的0462dc2合入main。
2. 明确允许保留中间设计文档提交 cb4b527af7e75c1ff85c3a49f7a51ee427ae55f0、e969fafa272d1d33bab3a303bfaa1619f552ffc5（含8.2验收/8.3设计及失败证据，仅文档，不代表8.3完成）。然后将#13 base改为main并合入00569eb；这样#13新增范围包含上述两份文档提交及9.1，不丢设计与负面证据。
3. 依次在父项合入后，另获允许将#14、#15、#16 base改为main并合入各自表列head，最后处理本轮交付PR。每步以重新核对的head/base及必要门禁为准，不用旧CI替代变化后的验证。

这是可审查的合并申请范围，不是执行授权；不得借此生产迁移、Archive、部署或开公网。OpenSpec保持37/47，8.3及9.2–10.5不勾选。125ms锁等待、IPv6和管理确认超时的原失败与未知根因全部保留。
