import { type ChangeEvent, type DragEvent, type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertTriangle,
  ArchiveRestore,
  ArrowDownToLine,
  Binary,
  CheckCircle2,
  CloudDownload,
  Cpu,
  Download,
  FileArchive,
  FileCode2,
  FileUp,
  Gauge,
  HardDriveDownload,
  History,
  LoaderCircle,
  PackageCheck,
  RefreshCw,
  RotateCcw,
  Server,
  ShieldCheck,
  UploadCloud,
  Usb,
} from 'lucide-react'
import { Button, Card, DataRow, EmptyState, SectionTitle, StatusBadge, TextField } from './components'
import { formatClock } from './i18n'
import { ReleaseDiscovery } from './release-discovery'
import type { SharedViewProps } from './views'
import {
  captureDeviceArtifacts,
  compareBuildIdentity,
  downloadArtifact,
  fetchRemoteArtifact,
  getArtifactManifest,
  getUpdateStatus,
  listBridgePeers,
  listArtifacts,
  sha256File,
  startEEPROMUpdate,
  startFlashRestore,
  startFirmwareUpdate,
  startHostUpdate,
  startPeerHostUpdate,
  uploadArtifact,
  type ArtifactDescriptor,
  type ArtifactKind,
  type ArtifactManifest,
  type ArtifactOperationResult,
  type BridgePeer,
  type UpdateStatus,
} from './updates-api'

const artifactKinds: ArtifactKind[] = ['firmware', 'eeprom', 'flash-backup', 'host-executable']

function formatBytes(value: number | undefined): string {
  if (!Number.isFinite(value) || !value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB']
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)))
  return `${(value / 1024 ** exponent).toFixed(exponent === 0 ? 0 : value >= 10 * 1024 ** exponent ? 1 : 2)} ${units[exponent]}`
}

function shortHash(value: string | undefined): string {
  return value ? value.slice(0, 12).toUpperCase() : '—'
}

function operationTone(state: UpdateStatus['state'] | undefined): 'neutral' | 'info' | 'good' | 'warn' | 'bad' {
  if (!state) return 'neutral'
  if (state === 'completed' || state === 'downloaded') return 'good'
  if (state === 'failed') return 'bad'
  if (state === 'verifying') return 'warn'
  return 'info'
}

const activeUpdateStates = new Set<UpdateStatus['state']>([
  'queued',
  'downloading',
  'reading',
  'backing-up',
  'programming',
  'staging',
  'verifying',
])

/** Progress is meaningful only while an operation can still advance. */
export function activeUpdateProgress(status: UpdateStatus | null | undefined): number | null {
  if (!status || !activeUpdateStates.has(status.state)) return null
  return Math.max(0, Math.min(100, status.progress_percent))
}

function artifactLabel(kind: ArtifactKind, locale: SharedViewProps['locale']): string {
  const labels: Record<ArtifactKind, [string, string]> = {
    firmware: ['Board firmware', 'میان‌افزار برد'],
    eeprom: ['Board EEPROM', 'EEPROM برد'],
    'flash-backup': ['Flash readback', 'بازخوانی فلش'],
    'host-executable': ['Host executable', 'فایل اجرایی میزبان'],
  }
  return labels[kind]?.[locale === 'fa' ? 1 : 0] ?? kind
}

function operationFrom(result: ArtifactOperationResult, setStatus: (status: UpdateStatus) => void): ArtifactDescriptor | undefined {
  setStatus(result.operation)
  return result.artifact
}

export function artifactUpdateAvailable(connected: boolean, kind: ArtifactKind): boolean {
  return kind === 'host-executable' || connected
}

export function UpdatesView({ appTitle, snapshot, events, locale, openDialog }: SharedViewProps) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const [manifest, setManifest] = useState<ArtifactManifest | null>(null)
  const [artifacts, setArtifacts] = useState<ArtifactDescriptor[]>([])
  const [selectedSHA, setSelectedSHA] = useState('')
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [peers, setPeers] = useState<BridgePeer[]>([])
  const [busy, setBusy] = useState('')
  const [notice, setNotice] = useState('')
  const [serviceError, setServiceError] = useState('')
  const [uploadKind, setUploadKind] = useState<ArtifactKind>('firmware')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [uploadDigest, setUploadDigest] = useState('')
  const [uploadHashing, setUploadHashing] = useState(false)
  const [uploadDragActive, setUploadDragActive] = useState(false)
  const [remoteURL, setRemoteURL] = useState('')
  const [remoteName, setRemoteName] = useState('')
  const [remoteDigest, setRemoteDigest] = useState('')
  const [remoteBearer, setRemoteBearer] = useState('')
  const fileInput = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setBusy((current) => current || 'refresh')
    try {
      const [nextManifest, nextList, nextPeers] = await Promise.all([getArtifactManifest(), listArtifacts(), listBridgePeers()])
      setManifest(nextManifest)
      setArtifacts(nextList.artifacts)
      setStatus(nextManifest.update ?? null)
      setPeers(nextPeers)
      setServiceError('')
      setSelectedSHA((current) => current && nextList.artifacts.some((item) => item.sha256 === current)
        ? current
        : nextManifest.defaults.firmware?.sha256 ?? nextList.artifacts[0]?.sha256 ?? '')
    } catch (cause) {
      setManifest(null)
      setArtifacts([])
      setPeers([])
      setServiceError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy((current) => current === 'refresh' ? '' : current)
    }
  }, [])

  useEffect(() => { void load() }, [load])

  const lastUpdateEvent = useMemo(
    () => events.find((event) => event.kind.startsWith('update.')),
    [events],
  )
  useEffect(() => {
    if (!lastUpdateEvent || !manifest?.enabled) return
    void getUpdateStatus().then(setStatus).then(() => load()).catch(() => undefined)
  }, [lastUpdateEvent?.id, lastUpdateEvent?.time]) // eslint-disable-line react-hooks/exhaustive-deps

  const selected = artifacts.find((item) => item.sha256 === selectedSHA)
  const currentForSelected = selected?.kind === 'host-executable'
    ? manifest?.current.host
    : selected?.kind === 'eeprom' ? manifest?.current.eeprom
      : selected?.kind === 'flash-backup' ? manifest?.current.flash_readback : manifest?.current.firmware
  const comparison = compareBuildIdentity(currentForSelected, selected)
  const bootloaderUnavailable = status?.isp_fallback_suggested === true
  const activeProgress = activeUpdateProgress(status)
  const running = activeProgress !== null

  const acceptUploadFile = async (file: File | null) => {
    setUploadFile(file)
    setUploadDigest('')
    setNotice('')
    if (!file) return
    setUploadHashing(true)
    try { setUploadDigest(await sha256File(file)) }
    catch (cause) { setNotice(`${copy('Could not hash selection', 'محاسبهٔ هش فایل انتخابی ممکن نشد')}: ${cause instanceof Error ? cause.message : String(cause)}`) }
    finally { setUploadHashing(false) }
  }

  const selectFile = (event: ChangeEvent<HTMLInputElement>) => {
    void acceptUploadFile(event.currentTarget.files?.[0] ?? null)
  }

  const dropFile = (event: DragEvent<HTMLButtonElement>) => {
    event.preventDefault()
    setUploadDragActive(false)
    void acceptUploadFile(event.dataTransfer.files?.[0] ?? null)
  }

  const stageUpload = async () => {
    if (!uploadFile || !uploadDigest) return
    setBusy('upload')
    try {
      const result = await uploadArtifact(uploadFile, uploadKind, uploadDigest)
      const staged = operationFrom(result, setStatus)
      setNotice(copy(`${uploadFile.name} was verified and staged. Nothing has been programmed.`, `${uploadFile.name} تأیید و آماده شد. هیچ چیزی پروگرام نشده است.`))
      await load()
      if (staged) setSelectedSHA(staged.sha256)
    } catch (cause) { setNotice(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const stageRemote = async (event: FormEvent) => {
    event.preventDefault()
    const digest = remoteDigest.trim().toLowerCase()
    if (digest && !/^[0-9a-f]{64}$/.test(digest)) {
      setNotice(copy('Expected SHA-256 must contain exactly 64 hexadecimal characters.', 'مقدار مورد انتظار SHA-256 باید دقیقاً ۶۴ نویسهٔ شانزده‌شانزدهی داشته باشد.'))
      return
    }
    setBusy('remote')
    try {
      const result = await fetchRemoteArtifact({
        url: remoteURL.trim(), kind: uploadKind,
        ...(remoteName.trim() ? { name: remoteName.trim() } : {}),
        ...(digest ? { sha256: digest } : {}),
        ...(remoteBearer ? { bearer_token: remoteBearer } : {}),
      })
      const staged = operationFrom(result, setStatus)
      setNotice(copy('Remote content was downloaded, checked and staged. Nothing has been programmed.', 'محتوای راه‌دور دانلود، بررسی و آماده شد. هیچ چیزی پروگرام نشده است.'))
      setRemoteBearer('')
      await load()
      if (staged) setSelectedSHA(staged.sha256)
    } catch (cause) { setNotice(cause instanceof Error ? cause.message : String(cause)) }
    finally { setBusy('') }
  }

  const confirmUpdate = (artifact: ArtifactDescriptor, method: 'urclock' | 'usbasp' = 'urclock') => {
    const boardTarget = artifact.kind !== 'host-executable'
    if (!artifactUpdateAvailable(snapshot.connected, artifact.kind)) {
      setNotice(copy('Connect and authenticate the controller before starting a board update.', 'پیش از آغاز به‌روزرسانی برد، کنترلر را متصل و احراز هویت کنید.'))
      return
    }
    const actionName = artifact.kind === 'host-executable' ? copy('replace the host application', 'برنامهٔ میزبان را جایگزین می‌کند')
      : artifact.kind === 'eeprom' ? copy('restore MCU EEPROM', 'EEPROM ریزکنترل‌گر را بازیابی می‌کند')
        : artifact.kind === 'flash-backup' ? copy('restore the captured flash image', 'تصویر ثبت‌شدهٔ فلش را بازیابی می‌کند') : copy('program board firmware', 'میان‌افزار برد را پروگرام می‌کند')
    openDialog({
      tone: 'danger',
      title: artifact.kind === 'flash-backup' ? copy('Authorize guarded flash restore?', 'بازیابی محافظت‌شدهٔ فلش مجاز شود؟') : copy(`Authorize ${artifactLabel(artifact.kind, locale).toLowerCase()} update?`, `به‌روزرسانی ${artifactLabel(artifact.kind, locale)} مجاز شود؟`),
      body: `${artifact.name}\nSHA-256 ${artifact.sha256}\n\n${copy('This is the authorization step. Selection and staging did not write hardware.', 'این مرحلهٔ صدور مجوز است. انتخاب و آماده‌سازی چیزی روی سخت‌افزار ننوشته‌اند.')} ${boardTarget ? copy(`The primary host will make the board safe, back up flash and EEPROM, then ${actionName}, verify readback, and restore lifecycle settings.`, `میزبان اصلی ابتدا برد را در وضعیت امن قرار می‌دهد، از فلش و EEPROM پشتیبان می‌گیرد، سپس ${actionName}، بازخوانی را تأیید و تنظیمات چرخهٔ کار را بازیابی می‌کند.`) : copy(`The primary host will verify the package, prepare a recoverable replacement, and ${actionName} only after the current process exits cleanly.`, `میزبان اصلی بسته را تأیید و جایگزینی قابل‌بازیابی آماده می‌کند و فقط پس از خروج پاک فرایند فعلی، ${actionName}.`)}`,
      confirmLabel: method === 'usbasp' ? copy('Authorize ISP programming', 'اجازهٔ پروگرام ISP') : copy('Authorize verified update', 'اجازهٔ به‌روزرسانی تأییدشده'),
      action: async () => {
        setBusy('update')
        try {
          const request = { artifact_sha256: artifact.sha256, authorized: true as const, method, ...(snapshot.port.name ? { port: snapshot.port.name } : {}) }
          const result = artifact.kind === 'host-executable'
            ? await startHostUpdate(request)
            : artifact.kind === 'eeprom' ? await startEEPROMUpdate(request)
              : artifact.kind === 'flash-backup' ? await startFlashRestore(request) : await startFirmwareUpdate(request)
          operationFrom(result, setStatus)
          setNotice(copy(`${artifact.name} entered the guarded ${artifact.kind === 'flash-backup' ? 'restore' : 'update'} queue.`, `${artifact.name} وارد صف محافظت‌شدهٔ ${artifact.kind === 'flash-backup' ? 'بازیابی' : 'به‌روزرسانی'} شد.`))
        } catch (cause) {
          setNotice(cause instanceof Error ? cause.message : String(cause))
          throw cause
        } finally { setBusy('') }
      },
    })
  }

  const confirmPeerUpdate = (peer: BridgePeer, artifact: ArtifactDescriptor) => {
    if (!peer.connected || !peer.allow_commands) {
      setNotice(copy(`Peer ${peer.name} is not connected for commands.`, `همتای ${peer.name} برای فرمان‌ها متصل نیست.`))
      return
    }
    openDialog({
      tone: 'danger',
      title: copy(`Upgrade ${peer.name}?`, `به‌روزرسانی ${peer.name}؟`),
      body: `${artifact.name}\nSHA-256 ${artifact.sha256}\n\n${copy('The primary instance will transfer the verified executable over its authenticated bridge. The remote coordinator will validate it again, gracefully close its surfaces, replace itself, and roll back if health acknowledgement fails.', 'نمونهٔ اصلی فایل اجرایی تأییدشده را از پل معتبر منتقل می‌کند. هماهنگ‌کنندهٔ راه‌دور دوباره آن را بررسی، رابط‌های خود را با نرمی می‌بندد، خود را جایگزین و در صورت شکست سلامت عقب‌گرد می‌کند.')}`,
      confirmLabel: copy('Authorize peer upgrade', 'اجازهٔ به‌روزرسانی همتا'),
      action: async () => {
        setBusy('peer-update')
        try {
          const result = await startPeerHostUpdate(peer.name, artifact.sha256)
          setStatus(result.operation)
          setNotice(copy(`${peer.name} accepted remote host update ${result.operation.id}.`, `${peer.name} به‌روزرسانی راه‌دور ${result.operation.id} را پذیرفت.`))
        } catch (cause) { setNotice(cause instanceof Error ? cause.message : String(cause)) }
        finally { setBusy('') }
      },
    })
  }

  const confirmCapture = (method: 'urclock' | 'usbasp' = 'urclock') => {
    if (!snapshot.connected) {
      setNotice(copy('Connect and authenticate the controller before capturing its memories.', 'پیش از ثبت حافظه‌ها، کنترلر را متصل و احراز هویت کنید.'))
      return
    }
    openDialog({
    title: copy('Capture current flash and EEPROM?', 'فلش و EEPROM فعلی ثبت شوند؟'),
    body: `${copy('The primary host will briefly take ownership of the programming path, read both memories, verify their exact byte counts, and store content-addressed backups. Existing identical images are not duplicated.', 'میزبان اصلی برای مدتی کوتاه مسیر پروگرام را در اختیار می‌گیرد، هر دو حافظه را می‌خواند، شمار دقیق بایت‌ها را تأیید و پشتیبان‌های محتوامحور ذخیره می‌کند. تصاویر یکسان موجود دوباره کپی نمی‌شوند.')}${method === 'usbasp' ? `\n\n${copy('ISP access is being used because the bootloader probe failed.', 'به‌دلیل ناموفق بودن بررسی بوت‌لودر، از دسترسی ISP استفاده می‌شود.')}` : ''}`,
    confirmLabel: method === 'usbasp' ? copy('Authorize ISP capture', 'اجازهٔ ثبت با ISP') : copy('Authorize readback', 'اجازهٔ بازخوانی'),
    action: async () => {
      setBusy('capture')
      try {
        const result = await captureDeviceArtifacts({ components: ['flash', 'eeprom'], authorized: true, method })
        operationFrom(result, setStatus)
        setNotice(copy('Flash and EEPROM capture entered the guarded queue.', 'ثبت فلش و EEPROM وارد صف محافظت‌شده شد.'))
      } catch (cause) {
        setNotice(cause instanceof Error ? cause.message : String(cause))
        throw cause
      } finally { setBusy('') }
    },
    })
  }

  if (serviceError) return (
    <>
      <SectionTitle eyebrow={copy('Verified host updates', 'به‌روزرسانی تأییدشدهٔ میزبان')} title={copy('Firmware & updates', 'میان‌افزار و به‌روزرسانی')} detail={copy('Remote update is performed by the primary host over the board programming link; it is not MCU-native OTA.', 'به‌روزرسانی راه‌دور توسط میزبان اصلی و از مسیر پروگرام برد انجام می‌شود؛ این فرایند OTA بومی ریزکنترل‌گر نیست.')} />
      <Card icon={Server} iconTone="red" title={copy('Artifact service', 'سرویس خروجی‌های ساخت')} eyebrow={copy('Unavailable', 'دردسترس نیست')} className="updates-unavailable">
        <EmptyState icon={Server} title={copy('Artifact service unavailable', 'سرویس خروجی‌های ساخت در دسترس نیست')} detail={serviceError} />
        <Button icon={RefreshCw} onClick={() => void load()}>{copy('Retry typed service', 'تلاش دوباره برای سرویس')}</Button>
      </Card>
    </>
  )

  return (
    <>
      <SectionTitle
        eyebrow={copy('Verified host updates', 'به‌روزرسانی تأییدشدهٔ میزبان')}
        title={copy('Firmware & updates', 'میان‌افزار و به‌روزرسانی')}
        detail={copy(`The primary ${appTitle} instance stages, hashes, backs up, programs and verifies. A browser or remote peer never writes the MCU directly.`, `نمونهٔ اصلی ${appTitle} فایل را آماده و هش می‌کند، پشتیبان می‌گیرد، پروگرام و تأیید می‌کند. مرورگر یا همتای راه‌دور هرگز مستقیم روی ریزکنترل‌گر نمی‌نویسد.`)}
        action={<div className="header-actions"><StatusBadge tone={manifest?.enabled ? 'good' : 'warn'}>{manifest?.enabled ? copy('SERVICE READY', 'سرویس آماده') : copy('DISABLED', 'غیرفعال')}</StatusBadge><Button compact icon={RefreshCw} busy={busy === 'refresh'} onClick={() => void load()}>{copy('Refresh', 'تازه‌سازی')}</Button></div>}
      />

      <section className="updates-hero">
        <div className="updates-hero__identity"><ShieldCheck /><div><span>{copy('Update permissions', 'مجوزهای به‌روزرسانی')}</span><strong>{copy('Primary host · explicit grant', 'میزبان اصلی · مجوز صریح')}</strong><p>{copy('Download and staging are safe preparation. Programming, EEPROM restore, ISP use, and self-replacement each require a separate confirmation.', 'دانلود و آماده‌سازی، مراحل مقدماتی امن هستند. پروگرام، بازیابی EEPROM، استفاده از ISP و جایگزینی خود برنامه هرکدام تأییدی جداگانه می‌خواهند.')}</p></div></div>
        {status && <div className="updates-hero__state">
          <StatusBadge tone={operationTone(status.state)} pulse={running}>{status.state.toUpperCase()}</StatusBadge>
          {activeProgress !== null && <strong>{activeProgress.toFixed(0)}%</strong>}
          {status.detail && <span>{status.detail}</span>}
        </div>}
        {activeProgress !== null && <div className="update-progress" role="progressbar" aria-label={copy('Update progress', 'پیشرفت به‌روزرسانی')} aria-valuemin={0} aria-valuemax={100} aria-valuenow={activeProgress}><i style={{ width: `${activeProgress}%` }} /></div>}
        <div className="updates-hero__metrics">
          <DataRow label={copy('Transfer', 'انتقال')} value={status?.bytes_total ? `${formatBytes(status.bytes_done)} / ${formatBytes(status.bytes_total)}` : '—'} mono />
          <DataRow label={copy('Artifact', 'خروجی ساخت')} value={shortHash(status?.artifact_sha256)} mono />
          <DataRow label={copy('Updated', 'آخرین تغییر')} value={formatClock(locale, status?.updated_at)} />
          <DataRow label={copy('Programming path', 'مسیر پروگرام')} value={status?.programming_method || copy('none', 'هیچ‌کدام')} mono />
          <DataRow label={copy('Bootloader outcome', 'نتیجهٔ بوت‌لودر')} value={status?.bootloader_outcome || copy('not attempted', 'تلاش نشده')} tone={bootloaderUnavailable ? 'bad' : status?.bootloader_outcome === 'succeeded' ? 'good' : undefined} />
        </div>
      </section>

      {notice && <div className={`update-notice${/failed|error|could not|expected/i.test(notice) ? ' is-error' : ''}`}><CheckCircle2 size={17} /><span>{notice}</span></div>}

      <section className="updates-grid">
        <Card icon={FileUp} iconTone="violet" title={copy('Stage a local artifact', 'آماده‌سازی خروجی محلی')} eyebrow={copy('Select · hash · stage', 'انتخاب · هش · آماده‌سازی')} className="update-source-card" action={<StatusBadge tone={uploadFile && uploadDigest ? 'good' : 'neutral'}>{uploadFile ? uploadHashing ? copy('HASHING', 'در حال هش') : copy('SELECTED', 'انتخاب‌شده') : copy('NO FILE', 'بدون فایل')}</StatusBadge>}>
          <div className="artifact-kind-picker" role="radiogroup" aria-label={copy('Artifact kind', 'نوع خروجی')}>
            {artifactKinds.map((kind) => <button key={kind} role="radio" aria-checked={uploadKind === kind} className={uploadKind === kind ? 'is-active' : ''} onClick={() => setUploadKind(kind)}>{kind === 'firmware' ? <FileCode2 /> : kind === 'eeprom' ? <Binary /> : kind === 'host-executable' ? <Cpu /> : <FileArchive />}<span>{artifactLabel(kind, locale)}</span></button>)}
          </div>
          <input ref={fileInput} className="visually-hidden" type="file" accept=".hex,.eep,.bin,.exe,application/octet-stream,text/plain" onChange={selectFile} />
          <button
            className={`artifact-drop${uploadDragActive ? ' is-dragging' : ''}`}
            type="button"
            onClick={() => fileInput.current?.click()}
            onDragEnter={(event) => { event.preventDefault(); setUploadDragActive(true) }}
            onDragOver={(event) => { event.preventDefault(); event.dataTransfer.dropEffect = 'copy' }}
            onDragLeave={() => setUploadDragActive(false)}
            onDrop={dropFile}
          >
            {uploadHashing ? <LoaderCircle className="spin" /> : <FileUp />}
            <div><strong>{uploadFile?.name || copy('Browse for firmware, EEPROM, or readback', 'انتخاب میان‌افزار، EEPROM یا بازخوانی')}</strong><span>{uploadFile ? `${formatBytes(uploadFile.size)} · SHA-256 ${shortHash(uploadDigest)}` : copy('The browser calculates SHA-256 locally before the host receives anything.', 'مرورگر پیش از دریافت هر چیزی توسط میزبان، SHA-256 را به‌صورت محلی محاسبه می‌کند.')}</span></div>
            <span>{copy('Choose file', 'انتخاب فایل')}</span>
          </button>
          <Button tone="primary" icon={UploadCloud} busy={busy === 'upload'} disabled={!uploadFile || !uploadDigest || uploadHashing} onClick={() => void stageUpload()}>{copy('Verify and stage only', 'فقط تأیید و آماده‌سازی')}</Button>
        </Card>

        <Card icon={CloudDownload} iconTone="accent" title={copy('Stage from a remote source', 'آماده‌سازی از منبع راه‌دور')} eyebrow={copy('HTTPS or trusted peer', 'HTTPS یا همتای مورد اعتماد')} className="update-source-card">
          <form className="remote-artifact-form" onSubmit={(event) => void stageRemote(event)}>
            <TextField label={copy('Source URL', 'نشانی منبع')} type="url" dir="ltr" value={remoteURL} placeholder="https://host.example/artifacts/firmware/…" onChange={(event) => setRemoteURL(event.target.value)} hint={copy('The host respects configured proxy environment variables, validates redirects, size, and digest, then stages without programming.', 'میزبان تنظیمات پراکسی محیط را رعایت می‌کند، تغییرمسیرها، اندازه و چکیده را می‌سنجد و سپس بدون پروگرام فایل را آماده می‌کند.')} />
            <div className="compact-fields"><TextField label={copy('Display name (optional)', 'نام نمایشی (اختیاری)')} value={remoteName} onChange={(event) => setRemoteName(event.target.value)} /><TextField label={copy('Expected SHA-256 (recommended)', 'SHA-256 مورد انتظار (پیشنهادی)')} dir="ltr" value={remoteDigest} maxLength={64} onChange={(event) => setRemoteDigest(event.target.value.replace(/[^0-9a-f]/gi, '').toLowerCase())} /></div>
            <TextField label={copy('Peer bearer token (optional)', 'توکن حامل همتا (اختیاری)')} type="password" autoComplete="off" dir="ltr" value={remoteBearer} onChange={(event) => setRemoteBearer(event.target.value)} hint={copy('Used once by the primary host for this authenticated peer download; it is not persisted or printed.', 'فقط یک‌بار توسط میزبان اصلی برای دانلود معتبر از همتا استفاده می‌شود و ذخیره یا چاپ نخواهد شد.')} />
            <Button type="submit" tone="primary" icon={CloudDownload} busy={busy === 'remote'} disabled={!remoteURL.trim()}>{copy('Download, verify and stage', 'دانلود، تأیید و آماده‌سازی')}</Button>
          </form>
        </Card>

        <ReleaseDiscovery
          manifest={manifest}
          events={events}
          locale={locale}
          openDialog={openDialog}
          onArtifactsChanged={() => void load()}
        />

        <Card icon={Server} iconTone="accent" title={copy('Update an authenticated peer', 'به‌روزرسانی همتای معتبر')} eyebrow={copy('Bridge-native · verified · coordinated', 'پل بومی · تأییدشده · هماهنگ')} action={<StatusBadge tone={peers.some((peer) => peer.connected) ? 'good' : 'warn'}>{peers.filter((peer) => peer.connected).length} {copy('CONNECTED', 'متصل')}</StatusBadge>}>
          <p className="card-copy">{copy('Transfer the selected verified host executable over the existing authenticated bridge, then ask that peer coordinator to replace itself gracefully. No SSH command or shared filesystem is used.', 'فایل اجرایی تأییدشدهٔ میزبان را از همان پل معتبر منتقل می‌کند و سپس از هماهنگ‌کنندهٔ همتا می‌خواهد خود را به‌شکل امن جایگزین کند. هیچ فرمان SSH یا فایل مشترکی استفاده نمی‌شود.')}</p>
          {peers.length ? <div className="data-list">{peers.map((peer) => <div key={peer.name}><DataRow label={peer.name} value={peer.connected ? copy('Connected', 'متصل') : peer.last_error || copy('Disconnected', 'قطع')} tone={peer.connected ? 'good' : 'bad'} /><div className="inline-actions"><Button icon={RotateCcw} tone="primary" disabled={!peer.connected || !peer.allow_commands || selected?.kind !== 'host-executable' || busy === 'peer-update'} busy={busy === 'peer-update'} onClick={() => selected && void confirmPeerUpdate(peer, selected)}>{copy('Review peer host update', 'بازبینی به‌روزرسانی میزبان همتا')}</Button></div></div>)}</div> : <EmptyState icon={Server} title={copy('No bridge peers configured', 'هیچ همتای پلی پیکربندی نشده')} detail={copy('Add an authenticated peer in integration settings; connected peers appear here immediately.', 'یک همتای معتبر را در تنظیمات یکپارچه‌سازی بیفزایید؛ همتایان متصل فوراً اینجا ظاهر می‌شوند.')} />}
        </Card>

        <Card icon={ArchiveRestore} iconTone="green" title={copy('Embedded first-board recovery', 'بازیابی تعبیه‌شدهٔ نخستین برد')} eyebrow={copy('Verified build defaults', 'پیش‌فرض‌های تأییدشدهٔ ساخت')} action={<StatusBadge tone={manifest?.defaults_enabled ? 'good' : 'warn'}>{manifest?.defaults_enabled ? copy('AVAILABLE', 'موجود') : copy('NOT PACKAGED', 'بسته‌بندی نشده')}</StatusBadge>}>
          <p className="card-copy">{copy('Enabled automatically only when the release contains both a real firmware image and a generated valid default EEPROM image. It is never auto-flashed—even a non-responsive, corrupt, or older board still requires your grant.', 'فقط وقتی خودکار فعال می‌شود که انتشار هم تصویر واقعی میان‌افزار و هم تصویر معتبرِ تولیدشده برای EEPROM پیش‌فرض داشته باشد. هرگز خودکار فلش نمی‌شود؛ حتی برد پاسخ‌گو‌نبود‌ه، خراب یا قدیمی نیز به اجازهٔ شما نیاز دارد.')}</p>
          <div className="data-list"><DataRow label={copy('Firmware', 'میان‌افزار')} value={manifest?.defaults.firmware ? `${shortHash(manifest.defaults.firmware.sha256)} · ${formatBytes(manifest.defaults.firmware.bytes)}` : copy('Missing from this host', 'در این میزبان موجود نیست')} mono /><DataRow label={copy('Default EEPROM', 'EEPROM پیش‌فرض')} value={manifest?.defaults.eeprom ? `${shortHash(manifest.defaults.eeprom.sha256)} · ${formatBytes(manifest.defaults.eeprom.bytes)}` : copy('Missing; empty .eep is never accepted', 'موجود نیست؛ فایل خالی .eep پذیرفته نمی‌شود')} mono /></div>
          {snapshot.connected && <div className="inline-actions"><Button icon={Cpu} tone="primary" disabled={!manifest?.defaults.firmware || busy === 'update'} onClick={() => manifest?.defaults.firmware && confirmUpdate(manifest.defaults.firmware)}>{copy('Review firmware programming', 'بازبینی پروگرام میان‌افزار')}</Button><Button icon={ArchiveRestore} disabled={!manifest?.defaults.eeprom || busy === 'update'} onClick={() => manifest?.defaults.eeprom && confirmUpdate(manifest.defaults.eeprom)}>{copy('Review EEPROM restore', 'بازبینی بازیابی EEPROM')}</Button></div>}
        </Card>

        {snapshot.connected && <Card icon={HardDriveDownload} iconTone="amber" title={copy('Capture this connected board', 'ثبت برد متصل')} eyebrow={copy('Flash + EEPROM · deduplicated', 'فلش + EEPROM · بدون تکرار')} action={<StatusBadge tone="good">{snapshot.port.name || copy('CONNECTED', 'متصل')}</StatusBadge>}>
          <p className="card-copy">{copy('A readback is stored by content hash. Identical firmware is referenced, not copied, and the resulting flash/EEPROM artifacts can be served to authenticated peers or downloaded below.', 'بازخوانی بر پایهٔ هش محتوا ذخیره می‌شود. به میان‌افزار یکسان ارجاع داده می‌شود و دوباره کپی نمی‌گردد؛ خروجی فلش/EEPROM را می‌توان در اختیار همتاهای معتبر گذاشت یا از پایین دانلود کرد.')}</p>
          <div className="data-list"><DataRow label={copy('Current board', 'برد فعلی')} value={snapshot.hello.name || appTitle} /><DataRow label={copy('Running build', 'ساخت در حال اجرا')} value={snapshot.hello.build_hash ? snapshot.hello.build_hash.toString(16).toUpperCase().padStart(8, '0') : '—'} mono /><DataRow label={copy('Port owner', 'مالک درگاه')} value={snapshot.port.name || copy('automatic', 'خودکار')} mono /></div>
          <Button icon={HardDriveDownload} busy={busy === 'capture'} onClick={() => confirmCapture()}>{copy('Review flash + EEPROM capture', 'بازبینی ثبت فلش + EEPROM')}</Button>
        </Card>}

        {snapshot.connected && bootloaderUnavailable && (
          <Card icon={AlertTriangle} iconTone="red" title={copy('Bootloader probe failed', 'بررسی بوت‌لودر ناموفق بود')} eyebrow={copy('ISP fallback recommended', 'مسیر جایگزین ISP پیشنهاد می‌شود')} className="isp-fallback-card" action={<StatusBadge tone="bad">{copy('NO BOOTLOADER ROUTE', 'بدون مسیر بوت‌لودر')}</StatusBadge>}>
            <div className="isp-guidance"><AlertTriangle /><p>{copy('The host could not enter Urboot/Urclock. Connect a supported ISP programmer; the same primary host will back up what it can, program through ISP, verify the image, and restore the application lifecycle. ISP was not suggested before this failed probe.', 'میزبان نتوانست وارد Urboot/Urclock شود. یک پروگرامر ISP پشتیبانی‌شده متصل کنید؛ همان میزبان اصلی از بخش‌های ممکن پشتیبان می‌گیرد، از طریق ISP پروگرام می‌کند، تصویر را تأیید و چرخهٔ کار برنامه را بازیابی می‌کند. پیش از این بررسی ناموفق، ISP پیشنهاد نشده بود.')}</p></div>
            <div className="inline-actions"><Button tone="danger" icon={Usb} disabled={!selected || selected.kind === 'host-executable'} onClick={() => selected && confirmUpdate(selected, 'usbasp')}>{copy('Review ISP programming', 'بازبینی پروگرام ISP')}</Button><Button icon={History} onClick={() => confirmCapture('usbasp')}>{copy('Review ISP capture', 'بازبینی ثبت با ISP')}</Button></div>
          </Card>
        )}

        <Card icon={FileArchive} iconTone="violet" title={copy('Verified artifact inventory', 'فهرست خروجی‌های تأییدشده')} eyebrow={copy(`${artifacts.length} content-addressed artifacts`, `${artifacts.length} خروجی محتوامحور`)} className="artifact-inventory" action={<StatusBadge tone="info">{artifacts.length} {copy('UNIQUE', 'یکتا')}</StatusBadge>}>
          {artifacts.length ? (
            <div className="artifact-table-wrap"><table className="artifact-table"><thead><tr><th aria-label={copy('Select', 'انتخاب')} /><th>{copy('Artifact', 'خروجی')}</th><th>{copy('Source', 'منبع')}</th><th>{copy('Build / digest', 'ساخت / چکیده')}</th><th>{copy('Size', 'اندازه')}</th><th>{copy('Created', 'ایجاد')}</th><th>{copy('Actions', 'عملیات')}</th></tr></thead><tbody>{artifacts.map((artifact) => {
              const active = selectedSHA === artifact.sha256
              const provenance = artifact.metadata
              const providerDetail = provenance?.repository || (provenance?.workflow_run_id ? `${copy('run', 'اجرا')} ${provenance.workflow_run_id}` : provenance?.release_id ? `${copy('release', 'انتشار')} ${provenance.release_id}` : '')
              return <tr key={`${artifact.kind}-${artifact.sha256}`} className={active ? 'is-selected' : ''}><td><input type="radio" name="artifact" checked={active} aria-label={copy(`Select ${artifact.name}`, `انتخاب ${artifact.name}`)} onChange={() => setSelectedSHA(artifact.sha256)} /></td><td><strong>{artifact.name}</strong><span>{artifactLabel(artifact.kind, locale)}{artifact.embedded ? copy(' · embedded', ' · تعبیه‌شده') : ''}{artifact.current ? copy(' · current', ' · فعلی') : ''}</span></td><td><strong>{provenance?.provider || artifact.source}</strong>{providerDetail && <span>{providerDetail}</span>}</td><td className="mono"><strong>{artifact.build_hash || shortHash(artifact.sha256)}</strong><span>{artifact.build_timestamp || artifact.sha256}</span></td><td className="mono">{formatBytes(artifact.bytes)}</td><td>{formatClock(locale, artifact.created_at)}</td><td><Button compact icon={Download} onClick={() => void downloadArtifact(artifact).catch((cause) => setNotice(cause instanceof Error ? cause.message : String(cause)))}>{copy('Download', 'دانلود')}</Button></td></tr>
            })}</tbody></table></div>
          ) : <EmptyState icon={FileArchive} title={copy('No verified artifacts yet', 'هنوز خروجی تأییدشده‌ای وجود ندارد')} detail={copy('Stage a local or remote image, or capture the connected board.', 'یک تصویر محلی یا راه‌دور آماده کنید یا از برد متصل نسخه بگیرید.')} />}
          {selected && <div className="artifact-selection"><div><span>{copy('SELECTED', 'انتخاب‌شده')} / {artifactLabel(selected.kind, locale).toUpperCase()}</span><strong>{selected.name}</strong><code>{selected.sha256}</code></div><StatusBadge tone={comparison === 'same' ? 'good' : comparison === 'newer' ? 'info' : comparison === 'older' ? 'warn' : 'neutral'}>{comparison === 'same' ? copy('SAME', 'یکسان') : comparison === 'newer' ? copy('NEWER', 'جدیدتر') : comparison === 'older' ? copy('OLDER', 'قدیمی‌تر') : copy('UNKNOWN', 'نامشخص')}</StatusBadge>{artifactUpdateAvailable(snapshot.connected, selected.kind) && <Button icon={selected.kind === 'host-executable' ? RotateCcw : selected.kind === 'flash-backup' ? ArchiveRestore : Gauge} tone="primary" disabled={busy === 'update'} onClick={() => confirmUpdate(selected)}>{selected.kind === 'host-executable' ? copy('Review host self-update', 'بازبینی به‌روزرسانی میزبان') : selected.kind === 'eeprom' ? copy('Review EEPROM restore', 'بازبینی بازیابی EEPROM') : selected.kind === 'flash-backup' ? copy('Review flash restore', 'بازبینی بازیابی فلش') : copy('Review board programming', 'بازبینی پروگرام برد')}</Button>}</div>}
        </Card>

        <Card icon={Server} iconTone="accent" title={copy('Distribution paths', 'مسیرهای توزیع')} eyebrow={copy('Serve · download · bridge', 'ارائه · دانلود · پل')} className="update-distribution-card">
          <div className="distribution-grid"><article><Server /><strong>{copy('Authenticated serving', 'ارائهٔ معتبر')}</strong><p>{copy('Current flash, EEPROM, firmware, host packages and backups are served by exact SHA-256 through HTTP and bridge RPC.', 'فلش فعلی، EEPROM، میان‌افزار، بسته‌های میزبان و پشتیبان‌ها با SHA-256 دقیق از طریق HTTP و RPC پل ارائه می‌شوند.')}</p></article><article><ArrowDownToLine /><strong>{copy('Remote synchronization', 'همگام‌سازی راه‌دور')}</strong><p>{copy('Secondary instances request staging and updates through the primary owner; they never race it for the serial port.', 'نمونه‌های ثانویه آماده‌سازی و به‌روزرسانی را از مالک اصلی درخواست می‌کنند و هرگز برای درگاه سریال با آن رقابت ندارند.')}</p></article><article><PackageCheck /><strong>{copy('Verified replacement', 'جایگزینی تأییدشده')}</strong><p>{copy('Size, digest, packed date/time and build hash comparisons are visible before any update is authorized.', 'مقایسهٔ اندازه، چکیده، تاریخ/زمان فشرده و هش ساخت پیش از صدور مجوز هر به‌روزرسانی دیده می‌شود.')}</p></article></div>
        </Card>
      </section>
    </>
  )
}
