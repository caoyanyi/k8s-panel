# K8s Panel

K8s Panel 是一个独立实现的 Kubernetes 与 Helm 管理面板。后端使用 Go，前端使用 React 和 TypeScript；Kubernetes 访问层基于标准 REST API，Helm 操作使用官方 Helm Go SDK。

## MVP 功能

- 单管理员登录、HttpOnly 会话 Cookie、登录失败限流
- 多集群配置、连接测试、启停和删除确认
- 集群概览、命名空间与 Deployment/StatefulSet/DaemonSet 工作负载查询
- Helm 仓库配置与连接测试
- Helm Release 查询、安装、升级、回滚和卸载
- Helm 写操作异步队列、同一 Release 串行执行
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
| `PANEL_ALLOWED_PRIVATE_CIDRS` | 空 | 允许访问的私网 CIDR，逗号分隔 |
| `PANEL_SECURE_COOKIES` | `false` | HTTPS 部署必须设为 `true` |
| `PANEL_LOG_LEVEL` | `info` | `debug`、`info`、`warn` 或 `error` |

面板默认拒绝访问私网地址。若 Kubernetes API 或 Helm 仓库位于私网，应将其精确网段加入 `PANEL_ALLOWED_PRIVATE_CIDRS`，不要无条件放开全部内网。

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

MVP 使用单节点文件存储，适合单副本部署。高可用、多租户 RBAC、OIDC、资源 YAML 编辑、终端和日志流式查看属于后续版本范围。
