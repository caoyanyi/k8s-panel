# K8s Panel

K8s Panel 是一个独立实现的 Kubernetes 与 Helm 管理面板。后端使用 Go，前端使用 React 和 TypeScript；Kubernetes 访问层基于标准 REST API，Helm 操作使用官方 Helm Go SDK。

## MVP 功能

- 单管理员登录、HttpOnly 会话 Cookie、登录失败限流
- 多集群配置、连接测试、命名空间权限能力检测、启停和删除确认，支持验证后原子轮换访问凭据
- 集群概览、节点/命名空间资源清单与节点诊断详情
- Deployment/StatefulSet/DaemonSet/Job/CronJob/Pod 工作负载查询
- Service/Ingress 网络清单、ConfigMap/Secret 最小配置摘要，以及 PVC/PV/StorageClass 存储摘要
- ServiceAccount、Role、RoleBinding、ClusterRole、ClusterRoleBinding 元数据清单与按需规则/主体详情，支持 ServiceAccount 单动作权限模拟
- 按集群或命名空间查看事件，默认聚焦 Warning，支持本地搜索和分页
- 工作负载详情、脱敏 YAML、关联事件与有界 Pod 日志快照
- Deployment 受控扩缩容、滚动重启与容器镜像更新，带预检、资源版本冲突保护和生产确认
- Helm 仓库配置与连接测试
- Helm Release 查询、安装、升级、回滚和卸载
- Helm 与工作负载写操作共享有界异步队列、同一目标串行执行，排队任务可审计取消
- 基于容器/主机内存和负载压力自适应限制后台操作并发
- 操作记录与审计日志
- Kubernetes 和 Helm 凭据 AES-256-GCM 加密落盘
- HTTPS 目标校验、私网 CIDR 显式许可和响应体大小限制

## 本地运行

需要 Go 1.26.5、Node.js 24 和 npm 11。

```bash
npm --prefix web ci
npm --prefix web run build
export PANEL_ENCRYPTION_KEY="$(go run ./cmd/panelctl encryption-key)"
export PANEL_ADMIN_PASSWORD_HASH="$(printf '%s\n' 'change-this-password' | go run ./cmd/panelctl hash-password)"
go run ./cmd/panel
```

默认监听 `http://127.0.0.1:8080`，管理员用户名为 `admin`。数据默认保存到 `./data/panel.json`。

## 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PANEL_LISTEN_ADDR` | `127.0.0.1:8080` | HTTP 监听地址 |
| `PANEL_DATA_FILE` | `./data/panel.json` | 持久化文件路径 |
| `PANEL_WEB_DIR` | `./web/dist` | 前端构建目录 |
| `PANEL_ENCRYPTION_KEY` | 无 | 必填，32 字节随机值的 Base64 编码 |
| `PANEL_ADMIN_USERNAME` | `admin` | 管理员用户名 |
| `PANEL_ADMIN_PASSWORD_HASH` | 无 | 必填，Argon2id 编码密码哈希 |
| `PANEL_SESSION_TTL` | `8h` | 会话有效期，最大 24 小时 |
| `PANEL_HELM_TIMEOUT` | `5m` | 单次 Helm 操作超时 |
| `PANEL_HELM_WORKERS` | `2` | 后台操作最大并发数（兼容原 Helm 配置名），低配置主机建议设为 `1` |
| `PANEL_OPERATION_QUEUE_SIZE` | `64` | 后台操作等待队列容量，范围 1 到 128 |
| `PANEL_ADAPTIVE_OPERATIONS` | `true` | 按当前内存与系统负载收缩后台操作、集群读取和客户端缓存容量 |
| `PANEL_KUBERNETES_READ_CONCURRENCY` | `4` | Kubernetes 与 Helm 读取最大并发数，容量不足时快速返回 `503` |
| `PANEL_KUBERNETES_CLIENT_CACHE_SIZE` | `8` | Kubernetes 客户端缓存上限，范围 1 到 64 |
| `PANEL_KUBERNETES_CLIENT_CACHE_TTL` | `10m` | Kubernetes 客户端闲置有效期，范围 1 分钟到 1 小时 |
| `PANEL_MAX_CONCURRENT_REQUESTS` | `16` | API 同时处理的请求上限，超限快速返回 `503` |
| `PANEL_ALLOWED_PRIVATE_CIDRS` | 空 | 允许访问的私网 CIDR，逗号分隔 |
| `PANEL_SECURE_COOKIES` | `false` | HTTPS 部署必须设为 `true` |
| `PANEL_LOG_LEVEL` | `info` | `debug`、`info`、`warn` 或 `error` |

面板默认拒绝访问私网地址。若 Kubernetes API 或 Helm 仓库位于私网，应将其精确网段加入 `PANEL_ALLOWED_PRIVATE_CIDRS`，不要无条件放开全部内网。

Kubernetes 单类资源清单最多读取 5,000 项、20 页和 32 MiB 原始数据，Web 表格每页只渲染 100 行；工作负载同时查询多种类型时共享这一总预算并固定串行读取，指定类型时只请求对应 API。事件中心不自动轮询，默认在 API Server 侧过滤 Warning，单次最多串行读取 8 页、2,000 项和 16 MiB 原始事件并返回最近 200 条，用户可请求的返回上限为 500。配置与存储清单使用 Kubernetes Table 内容协商，只接收最小摘要且不回退读取完整对象；Secret 必须限定到单个命名空间，PV 卷源、CSI 标识、挂载选项和 StorageClass 参数不会进入面板响应。访问控制清单一次只读取一种资源并使用元数据内容协商，命名空间资源不允许全集群读取；清单最多 8 页、2,000 项和 16 MiB，单对象详情最多 2 MiB，规则和主体分别最多展示 128 项，ServiceAccount 详情不返回 Secret 名称。ServiceAccount 权限模拟每次只提交一个 64 KiB 上限的 SubjectAccessReview，不扫描 RBAC 图谱、不轮询、不缓存结果。超过后端上限会停止读取并返回错误，避免大集群拖垮控制面。

自适应资源控制默认在内存或归一化系统负载达到 80% 时，将后台操作、Kubernetes 读取并发和客户端缓存容量分别减半；达到 95% 时暂停启动新任务、快速拒绝新的集群读取、显式连接测试、权限能力检测和凭据轮换，并将客户端缓存收缩到 1。权限能力检测固定顺序执行 10 次轻量授权检查，单批次只占用一个读取槽，不轮询、不缓存结果；重复检测同一集群和命名空间时快速拒绝。凭据轮换先使用独立客户端验证候选凭据，成功后再原子替换，失败时保留原凭据和缓存连接。客户端按集群与凭据版本复用，闲置超时、LRU 淘汰、禁用/删除集群或服务关闭时释放空闲连接，不缓存 Kubernetes 对象和日志。指标暂不可用时按配置上限运行。低配置主机建议设置 `PANEL_HELM_WORKERS=1`、`PANEL_KUBERNETES_READ_CONCURRENCY=2`、`PANEL_KUBERNETES_CLIENT_CACHE_SIZE=4`、较小的 `PANEL_OPERATION_QUEUE_SIZE`，并按实际流量下调 `PANEL_MAX_CONCURRENT_REQUESTS`。

单节点文件存储最多保留最近 2,000 条操作记录和 5,000 条审计记录；排队中或执行中的操作不会因历史清理而移除。

MVP 不加载本地 Chart、Helm 插件或宿主机凭据。OCI 模式仅支持匿名拉取；Chart provenance/OpenPGP 校验暂未启用。

## 容器运行

```bash
docker build -t k8s-panel:local .
export PANEL_ENCRYPTION_KEY="$(docker run --rm --entrypoint /app/panelctl k8s-panel:local encryption-key)"
export PANEL_ADMIN_PASSWORD_HASH="$(printf '%s\n' 'change-this-password' | docker run --rm -i --entrypoint /app/panelctl k8s-panel:local hash-password)"
docker run --rm --name k8s-panel -p 8080:8080 \
  -e PANEL_LISTEN_ADDR=0.0.0.0:8080 \
  -e PANEL_ENCRYPTION_KEY \
  -e PANEL_ADMIN_PASSWORD_HASH \
  -v k8s-panel-data:/data \
  k8s-panel:local
```

生产环境应在 TLS 终止代理之后运行，并启用 `PANEL_SECURE_COOKIES=true`。Kubernetes 示例清单位于 `deploy/kubernetes.yaml`，部署前需创建 Secret：

```bash
kubectl create namespace k8s-panel
kubectl -n k8s-panel create secret generic k8s-panel-secrets \
  --from-literal=encryption-key="$PANEL_ENCRYPTION_KEY" \
  --from-literal=admin-password-hash="$PANEL_ADMIN_PASSWORD_HASH"
kubectl apply -f deploy/kubernetes.yaml
```

## 版本发布

向 GitHub 推送符合 SemVer 的 `v` 前缀标签后，会自动执行测试、跨平台打包、GHCR 多架构镜像发布和 GitHub Release 创建。标签必须指向 `main` 已包含的提交，例如：

```bash
git tag -a v0.2.0 -m "v0.2.0"
git push origin v0.2.0
```

预发布版本可使用 `v0.2.0-rc.1`。Release 包含 Linux、macOS 和 Windows 归档及 `checksums.txt`；镜像发布为 `ghcr.io/caoyanyi/k8s-panel:v0.2.0`、`ghcr.io/caoyanyi/k8s-panel:0.2.0` 和对应的 `sha-<commit>` 标签，不发布 `latest`。

二进制可通过 `panel --version` 和 `panelctl version` 查看内嵌版本及提交号。

## 验证

```bash
go test -race ./...
go vet ./...
npm --prefix web run typecheck
npm --prefix web test
npm --prefix web run build
npm --prefix web run e2e
```

MVP 使用单节点文件存储，适合单副本部署。高可用、多租户 RBAC、OIDC、资源 YAML 编辑、终端和日志流式跟随属于后续版本范围。
