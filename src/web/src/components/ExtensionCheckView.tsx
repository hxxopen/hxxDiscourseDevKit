import type { ExtensionCheckReport, ExtensionFindingStatus } from '../api/types'
import { Badge } from './ui'
import { formatDate } from '../lib/format'

const findingLabels: Record<ExtensionFindingStatus, string> = {
  ok: '正常',
  warning: '提醒',
  error: '错误',
  skipped: '跳过',
}

const findingTones: Record<
  ExtensionFindingStatus,
  'emerald' | 'amber' | 'rose' | 'slate'
> = {
  ok: 'emerald',
  warning: 'amber',
  error: 'rose',
  skipped: 'slate',
}

export function ExtensionCheckView({ report }: { report: ExtensionCheckReport }) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Badge tone={report.ready ? 'emerald' : 'rose'}>
          {report.ready ? '检查通过' : '需要处理'}
        </Badge>
        <Badge tone={report.error_count > 0 ? 'rose' : 'slate'}>
          {report.error_count} 个错误
        </Badge>
        <Badge tone={report.warning_count > 0 ? 'amber' : 'slate'}>
          {report.warning_count} 个提醒
        </Badge>
        {report.kind === 'plugin' ? (
          <Badge tone={report.enabled ? 'emerald' : 'slate'}>
            {report.enabled ? '已启用' : '已禁用'}
          </Badge>
        ) : null}
      </div>

      <dl className="grid gap-2 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs sm:grid-cols-2">
        <MetaRow label="当前版本" value={report.discourse_version || '未识别'} />
        <MetaRow label="最低要求" value={report.required_version || '未声明'} />
        <MetaRow label="最高限制" value={report.maximum_version || '未声明'} />
        <MetaRow label="检查时间" value={formatDate(report.checked_at)} />
      </dl>

      <div className="space-y-2">
        {report.findings.map((finding) => (
          <div
            key={finding.id}
            className="rounded-xl border border-slate-800 bg-slate-950/40 px-3 py-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={findingTones[finding.status]}>
                {findingLabels[finding.status]}
              </Badge>
              <span className="text-sm font-medium text-slate-100">{finding.title}</span>
              {finding.required && finding.status === 'error' ? (
                <span className="text-[10px] font-semibold uppercase tracking-wider text-rose-300/80">
                  必需
                </span>
              ) : null}
            </div>
            <p className="mt-1 text-xs leading-5 text-slate-300">{finding.message}</p>
            {finding.fix ? (
              <p className="mt-1 text-[11px] leading-5 text-slate-500">
                处理：{finding.fix}
              </p>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  )
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 items-baseline gap-2">
      <dt className="w-14 shrink-0 text-slate-500">{label}</dt>
      <dd className="min-w-0 truncate text-slate-300" title={value}>
        {value}
      </dd>
    </div>
  )
}
