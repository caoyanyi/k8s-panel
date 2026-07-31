interface StatusBadgeProps {
  status: string
}

const labels: Record<string, string> = {
  pending: '待检测',
  connected: '已连接',
  degraded: '受限',
  unreachable: '不可达',
  disabled: '已停用',
  queued: '排队中',
  running: '执行中',
  succeeded: '成功',
  passed: '通过',
  failed: '失败',
  canceled: '已取消',
  unknown: '待确认',
  ready: '就绪',
  notready: '未就绪',
  progressing: '更新中',
  retrying: '重试中',
  scheduled: '已调度',
  suspended: '已暂停',
  unavailable: '不可用',
  active: '活跃',
  true: '正常',
  normal: '正常',
  constrained: '资源承压',
  critical: '资源紧张',
  deployed: '已部署',
  superseded: '已替代',
  uninstalled: '已卸载',
  uninstalling: '卸载中',
  'pending-install': '等待安装',
  'pending-upgrade': '等待升级',
  'pending-rollback': '等待回滚',
  'same-minor': '同一次版本',
  'within-policy': '政策范围内',
  'upgrade-blocking': '升级前需处理',
  'outside-policy': '超出偏差范围',
  'newer-than-server': '新于 API Server',
  'major-mismatch': '主版本不一致',
  available: '允许中断',
  blocked: '当前受阻',
  inactive: '未匹配 Pod',
  unobserved: '待同步',
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const normalized = status.toLowerCase()
  return (
    <span className={`status-badge status-${normalized}`}>
      <span className="status-dot" aria-hidden="true" />
      {statusLabel(status)}
    </span>
  )
}

export function statusLabel(status: string): string {
  return labels[status.toLowerCase()] ?? status
}
