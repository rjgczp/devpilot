# Task8.1 开发负面证据

这些是开发阶段真实失败输出，不是冻结实现提交的通过证据。原record回执、完整源码清单、
命令/退出码和全部原始日志保留在`/private/tmp/devpilot-task81/`及交付归档。
开发head为a0273be且dirty=true。最终冻结SHA的通过结果须查GitHub Actions Artifacts。

- `first-duplicate-account*.log`：同一新测试重复构造固定账号；PG明确users_email_key，
  原测试退出1。只修新夹具子测试命名。
- `first-inventory-mismatch.log`：精确测试清单漏掉新增UpgradeInFlight；runner退出1，
  测试尚未执行。保留全部原23项，补齐新清单为31。
- `first-truncate-boundary.log`：新测试要求23514，实际先被FK以0A000拒绝，退出1。
  不移除FK：保留0A000检查，再增加CASCADE负向请求验证新Trigger的23514。
- `first-unreviewed-migration-unit.log`：原白名单只允许8个迁移，unit退出1。
  仅纳入已批准的精确Migration9，并增加原Migration8哈希保护，生产依赖/启动断言不变。
- `first-migration8-repeat-extract.jsonl`：从完整core-05原始JSONL逐行提取该测试事件，
  没有重写事件内容。原组退出1；重复up误升9，Down因而没有执行8→7的退役动作。
  历史用例只改目标up-to8，原g/冷却断言不改；新9生命周期由专门测试验证。
- `migrations1-8.sha256`：本轮开始前哈希，修改后逐份复核一致。

另一次CI合同检查发现向冻结7.1文件添加断言改变了其哈希。原工具输出为
`test_original_71_assertions_unchanged_and_history_pin_explicit`失败，实际哈希
`99c2abd1452505bcef58fcecc77c55c1a97580016317e65ff0fc374753b8cf14`，
期待`1ad62b367f57f82d93b565be732558a84abaa9408449a2594195a2ba1bba30a0`。
该首次输出仅在会话工具记录中保存，未单独捕获内层退出码；外层命令串被末尾diff check
遮蔽为0，不能当作测试通过。已恢复原文件，未更改期待哈希，观察断言移到7.5。
复验使用独立record.py，`checks/ci-contract-fixed.json`记录退出0。

历史IPv620s及既有风险继续保留。无失败被当作物理证明，无权限检查或网络断言被关闭。
