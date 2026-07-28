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
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const normalized = status.toLowerCase()
  return (
    <span className={`status-badge status-${normalized}`}>
      <span className="status-dot" aria-hidden="true" />
      {labels[normalized] ?? status}
    </span>
  )
}
