# 实施路线图

> 第一版只实现 Gemini；本次可发布交付必须完成 Stage 0–2，Stage 3–4 后置。

## 1. 分阶段计划

### Stage 0：OpenCLI 合同 PoC

先写一个最小脚本或 Go 命令，不做完整 UI/数据库。

必须在目标宿主机验证：

- [ ] OpenCLI v1.8.7 与 Extension v1.0.23 版本匹配并连通。
- [ ] 专用服务 HOME 没有 Gemini adapter override 或 OpenCLI plugin。
- [ ] 每个命令显式使用专用 `OPENCLI_PROFILE`。
- [ ] 在没有 active turn 时验证 Gemini `login/status/whoami`；active 有成功 turn 后不得运行任何非 ask OpenCLI operation。
- [ ] `gemini models --format json` 的实际输出。
- [ ] 显式设置 `LIVE_GEMINI_SMOKE=1` 后，验证 Gemini 新会话提问及脱敏 JSON fixture。
- [ ] 通过锁定源码 fixture 和 fake OpenCLI 识别退出码 0 的 `[NO RESPONSE]` sentinel，不能在真实账号上故意触发。
- [ ] opt-in smoke 验证 `--model`、`--thinking` 的实际行为；确认 v1.8.7 不提供可靠 per-model thinking 矩阵。
- [ ] 通过 fake OpenCLI 验证 timeout、kill、session busy 和 Chrome/Bridge 中断分类，不在真实账号上执行破坏性故障注入。
- [ ] 当前活跃会话至少完成两轮连续追问。

验收：形成 Gemini 的“命令、参数、成功 JSON、错误退出码、完整回答和上下文”合同记录。真实 Chrome 不可用时只允许标记 live smoke pending，不能声称通过。

### Stage 1：最小产品

- [ ] Go/PocketBase + React 骨架。
- [ ] Basic Auth，并另配 HTTPS/VPN 传输保护。
- [ ] conversations / turns / tasks。
- [ ] 一个 Gemini operation queue。
- [ ] `POST /turns` + 轮询。
- [ ] Gemini 登录状态、模型选择和错误引导。
- [ ] 当前 active conversation 连续追问。
- [ ] 历史会话只读。
- [ ] 自动化测试使用 fake OpenCLI，真实账号只用于人工 smoke。

验收：当前会话可连续追问并展示 Gemini 完整结果；新会话不会继承旧上下文；所有自动化检查通过。

### Stage 2：恢复与运维

- [ ] 重启安全降级：pending → canceled、running → unknown、active → archived。
- [ ] 队列原子准入、pending 取消、受限人工重试。
- [ ] unknown outcome 归档会话、隔离 Gemini、人工确认解除。
- [ ] Bridge/auth/login operation 分层缓存状态。
- [ ] 流式输出上限、日志截断与脱敏。
- [ ] 数据备份与恢复演练。
- [ ] OpenCLI 版本升级合同测试。

### Stage 3：旧会话续聊

已实现：

- [x] 保存 Gemini 远端 conversation ID/URL（首轮成功后 `gemini status` 捕获到 `conversations.remote_id`）。
- [x] 从历史中重新定位 Gemini 网页会话（`gemini detail <id>` 导航 persistent tab）。
- [x] 验证恢复后不会串到其他本地会话（`status` URL 校验，不匹配则中止）。
- [x] 无法可靠恢复时继续保持旧会话只读（无 `remote_id` 拒绝续聊）。

待实测验证（需真实 Chrome）：

- [ ] 在目标宿主机验证 `gemini detail` 对真实会话的导航与 `status` URL 校验。
- [ ] 验证恢复后连续追问的上下文正确性。

### Stage 4：按需增强

- [ ] 接入第二个 provider，再提取最小通用抽象。
- [ ] SSE 替代轮询，仅在轮询体验确实不足时添加。
- [ ] 图片与附件。
- [ ] 导入网站历史。

---

## 2. 主要风险

| 风险 | 影响 | 当前措施 |
|---|---|---|
| 网站 ToS/封号 | Gemini 账号限制 | Gemini 串行、有限队列、无不确定结果自动重发 |
| 网站改版 | Gemini adapter 失效 | 合同 PoC、版本锁定、明确不可用状态 |
| 登录过期 | 任务失败 | `auth_required` + 可见 Chrome 人工登录 |
| 网页上下文串线 | 回答进入错误会话 | v1 只有一个 active conversation，旧会话只读 |
| 超时后重复提交 | 重复消耗额度 | `unknown_outcome` 归档会话并隔离，禁止直接重试 |
| LAN 未授权访问 | 他人操作真实 Gemini 账号 | Stage 1 即启用 HTTPS/VPN + Basic Auth |
| Chrome/Bridge 中断 | Gemini 暂时不可用 | 后端与历史仍可用，分层显示 Bridge 状态 |
