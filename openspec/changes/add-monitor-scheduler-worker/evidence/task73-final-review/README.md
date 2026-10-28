# Task 7.3 最终验收待产品范围确认（2026-10-28）

实现及测试基线：`a1fc65a0dbc9c53259d9b06a411250575f35fa6d`。
独立代码审查认可 A 项保守 unknown，冻结 SHA 的 Push/PR CI 均成功；
本次只整理审计材料，不重复测试，不表示产品最终验收或任务完成。

## 待接受的实际能力边界

当前实现**不支持**把缺少可区分、可信、已持久化许可拒绝证据的 started Run
归类为 canceled；不存在目标许可、Controller 的 egress_unavailable、
可信物理释放三者不能补足“明确拒绝事件”的来源证明。
两种既有 Worker 调用可留下相同 stop_reason，因此这些 Run 均保持 unknown。
补充可信事件来源须后续单独评审，不在 7.4 中补字段/权限/信任。

unknown 终态不可逆；不重发旧 GET；HTTP 结果、物理终止和返槽为不同事实。
真实物理释放后仍为 unknown，不会伪造 HTTP success=false、timeout 或 completed。
既有合法终态和未 Start 的取消/过期行为不因本限制被改写。

产品负责人明确接受上述范围收缩前，7.3 不勾选，进度 **31/47**。
不改写 tasks/Delta 原始设计为已达成；不从本次“准备收尾”推断产品接受。
随后 7.4 也只准备方案，实施仍待正式授权。

## 已核验的冻结证据

- [Push 40613022394](https://github.com/rjgczp/devpilot/actions/runs/40613022394)：verify、integration、execution-security success。
- [PR 40613027846](https://github.com/rjgczp/devpilot/actions/runs/40613027846)：同三项 success。
- `pr-checks.json` 为本次实际读取的六项结果；`artifact-index.json` 为六份原始 ZIP 的路径、链接、大小及 SHA256。
- 本次实际重新读取六份本地 ZIP，大小/哈希全部一致；只读核验退出 0，未重跑验收。
- 冻结原始 7.3 矩阵每次 CI 各 22 normal + 22 race；两份 recovery44 audit 及 provenance-collision audit 是前次只读核验的原样副本，不作为本次新测试。
- 同 SHA 原始网络、PG18、签名/PIDFD/返槽、命令/SQLSTATE/接收端与日志都在索引所指原始 security/integration Artifacts；本地完整解压与 ZIP 保留于 `/private/tmp/devpilot-task73-a-ci-20261028/artifacts/`。
- 完整冻结交付报告及小型审查包仍保留在 `/private/tmp/devpilot-task73-a-ci-20261028/`；包 SHA256 为 `56f4fd0a90934d814dc5694f7b043a5584a2de9f6c33fe17265eedd853af0830`。

大型原始 trace 不重复提交 Git。外部审查应下载 GitHub Artifact 并对照哈希；
临时目录不是永久存储保证，Artifact 也有保留期限，长期归档前须按清单保全原始 ZIP。
本次保留已有原件，不将哈希索引冒充原始物理执行证据。

## 仍保留的负面证据与风险

- 历史 PR40468671798 的 IPv6 路由配置超过原 20 秒；根因未确定、未修复。
  原 Push40468664422 cancelled，不能替代该失败。原失败命令/traceback/容器/路由/
  receiver/Runner 对比仍在 `../task73-ci-blocker/` 和 `../task73-review2/`。
- 旧安全 Artifact12719305073 的 ZIP SHA256：
  `8d3a5acf0eaa53bec18a66fd84db872b4bf8cadce66fa55bdbed684e7468f970`。
- 本轮冻结 CI 通过不证明历史超时已解决；没有延长预算、删除 IPv6、跳过用例或反复 rerun。
- `retained-negative-manifest.json` 逐文件绑定现有 7.3 负面原件及审查记录，未修改原件。
- 之前 SIGCONT/管理确认、PID1/tracer、NSCD、permit-missing 等原失败及未确定原因继续保留；历史部署角色清单限制不变。
- 本次一次只读 GitHub 元数据请求 EOF 记录在 `read-only-check.json`，没有触发 CI 或测试重试。

本次不修改 SQL、Go、数据库、ACL、物理返槽、网络规则或测试断言。
生产执行硬关闭；PR11 Open，不合并、Archive、部署或进入 7.4。

## Subsequent explicit product acceptance (2026-10-28)

The human owner has now explicitly accepted the exact conservative scope above.
`product-acceptance.json` records the direct message and the rechecked frozen
Artifact/negative evidence. Task7.3 is complete at32/47. Earlier pending-product
statements in this document and read-only-check.json are retained history.
Precise canceled classification remains deferred; the historical IPv6 failure
is not fixed. Only7.4 implementation is separately authorized now.
