# 实施路线图

> 按验证优先顺序推进；Stage 0 未通过的 provider 可以禁用，不阻塞其他 provider。

## 1. 分阶段计划

### Stage 0：OpenCLI 合同 PoC

先写一个最小脚本或 Go 命令，不做完整 UI/数据库。

必须在目标宿主机验证：

- [ ] OpenCLI v1.8.7 与 Extension v1.0.23 连通。
- [ ] 五家 `login/status/whoami`。
- [ ] 五家新会话提问及 `--format json` 实际输出 fixture。
- [ ] Kimi 完整回答提取，不能只取 300 字预览。
- [ ] ChatGPT、Gemini、Kimi、DeepSeek 的实际模型能力。
- [ ] 两家 provider 并行提问。
- [ ] 同 provider 并发时的 session busy 行为。
- [ ] 超时、kill、Chrome 重启后的网页实际状态。
- [ ] 当前活跃会话至少完成两轮连续追问。

验收：每家形成一份“命令、参数、成功 JSON、错误退出码、是否完整回答”的合同记录。未通过的 provider 可在 v1 中禁用，不阻塞其他 provider。

### Stage 1：最小产品

- [ ] Go/PocketBase + React 骨架。
- [ ] Basic Auth + HTTPS/VPN 入口。
- [ ] conversations / turns / provider_tasks。
- [ ] 每 provider operation queue。
- [ ] `POST /turns` + 轮询。
- [ ] 当前 active conversation 连续追问。
- [ ] 历史会话只读。
- [ ] 先接入一个 PoC 最稳定 provider，再按合同表扩到其余 provider。

验收：当前会话可连续追问，同一轮可并排看到所有已启用 provider 的完整结果。

### Stage 2：恢复与运维

- [ ] pending 重启恢复，running → unknown_outcome。
- [ ] 队列上限、取消、人工重试。
- [ ] Bridge/auth 分层状态。
- [ ] 日志截断与脱敏。
- [ ] 数据备份与恢复演练。
- [ ] OpenCLI 版本升级合同测试。

### Stage 3：旧会话续聊

逐家验证并实现：

- [ ] 保存远端 conversation ID/URL。
- [ ] 从历史中重新定位 provider 网页会话。
- [ ] 验证恢复后不会串到其他本地会话。
- [ ] 不支持可靠恢复的 provider 保持旧会话只读。

### Stage 4：按需增强

- [ ] SSE 替代轮询，仅在轮询体验确实不足时添加。
- [ ] 图片与附件。
- [ ] 导入网站历史。
- [ ] 更丰富的 provider 能力。

---

## 2. 主要风险

| 风险 | 影响 | 当前措施 |
|---|---|---|
| 网站 ToS/封号 | 账号限制 | 单 provider 串行、有限队列、无不确定结果自动重发 |
| 网站改版 | adapter 失效 | 合同 PoC、版本锁定、provider 独立禁用 |
| 登录过期 | 任务失败 | `auth_required` + 可见 Chrome 人工登录 |
| 网页上下文串线 | 回答进入错误会话 | v1 只有一个 active conversation，旧会话只读 |
| 超时后重复提交 | 重复消耗额度 | `unknown_outcome`，人工确认重试 |
| LAN 未授权访问 | 他人操作真实账号 | Stage 1 即启用 HTTPS/VPN + Basic Auth |
| Kimi 结果截断 | 展示不完整 | Stage 0 阻塞验证；未解决则禁用 Kimi |
| Chrome/Bridge 中断 | 全部 provider 暂时不可用 | 后端与历史仍可用，分层显示 Bridge 状态 |
