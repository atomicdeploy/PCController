import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Box, CheckCircle2, CloudDownload, GitBranch, LoaderCircle, PackageSearch, RefreshCw, ShieldCheck } from 'lucide-react'
import { Button, Card, DataRow, EmptyState, StatusBadge, TextField } from './components'
import type { SharedViewProps } from './views'
import {
  checkReleaseCandidate,
  currentBrowserPlatform,
  discoverManifest,
  discoverRelease,
  discoverWorkflow,
  getReleaseStageStatus,
  stageReleaseCandidate,
  type ReleaseCandidate,
  type ReleaseCheckResult,
  type ReleaseDiscoveryResult,
  type ReleaseStageStatus,
} from './release-discovery-api'
import type { ArtifactKind, ArtifactManifest } from './updates-api'

type DiscoverySource = 'release' | 'workflow' | 'manifest'

interface ReleaseDiscoveryProps extends Pick<SharedViewProps, 'events' | 'locale' | 'openDialog'> {
  manifest: ArtifactManifest | null
  onArtifactsChanged: () => void
}

const kinds: ArtifactKind[] = ['firmware', 'eeprom', 'flash-backup', 'host-executable']

function parsePackedTimestamp(value: string, invalidMessage: string): number | undefined {
  value = value.trim()
  if (!value) return undefined
  const parsed = Number.parseInt(value.replace(/^0x/i, ''), value.toLowerCase().startsWith('0x') ? 16 : 10)
  if (!Number.isSafeInteger(parsed) || parsed < 0 || parsed > 0xffffffff) throw new Error(invalidMessage)
  return parsed
}

function currentArtifact(manifest: ArtifactManifest | null, kind: ArtifactKind) {
  if (!manifest) return undefined
  if (kind === 'host-executable') return manifest.current.host
  if (kind === 'eeprom') return manifest.current.eeprom
  if (kind === 'flash-backup') return manifest.current.flash_readback
  return manifest.current.firmware
}

function shortHash(value: string | undefined, unpublished: string): string { return value ? value.slice(0, 12).toUpperCase() : unpublished }

function bytes(value: number | undefined, unknown: string): string {
  if (value === undefined) return unknown
  if (value === 0) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 ** 2) return `${(value / 1024).toFixed(1)} KiB`
  return `${(value / 1024 ** 2).toFixed(2)} MiB`
}

export function ReleaseDiscovery({ manifest, events, locale, openDialog, onArtifactsChanged }: ReleaseDiscoveryProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const kindLabel = (value: ArtifactKind) => ({
    firmware: copy('Board firmware', 'میان‌افزار برد'),
    eeprom: copy('Board EEPROM', 'EEPROM برد'),
    'flash-backup': copy('Flash backup', 'پشتیبان فلش'),
    'host-executable': copy('Host executable', 'فایل اجرایی میزبان'),
  })[value]
  const [source, setSource] = useState<DiscoverySource>('release')
  const [kind, setKind] = useState<ArtifactKind>('firmware')
  const [repository, setRepository] = useState('')
  const [branch, setBranch] = useState('main')
  const [workflow, setWorkflow] = useState('Build')
  const [tag, setTag] = useState('')
  const [manifestURL, setManifestURL] = useState('')
  const [apiBaseURL, setAPIBaseURL] = useState('')
  const [platform, setPlatform] = useState(() => currentBrowserPlatform())
  const [packedTimestamp, setPackedTimestamp] = useState('')
  const [bearerToken, setBearerToken] = useState('')
  const [archivePath, setArchivePath] = useState('')
  const [discovery, setDiscovery] = useState<ReleaseDiscoveryResult | null>(null)
  const [selectedID, setSelectedID] = useState('')
  const [comparison, setComparison] = useState<ReleaseCheckResult | null>(null)
  const [operation, setOperation] = useState<ReleaseStageStatus | null>(null)
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')

  const selected = discovery?.candidates.find((candidate) => candidate.id === selectedID)
  const latestDiscoveryEvent = useMemo(
    () => events.find((event) => event.kind.startsWith('artifact.discovery.')),
    [events],
  )

  const acceptStageStatus = (status: ReleaseStageStatus) => {
    setOperation(status)
    if (status.state === 'failed') {
      setNotice(status.error || status.detail || copy('Candidate staging failed.', 'آماده‌سازی گزینهٔ انتخابی ناموفق بود.'))
      return
    }
    if (status.state !== 'completed') return
    setBearerToken('')
    onArtifactsChanged()
  }

  useEffect(() => {
    if (!operation?.id || !latestDiscoveryEvent) return
    void getReleaseStageStatus(operation.id).then(acceptStageStatus).catch((cause) => {
      setNotice(cause instanceof Error ? cause.message : String(cause))
    })
  }, [latestDiscoveryEvent?.id, latestDiscoveryEvent?.time]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    setComparison(null)
    if (!selected) return
    void checkReleaseCandidate(currentArtifact(manifest, selected.kind), selected).then(setComparison).catch((cause) => {
      setNotice(cause instanceof Error ? cause.message : String(cause))
    })
  }, [selectedID, manifest]) // eslint-disable-line react-hooks/exhaustive-deps

  const runDiscovery = async (event: FormEvent) => {
    event.preventDefault()
    setBusy('discover')
    setNotice('')
    try {
      const packed = parsePackedTimestamp(packedTimestamp, copy('Packed timestamp must be a 32-bit decimal or 0x-prefixed hexadecimal value.', 'مهر زمانی فشرده باید یک عدد ده‌دهی ۳۲ بیتی یا مقدار شانزده‌شانزدهی با پیشوند 0x باشد.'))
      const auth = bearerToken ? { bearer_token: bearerToken } : {}
      const result = source === 'manifest'
        ? await discoverManifest(manifestURL.trim(), bearerToken)
        : source === 'workflow'
          ? await discoverWorkflow({
            repository: repository.trim(), branch: branch.trim(), workflow: workflow.trim(), kind,
            platform: kind === 'host-executable' ? platform.trim() : undefined,
            api_base_url: apiBaseURL.trim() || undefined, packed_timestamp: packed, ...auth,
          })
          : await discoverRelease({
            repository: repository.trim(), tag: tag.trim() || undefined, kind,
            platform: kind === 'host-executable' ? platform.trim() : undefined,
            api_base_url: apiBaseURL.trim() || undefined, packed_timestamp: packed, ...auth,
          })
      setDiscovery(result)
      setSelectedID(result.candidates[0]?.id ?? '')
      setNotice(copy(
        `${result.candidates.length} verified metadata candidate${result.candidates.length === 1 ? '' : 's'} discovered. No file was downloaded.`,
        `${result.candidates.length} گزینه با فرادادهٔ تأییدشده پیدا شد. هیچ فایلی دانلود نشده است.`,
      ))
    } catch (cause) {
      setNotice(cause instanceof Error ? cause.message : String(cause))
    } finally { setBusy('') }
  }

  const beginStage = (candidate: ReleaseCandidate) => openDialog({
    title: copy('Download and stage this candidate?', 'این گزینه دانلود و آماده شود؟'),
    body: `${candidate.name}\n${candidate.repository || candidate.url}\n${candidate.archive_sha256 || candidate.sha256 || copy('No provider digest; the content-addressed store will still calculate SHA-256.', 'ارائه‌دهنده چکیده‌ای نداده است؛ مخزن محتوامحور همچنان SHA-256 را محاسبه می‌کند.')}\n\n${copy('This downloads through the primary host, safely selects a ZIP member when needed, verifies all available size/digest metadata, and stages it. It does not open or program the board.', 'فایل از طریق میزبان اصلی دانلود می‌شود، در صورت نیاز عضو مناسب ZIP با ایمنی انتخاب می‌گردد، همهٔ فراداده‌های موجودِ اندازه و چکیده بررسی و سپس آماده می‌شود. این کار برد را باز یا پروگرام نمی‌کند.')}`,
    confirmLabel: copy('Download and stage only', 'فقط دانلود و آماده‌سازی'),
    action: async () => {
      setBusy('stage')
      try {
        const stagedCandidate = archivePath.trim() ? { ...candidate, archive_path: archivePath.trim() } : candidate
        const result = await stageReleaseCandidate(stagedCandidate, bearerToken)
        acceptStageStatus(result.operation)
        setNotice(copy('Verified download queued. Programming still requires a separate update authorization.', 'دانلود تأییدشده در صف قرار گرفت. پروگرام همچنان به مجوز جداگانهٔ به‌روزرسانی نیاز دارد.'))
      } catch (cause) {
        setNotice(cause instanceof Error ? cause.message : String(cause))
        throw cause
      } finally { setBusy('') }
    },
  })

  return (
    <Card icon={PackageSearch} iconTone="violet" title={copy('Release & workflow discovery', 'کشف انتشار و گردش‌کار')} eyebrow={`${source === 'release' ? copy('Release', 'انتشار') : source === 'workflow' ? copy('Workflow', 'گردش‌کار') : copy('Manifest', 'مانیفست')} · ${copy('proxy aware', 'سازگار با پراکسی')}`} className="release-discovery-card" action={<StatusBadge tone={operation?.state === 'failed' ? 'bad' : operation?.state === 'completed' ? 'good' : operation ? 'info' : 'neutral'}>{operation?.state?.toUpperCase() || copy('READY', 'آماده')}</StatusBadge>}>
      <p className="card-copy">{copy('Discover build artifacts without hardcoded product names. Provider metadata, release checksums, hashes, packed timestamps and platform are preserved into the content-addressed inventory.', 'خروجی‌های ساخت بدون نام محصولِ ثابت کشف می‌شوند. فرادادهٔ ارائه‌دهنده، چک‌سام انتشار، هش، مهر زمانی فشرده و پلتفرم در فهرست محتوامحور حفظ می‌شود.')}</p>
      <form className="release-discovery-form" onSubmit={(event) => void runDiscovery(event)}>
        <div className="discovery-source-picker">
          <button type="button" className={source === 'release' ? 'is-active' : ''} onClick={() => setSource('release')}><GitBranch />{copy('GitHub release', 'انتشار GitHub')}</button>
          <button type="button" className={source === 'workflow' ? 'is-active' : ''} onClick={() => setSource('workflow')}><GitBranch />{copy('Workflow artifact', 'خروجی گردش‌کار')}</button>
          <button type="button" className={source === 'manifest' ? 'is-active' : ''} onClick={() => setSource('manifest')}><PackageSearch />{copy('HTTP manifest', 'مانیفست HTTP')}</button>
        </div>
        {source === 'manifest'
          ? <TextField label={copy('Manifest URL', 'نشانی مانیفست')} type="url" dir="ltr" value={manifestURL} placeholder="https://host.example/update-manifest.json" onChange={(event) => setManifestURL(event.target.value)} />
          : <>
            <div className="compact-fields"><TextField label={copy('Repository', 'مخزن')} dir="ltr" value={repository} placeholder="owner/repository" onChange={(event) => setRepository(event.target.value)} />{source === 'workflow' ? <TextField label={copy('Branch', 'شاخه')} dir="ltr" value={branch} onChange={(event) => setBranch(event.target.value)} /> : <TextField label={copy('Tag (blank = latest stable)', 'برچسب (خالی = آخرین نسخهٔ پایدار)')} dir="ltr" value={tag} onChange={(event) => setTag(event.target.value)} />}</div>
            {source === 'workflow' && <TextField label={copy('Workflow name', 'نام گردش‌کار')} value={workflow} onChange={(event) => setWorkflow(event.target.value)} />}
            <TextField label={copy('GitHub API base (optional)', 'نشانی پایهٔ API گیت‌هاب (اختیاری)')} type="url" dir="ltr" value={apiBaseURL} placeholder="https://api.github.com" onChange={(event) => setAPIBaseURL(event.target.value)} hint={copy('Supports GitHub Enterprise; normal GitHub uses the default when blank.', 'از GitHub Enterprise پشتیبانی می‌کند؛ برای گیت‌هاب عادی، خالی بگذارید تا مقدار پیش‌فرض استفاده شود.')} />
          </>}
        <div className="compact-fields"><label className="field"><span>{copy('Artifact kind', 'نوع خروجی')}</span><select value={kind} onChange={(event) => setKind(event.target.value as ArtifactKind)}>{kinds.map((item) => <option key={item} value={item}>{kindLabel(item)}</option>)}</select></label><TextField label={copy('Target platform', 'پلتفرم مقصد')} dir="ltr" value={platform} disabled={kind !== 'host-executable'} onChange={(event) => setPlatform(event.target.value)} /></div>
        <div className="compact-fields"><TextField label={copy('Packed timestamp override', 'بازنویسی مهر زمانی فشرده')} dir="ltr" value={packedTimestamp} placeholder={copy('0x… or decimal', '0x… یا ده‌دهی')} onChange={(event) => setPackedTimestamp(event.target.value)} /><TextField label={copy('Provider / peer bearer token', 'توکن حامل ارائه‌دهنده / همتا')} type="password" autoComplete="off" dir="ltr" value={bearerToken} onChange={(event) => setBearerToken(event.target.value)} hint={copy('Held only in this browser session and cleared after a successful stage.', 'فقط در همین نشست مرورگر نگهداری و پس از آماده‌سازی موفق پاک می‌شود.')} /></div>
        <Button type="submit" tone="primary" icon={PackageSearch} busy={busy === 'discover'} disabled={source === 'manifest' ? !manifestURL.trim() : !repository.trim()}>{copy('Discover metadata', 'کشف فراداده')}</Button>
      </form>

      {notice && <div className={`update-notice${/failed|error|invalid|mismatch/i.test(notice) ? ' is-error' : ''}`}><CheckCircle2 size={17} /><span>{notice}</span></div>}

      {discovery?.candidates.length ? <div className="release-candidates">
        {discovery.candidates.map((candidate) => <label key={candidate.id} className={candidate.id === selectedID ? 'is-selected' : ''}><input type="radio" name="release-candidate" checked={candidate.id === selectedID} onChange={() => setSelectedID(candidate.id)} /><span><strong>{candidate.name}</strong><small>{candidate.release_tag ? `${copy('release', 'انتشار')} ${candidate.release_tag}` : candidate.workflow_run_id ? `${copy('run', 'اجرا')} ${candidate.workflow_run_id}` : candidate.source} · {candidate.platform || copy('platform-neutral', 'مستقل از پلتفرم')}</small></span><code>{shortHash(candidate.archive_sha256 || candidate.sha256, copy('UNPUBLISHED', 'منتشرنشده'))}</code><em>{bytes(candidate.archive_bytes || candidate.bytes, copy('unknown', 'نامشخص'))}</em></label>)}
      </div> : !busy && discovery && <EmptyState icon={Box} title={copy('No matching candidates', 'گزینهٔ منطبقی پیدا نشد')} detail={copy('Adjust artifact kind, platform, release tag, branch, or workflow name.', 'نوع خروجی، پلتفرم، برچسب انتشار، شاخه یا نام گردش‌کار را تغییر دهید.')} />}

      {selected && <div className="release-selection">
        <div className="data-list"><DataRow label={copy('Comparison', 'مقایسه')} value={comparison?.status || copy('checking', 'در حال بررسی')} tone={comparison?.status === 'newer' ? 'good' : comparison?.status === 'older' ? 'warn' : undefined} /><DataRow label={copy('Reason', 'دلیل')} value={comparison?.reason || copy('Comparing hashes and packed/build timestamps…', 'در حال مقایسهٔ هش و مهرهای زمانی فشرده/ساخت…')} /><DataRow label={copy('Build hash', 'هش ساخت')} value={selected.build_hash || copy('not supplied', 'ارائه نشده')} mono /><DataRow label={copy('Build time', 'زمان ساخت')} value={selected.build_timestamp || copy('not supplied', 'ارائه نشده')} mono /></div>
        {selected.archive && <TextField label={copy('ZIP member path (optional)', 'مسیر عضو ZIP (اختیاری)')} dir="ltr" value={archivePath} placeholder={copy('Auto-select a unique platform/kind match', 'انتخاب خودکار فایل یکتای منطبق با پلتفرم و نوع')} onChange={(event) => setArchivePath(event.target.value)} hint={copy('Specify this only when an archive contains multiple equally suitable files.', 'فقط زمانی مشخص کنید که آرشیو چند فایل به یک اندازه مناسب دارد.')} />}
        <Button tone="primary" icon={CloudDownload} busy={busy === 'stage'} onClick={() => beginStage(selected)}>{copy('Review verified staging', 'بازبینی آماده‌سازی تأییدشده')}</Button>
      </div>}

      {operation && <div className="release-operation"><div><LoaderCircle className={operation.state === 'queued' || operation.state === 'downloading' ? 'spin' : ''} /><strong>{operation.error || operation.detail || operation.state}</strong><span>{bytes(operation.bytes_done, copy('unknown', 'نامشخص'))} / {bytes(operation.bytes_total, copy('unknown', 'نامشخص'))}</span></div><div className="update-progress"><i style={{ width: `${Math.max(0, Math.min(100, operation.progress_percent))}%` }} /></div><Button compact icon={RefreshCw} onClick={() => void getReleaseStageStatus(operation.id).then(acceptStageStatus).catch((cause) => setNotice(cause instanceof Error ? cause.message : String(cause)))}>{copy('Refresh operation', 'تازه‌سازی عملیات')}</Button>{operation.artifact && <p><ShieldCheck /> {copy('Staged as', 'آماده‌شده با شناسه')} <code>{operation.artifact.sha256}</code>؛ {copy('board programming remains a separate action.', 'پروگرام برد همچنان عملی جداگانه است.')}</p>}</div>}
    </Card>
  )
}
