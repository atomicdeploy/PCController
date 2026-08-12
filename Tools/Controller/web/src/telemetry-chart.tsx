import { useMemo, useState } from 'react'
import {
  Area,
  AreaChart,
  Legend,
  Line,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { Segmented, StatusBadge } from './components'
import type { Locale, MetricSample } from './types'

type ChartMode = 'electrical' | 'power' | 'thermal'
type WindowSize = '30' | '60' | 'all'

interface TelemetryChartProps {
  connected: boolean
  locale: Locale
  samples: MetricSample[]
}

const modeLabels = {
  electrical: { en: 'Electrical', fa: 'الکتریکی' },
  power: { en: 'Power', fa: 'توان' },
  thermal: { en: 'Thermal', fa: 'دما' },
} as const

function valueLabel(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? value.toFixed(2) : String(value ?? '—')
}

export function TelemetryChart({ connected, locale, samples }: TelemetryChartProps) {
  const [mode, setMode] = useState<ChartMode>('electrical')
  const [windowSize, setWindowSize] = useState<WindowSize>('60')
  const persian = locale === 'fa'
  const visible = useMemo(() => {
    const count = windowSize === 'all' ? samples.length : Number(windowSize)
    const formatter = new Intl.DateTimeFormat(persian ? 'fa-IR' : 'en-US', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
    return samples.slice(-count).map((sample) => ({
      ...sample,
      timeLabel: formatter.format(sample.at),
    }))
  }, [persian, samples, windowSize])

  const availableModes = useMemo(() => {
    const modes: ChartMode[] = []
    if (samples.some((sample) => [sample.supply, sample.bus, sample.current].some((value) => typeof value === 'number' && Number.isFinite(value)))) modes.push('electrical')
    if (samples.some((sample) => typeof sample.power === 'number' && Number.isFinite(sample.power))) modes.push('power')
    if (samples.some((sample) => [sample.ledTemp, sample.btTemp].some((value) => typeof value === 'number' && Number.isFinite(value)))) modes.push('thermal')
    return modes
  }, [samples])
  const visibleMode = availableModes.includes(mode) ? mode : availableModes[0] ?? 'electrical'

  const latest = visible.at(-1)
  const chartLabel = persian
    ? `نمودار ${modeLabels[visibleMode].fa} با ${visible.length} نمونه`
    : `${modeLabels[visibleMode].en} chart with ${visible.length} samples`

  if (!visible.length) {
    return (
      <div className="telemetry-chart__empty" role="status">
        <strong>{persian ? 'هنوز نمونه‌ای دریافت نشده است' : 'No telemetry samples yet'}</strong>
        <span>{connected ? (persian ? 'در انتظار اولین وضعیت برد' : 'Waiting for the first board status') : (persian ? 'برد آفلاین است' : 'Controller offline')}</span>
      </div>
    )
  }

  return (
    <div className="telemetry-chart">
      <div className="telemetry-chart__toolbar">
        <Segmented
          value={visibleMode}
          label={persian ? 'گروه نمودار' : 'Chart group'}
          options={availableModes.map((value) => ({ value, label: modeLabels[value][persian ? 'fa' : 'en'] }))}
          onChange={setMode}
        />
        <Segmented
          value={windowSize}
          label={persian ? 'بازه نمونه‌ها' : 'Sample window'}
          options={[
            { value: '30', label: persian ? '۳۰ نمونه' : '30 samples' },
            { value: '60', label: persian ? '۶۰ نمونه' : '60 samples' },
            { value: 'all', label: persian ? 'همه' : 'All' },
          ]}
          onChange={setWindowSize}
        />
      </div>

      <div className="telemetry-chart__canvas" role="img" aria-label={chartLabel}>
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={visible} margin={{ top: 14, right: 18, bottom: 2, left: 4 }}>
            <defs>
              <linearGradient id="telemetry-accent-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="var(--accent)" stopOpacity={0.34} />
                <stop offset="1" stopColor="var(--accent)" stopOpacity={0.01} />
              </linearGradient>
              <linearGradient id="telemetry-amber-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stopColor="var(--amber)" stopOpacity={0.28} />
                <stop offset="1" stopColor="var(--amber)" stopOpacity={0.01} />
              </linearGradient>
            </defs>
            <XAxis dataKey="timeLabel" minTickGap={38} tick={{ fill: 'var(--text-muted)', fontSize: 9 }} axisLine={{ stroke: 'var(--line-strong)' }} tickLine={false} />
            <YAxis yAxisId="left" width={44} tick={{ fill: 'var(--text-muted)', fontSize: 9 }} axisLine={false} tickLine={false} domain={['auto', 'auto']} />
            {visibleMode === 'electrical' && <YAxis yAxisId="right" orientation="right" width={46} tick={{ fill: 'var(--text-muted)', fontSize: 9 }} axisLine={false} tickLine={false} domain={['auto', 'auto']} />}
            <Tooltip
              cursor={{ stroke: 'var(--line-strong)', strokeWidth: 1 }}
              contentStyle={{ background: 'var(--glass-strong)', border: '1px solid var(--line-strong)', borderRadius: 12, boxShadow: 'var(--shadow-tight)', color: 'var(--text)' }}
              labelStyle={{ color: 'var(--text-muted)', marginBottom: 6 }}
              formatter={(value, name) => [valueLabel(value), String(name)]}
            />
            <Legend iconType="plainline" wrapperStyle={{ color: 'var(--text-soft)', fontSize: 10, paddingTop: 7 }} />
            {visibleMode === 'electrical' && <>
              <Area yAxisId="left" type="monotone" dataKey="supply" name={persian ? 'تغذیه V' : 'Supply V'} stroke="var(--accent)" strokeWidth={2.2} fill="url(#telemetry-accent-fill)" isAnimationActive={false} />
              <Line yAxisId="left" type="monotone" dataKey="bus" name={persian ? 'باس V' : 'Bus V'} stroke="var(--violet)" strokeWidth={1.8} dot={false} isAnimationActive={false} />
              <Line yAxisId="right" type="monotone" dataKey="current" name={persian ? 'جریان mA' : 'Current mA'} stroke="var(--amber)" strokeWidth={1.8} dot={false} isAnimationActive={false} />
            </>}
            {visibleMode === 'power' && <Area yAxisId="left" type="monotone" dataKey="power" name={persian ? 'توان W' : 'Power W'} stroke="var(--amber)" strokeWidth={2.2} fill="url(#telemetry-amber-fill)" isAnimationActive={false} />}
            {visibleMode === 'thermal' && <>
              <Area yAxisId="left" type="monotone" dataKey="ledTemp" name={persian ? 'دمای LED °C' : 'LED °C'} stroke="var(--red)" strokeWidth={2.1} fill="url(#telemetry-amber-fill)" isAnimationActive={false} />
              <Line yAxisId="left" type="monotone" dataKey="btTemp" name={persian ? 'دمای صدا °C' : 'Audio °C'} stroke="var(--violet)" strokeWidth={1.9} dot={false} isAnimationActive={false} />
            </>}
          </AreaChart>
        </ResponsiveContainer>
      </div>

      <div className="telemetry-chart__summary" aria-live="polite">
        <StatusBadge tone={connected ? 'good' : 'warn'}>{connected ? (persian ? 'زنده' : 'Live') : (persian ? 'آخرین داده' : 'Last known')}</StatusBadge>
        <span>{persian ? `${visible.length} نمونه` : `${visible.length} samples`}</span>
        {latest && <span dir="ltr">{latest.timeLabel}</span>}
      </div>

      <table className="sr-only">
        <caption>{chartLabel}</caption>
        <thead><tr><th>{persian ? 'زمان' : 'Time'}</th><th>{persian ? 'تغذیه' : 'Supply'}</th><th>{persian ? 'جریان' : 'Current'}</th><th>{persian ? 'توان' : 'Power'}</th><th>{persian ? 'دما' : 'Temperature'}</th></tr></thead>
        <tbody>{visible.slice(-10).map((sample) => <tr key={sample.at}><td>{sample.timeLabel}</td><td>{sample.supply}</td><td>{sample.current}</td><td>{sample.power}</td><td>{sample.ledTemp}</td></tr>)}</tbody>
      </table>
    </div>
  )
}
