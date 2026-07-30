# K8s Panel

K8s Panel 是一个独立实现的 Kubernetes 与 Helm 管理面板。后端使用 Go，前端使用 React 和 TypeScript；Kubernetes 访问层基于标准 REST API，Helm 操作使用官方 Helm Go SDK。

## MVP 功能

- 单管理员登录、HttpOnly 会话 Cookie、登录失败限流
- 多集群配置、连接测试、命名空间权限能力检测、启停和删除确认，支持验证后原子轮换访问凭据
- 集群概览、节点/命名空间资源清单、节点诊断详情、CRD 与证书签名请求元数据/按需状态详情、聚合 API 健康清单，以及准入 Webhook/CEL 校验策略与绑定检查
- Deployment/StatefulSet/DaemonSet/Job/CronJob/Pod 工作负载查询
- Service/Ingress/EndpointSlice/NetworkPolicy 网络清单、ConfigMap/Secret 最小配置摘要，以及 PVC/PV/StorageClass 与集群级 CSIDriver 存储摘要
- 命名空间 ResourceQuota、LimitRange、HPA、PodDisruptionBudget 治理清单，以及集群级 PriorityClass 与 RuntimeClass 元数据和按需配置详情
- Pod Security Admission 命名空间安全态势，区分显式级别、版本固定、继承集群默认值和无效标签组合
- 节点 Kubelet 与已观测 API Server 的版本偏差态势，标记政策范围、升级阻塞、超限和主版本不一致
- 当前 API Server 实例报告的废弃 API 请求证据，按 API、资源和计划移除版本展示
- 当前连接端点经验证的 TLS 叶证书有效期证据，区分有效、临近到期、紧急和已到期状态
- 集群级 PodDisruptionBudget 中断证据，区分允许中断、当前受阻、未匹配 Pod 和控制器待同步状态
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

Kubernetes 单类资源清单最多读取 5,000 项、20 页和 32 MiB 原始数据，Web 表格每页只渲染 100 行；工作负载同时查询多种类型时共享这一总预算并固定串行读取，指定类型时只请求对应 API。资源治理必须限定到单个命名空间，一次只读取 ResourceQuota、LimitRange、HPA 或 PodDisruptionBudget 中的一类；单次最多 4 页、1,000 个对象、4 MiB 原始数据和 4,096 条投影资源、约束或状态条件，ResourceQuota 另限制 1,024 个 Scope。HPA 只返回目标、副本与指标计数，不查询 Metrics API；PodDisruptionBudget 只返回选择器计数和可用性状态，不返回标签值或 disruptedPods。Pod Security Admission 态势仅按需读取 Namespace PartialObjectMetadata，每次最多 4 页、1,000 个命名空间、4 MiB 和每对象 256 个标签；只投影六个标准 PSA 标签的有限状态，不回显其他标签或非法值，不读取 Pod、API Server 默认配置和静态豁免。节点版本偏差态势仅在用户切换视图后读取一次 `/version` 和 Kubernetes Table 格式的 Node 清单；最多读取 4 页、1,000 个节点、每页 1 MiB 和总计 2 MiB，不回退完整 Node 对象、不轮询，也不据此宣称整个集群已可升级。废弃 API 请求证据仅在用户切换视图后占用一个 Kubernetes 读取槽，并从当前 API Server 实例读取一次 `/metrics`；响应上限为 8 MiB、单行 64 KiB、最多投影 512 条目标指标，只保留 group、version、resource、subresource 和 removed_release，不轮询、不缓存，也不读取 Prometheus、审计日志、客户端或对象身份，因此不代表整个高可用集群的完整使用情况。EndpointSlice 和 NetworkPolicy 清单沿用 4 页、1,000 个对象和 4 MiB 上限，并分别把单次端点/端口或 selector/规则/peer/port 处理量限制为 16,384 项；EndpointSlice 响应只返回 Service 归属、地址族和条件计数，不返回端点地址、节点、目标对象或端口详情，NetworkPolicy 响应只包含选择范围和规则计数，不返回标签、CIDR 或端口内容，也不推断有效连通性或 CNI 执行状态。CRD 清单仅使用 PartialObjectMetadata，最多读取 8 页、2,000 项和 16 MiB；完整 CRD 只在用户选择单个对象后读取，响应上限为 2 MiB，且不会返回 OpenAPI Schema、转换 Webhook 配置或扫描自定义资源实例。聚合 API 清单只在用户切换到对应视图后读取 APIService 元数据，单次最多 4 页、1,000 个对象、4 MiB 和 4,096 条状态条件；响应不包含 CA Bundle、标签、注解或条件消息，也不会探测聚合 API、Service、Pod、网络和证书。准入 Webhook、ValidatingAdmissionPolicy 与 Binding 清单同样只在对应视图激活时读取元数据，每类最多 4 页、1,000 项和 4 MiB，单个详情最多 2 MiB；响应只保留失败/匹配策略、参数类型、执行动作和有界计数，不返回 CA、URL、规则内容、selector、参数名称、CEL 或类型检查正文，也不回退 beta API 或执行 Discovery。事件中心不自动轮询，默认在 API Server 侧过滤 Warning，单次最多串行读取 8 页、2,000 项和 16 MiB 原始事件并返回最近 200 条，用户可请求的返回上限为 500。配置与存储清单使用 Kubernetes Table 内容协商，只接收最小摘要且不回退读取完整对象；Secret 必须限定到单个命名空间，PV 卷源、CSI 标识、挂载选项和 StorageClass 参数不会进入面板响应。访问控制清单一次只读取一种资源并使用元数据内容协商，命名空间资源不允许全集群读取；清单最多 8 页、2,000 项和 16 MiB，单对象详情最多 2 MiB，规则和主体分别最多展示 128 项，ServiceAccount 详情不返回 Secret 名称。ServiceAccount 权限模拟每次只提交一个 64 KiB 上限的 SubjectAccessReview，不扫描 RBAC 图谱、不轮询、不缓存结果。超过后端上限会停止读取并返回错误，避免大集群拖垮控制面。

集群级中断预算证据只在用户切换视图后占用一个 Kubernetes 读取槽，并串行读取一次 `policy/v1` PDB 清单；最多 4 页、1,000 个对象、单页 2 MiB、总计 4 MiB 和 4,096 条投影条件。该视图不读取 Pod、Node 或工作负载，不执行 Eviction 或排空模拟；“当前受阻”仅表示已同步且匹配 Pod 的 PDB 当前不允许健康 Pod 自愿中断，不代表节点一定无法排空。

TLS 证书证据只在用户切换视图后占用一个 Kubernetes 读取槽，复用一次 `/version` 请求已经完成验证的 TLS 握手状态，并将最多 64 KiB 的响应体流式丢弃。面板只返回 UTC 有效期、剩余秒数和固定状态，不返回 Subject、Issuer、SAN、Serial、指纹或证书内容；该证据可能来自负载均衡器或反向代理，仅代表当前连接命中的 TLS 终止端点，不扫描宿主机 PKI、其他控制面实例或执行证书续期。

CertificateSigningRequest 清单只在用户切换视图后读取 PartialObjectMetadata，最多 4 页、1,000 个对象和 4 MiB；单对象详情只在用户选择后读取且最多 2 MiB。响应仅保留请求者、签名器、请求有效期、用途和有界状态条件，不返回或解析 PKCS#10 请求、已签发证书、UID、用户组、额外身份属性、标签、注解或条件消息，也不提供审批、拒绝和签发操作。

PriorityClass 清单只在用户切换到集群级治理视图后读取 PartialObjectMetadata，同时停止不再需要的命名空间清单读取；最多 4 页、1,000 个对象和 4 MiB。单对象详情最多 1 MiB，只返回整数优先级、全局默认标记和规范化后的抢占策略，不返回 description、标签、注解或 managedFields，不扫描 Pod、工作负载或调度队列，也不模拟实际抢占结果。

RuntimeClass 清单同样只在用户切换视图后读取 PartialObjectMetadata，最多 4 页、1,000 个对象和 4 MiB；单对象详情最多 1 MiB。响应只返回运行时 handler、CPU/内存 Pod overhead 字符串及资源项、节点选择器和容忍数量，不返回扩展资源名称、标签键值、容忍规则或对象元数据，不读取 Pod、Node、CRI 配置或宿主机运行时 socket，也不验证 handler 是否实际可用。

CSIDriver 清单只在用户切换到对应集群级存储视图后读取 PartialObjectMetadata，同时停止只供 PVC 使用的命名空间清单读取；最多 4 页、1,000 个对象和 4 MiB。单对象详情最多 1 MiB，只返回稳定 CSI 配置和 TokenRequest 数量，不返回 audience、有效期或对象元数据，不扫描 PV/PVC、VolumeAttachment、CSINode、CSIStorageCapacity、Pod、Node 或宿主机 CSI socket，也不验证驱动是否实际部署或健康。

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
