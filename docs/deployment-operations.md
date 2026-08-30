# 部署与运维

> 主方案为宿主机 OpenCLI + 可见 Chrome；本文同时定义安全和运维边界。

## 1. 部署拓扑

### 1.1 已确认主方案：宿主机 OpenCLI + 可见 Chrome

```text
┌────────────────────────────┐
│ LAN 浏览器 / React Web UI   │
└─────────────┬──────────────┘
              │ HTTPS / REST
              ▼
┌─────────────────────────────────────────────────────┐
│ 宿主机 Go + PocketBase（单进程、单实例）             │
│  - API / 静态前端                                    │
│  - conversations / turns / tasks                    │
│  - Gemini operation queue                           │
│  - os/exec.CommandContext                           │
└─────────────┬───────────────────────────────────────┘
              │ 启动同一宿主机上的 opencli 子进程
              ▼
┌─────────────────────────────────────────────────────┐
│ @jackwener/opencli v1.8.7                           │
│  localhost:19825 daemon ↔ Extension v1.0.23         │
└─────────────┬───────────────────────────────────────┘
              ▼
       可见 Chrome（专用 OpenCLI profile）
              ▼
          Gemini Web
```

此方案成立的原因：

- `exec.Command` 启动的是同一宿主机上的 OpenCLI，可执行边界真实存在。
- 用户能直接看到并操作 provider 登录页面。
- 使用宿主机上的专用 Chrome/OpenCLI profile，无需在容器中维护扩展、显示服务器和登录入口。
- OpenCLI daemon 继续只绑定 localhost。

### 1.2 部署前置条件

- Node.js `>=20`。
- 后端使用专用 OS 服务账号/HOME，不与日常 OpenCLI 配置混用。
- `@jackwener/opencli@1.8.7`，启动探针必须校验版本。
- `~/.opencli/clis/gemini` 不存在且没有已安装 OpenCLI plugin；否则拒绝 Gemini 写操作。
- 与该 release 匹配的 Browser Bridge Extension v1.0.23，Bridge 探针必须校验版本。
- Chrome/Chromium 处于运行状态且扩展已连接。
- 配置专用 `OPENCLI_PROFILE`，每次调用显式指定；应用独占其 OpenCLI Adapter tab。
- 用户已在该专用 profile 的 Gemini Web 人工登录。
- Go 后端服务账号有权限执行 `opencli`，但不读取 Chrome profile 文件。

### 1.3 后置方案：远程 Runner

只有宿主机方案无法满足部署要求时，才增加 Node Runner 服务：

```text
Go backend → 私有 HTTP/Unix Socket → Node Runner → spawn opencli
```

Runner 必须命令白名单化，且 19825 仍不得暴露。该方案不属于 v1。

---

## 2. 安全边界

平台能操作用户的真实 Gemini 账号，不能沿用“无鉴权 LAN”作为默认值。

v1 上线条件：

- 应用始终启用 Basic Auth；另通过 Tailscale/VPN 或 HTTPS 反向代理保护传输。
- Basic Auth 不允许运行在明文公网 HTTP 上。
- 非 loopback 监听时，认证凭据、可信 Host 和可信 Origin 缺失必须拒绝启动。
- Go 后端只绑定受控接口；OpenCLI daemon 始终只在 loopback。
- 对 UI/API 应用全局认证，仅显式放行 health；保护或禁用 PocketBase 管理端和系统路由。
- 对写 API 检查 Origin/Host，并只接受 JSON。
- 不记录完整 cookie、profile 路径或未脱敏 stderr。
- OpenCLI 命令使用 `exec.CommandContext(name, args...)`，禁止 `sh -c`；子进程移除 `NODE_OPTIONS/NODE_PATH`。
- README 明示网站 ToS、额度消耗和封号风险。

---

## 3. 健康检查与运维

健康状态分层，避免一个笼统的 healthy：

1. **Backend**：`GET /api/health`，只检查进程和 SQLite。
2. **Bridge**：启动期或 active 尚无成功 turn 时，低频运行带专用 profile 的 `opencli doctor`。
3. **Gemini auth**：同一安全阶段运行 `gemini status/whoami`；active 有成功 turn 后只显示缓存，避免导航 shared tab。
4. **Adapter functional**：仅显式 opt-in 的人工 smoke prompt，不能拿真实写操作做定时 healthcheck。

版本策略：

- OpenCLI 固定 v1.8.7。
- Extension 固定 v1.0.23，并作为同一升级单元记录。
- baseline 不打 patch。
- 升级时先在 Stage 0 合同测试中验证 Gemini 命令、输出字段、退出码、模型和登录。
- 网站改版可能让锁定版本失效；锁版本只防依赖漂移，不能防 DOM 漂移。

备份：

- PocketBase 数据目录和备份使用 owner-only 权限。
- Chrome 登录态由用户的专用 OpenCLI profile 管理，平台不复制、不导出 cookie。
- 恢复数据库不会自动恢复远端网页上下文，旧会话按只读处理。
