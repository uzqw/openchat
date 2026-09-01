# 实施路线图

> 当前：Gemini 单一 provider。Stage 0–3 已完成（Stage 0/3 的宿主机实测待真实 Chrome），Stage 4 按需进行。

## 1. 分阶段状态

### Stage 0：OpenCLI 合同 PoC — 完成（自动化部分）

通过锁定源码 fixture 与 fake OpenCLI 完成：命令/参数/成功 JSON/错误退出码合同、`[NO RESPONSE]` sentinel 识别、timeout/kill/session busy/Chrome 中断分类。目标宿主机实测（版本与 Extension 匹配、`gemini models` 实际输出、live smoke、两轮连续追问）待真实 Chrome 环境执行。

### Stage 1：最小产品 — 完成

Go/PocketBase + React 骨架、Basic Auth、conversations/turns/tasks、FIFO operation queue、`POST /turns` + 轮询、登录状态/模型选择/错误引导、当前会话连续追问、历史只读、fake OpenCLI 自动化测试。

### Stage 2：恢复与运维 — 完成

重启安全降级（pending→canceled、running→unknown、active→archived）、队列原子准入、pending 取消、受限人工重试、unknown 隔离与人工解除、Bridge/auth/login 分层缓存状态、输出上限与脱敏、版本锁定 write guard。

### Stage 3：旧会话续聊 — 完成（代码层面）

首轮成功后捕获 `conversations.remote_id`；恢复时 `gemini detail <id>` 导航 + `status` URL 校验（不匹配则中止）；无 `remote_id` 保持只读。`gemini detail` 对真实会话的导航与恢复后上下文正确性待真实 Chrome 实测。

### Stage 4：按需增强 — 部分完成

- [x] 移除第二 provider，收敛为 Gemini 单一站点：Site 能力表仅保留 gemini，`OPENCLI_SITE` 固定为 `gemini`（非法值启动即拒），前端按站点能力渲染的选择器恒为 gemini 能力。
- [ ] SSE 替代轮询，仅在轮询体验确实不足时添加。
- [ ] 图片与附件。
- [ ] 导入网站历史。

## 2. 主要风险

| 风险 | 影响 | 当前措施 |
|---|---|---|
| 网站 ToS/封号 | Gemini 账号限制 | Gemini 串行、有限队列、无不确定结果自动重发 |
| 网站改版 | Gemini adapter 失效 | 合同 PoC、版本锁定、明确不可用状态 |
| 登录过期 | 任务失败 | `auth_required` + 可见 Chrome 人工登录 |
| 网页上下文串线 | 回答进入错误会话 | 每个会话带 `provider` 字段，恢复时按会话的 `detail` 导航 + URL 校验（不匹配则中止），旧会话只读 |
| 超时后重复提交 | 重复消耗额度 | `unknown_outcome` 归档会话并隔离，禁止直接重试 |
| LAN 未授权访问 | 他人操作真实 Gemini 账号 | 默认 HTTPS/VPN + Basic Auth |
| Chrome/Bridge 中断 | Gemini 暂时不可用 | 后端与历史仍可用，分层显示 Bridge 状态 |
