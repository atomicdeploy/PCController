import { useEffect, useRef, useState } from 'react'
import { CircleStop, Play, RefreshCw } from 'lucide-react'
import { execute } from './api'
import { shellArgument } from './command-line'
import { Button, TextField } from './components'
import type { Locale } from './types'

interface InputState {
  running: boolean; protocol?: string; listen?: string; serial?: string; sender?: string
  received?: number; applied?: number; coalesced?: number; rejected?: number; last_error?: string
}

const protocols = [
  ['openrgb', 'OpenRGB / Keyboard Visualizer', 65506], ['ddp', 'DDP', 4048],
  ['artnet', 'Art-Net · ArtDmx', 6454], ['sacn', 'sACN / E1.31', 5568],
  ['wled', 'WLED · DRGB / DNRGB / WARLS', 21324], ['hyperion', 'Hyperion · raw RGB', 19446],
  ['tpm2net', 'TPM2.NET', 65506], ['adalight', 'Adalight · Adafruit', 65506],
  ['adalight-openrgb', 'Adalight · OpenRGB', 65506], ['tpm2', 'TPM2', 65506],
] as const
const serialProtocols = new Set(['openrgb', 'adalight', 'adalight-openrgb', 'tpm2'])

export function RGBInput({ locale, online, revision }: { locale: Locale; online: boolean; revision: number }) {
  const copy = (en: string, fa: string) => locale === 'fa' ? fa : en
  const [state, setState] = useState<InputState | null>(null)
  const [protocol, setProtocol] = useState('openrgb')
  const [pixels, setPixels] = useState('60')
  const [fps, setFPS] = useState('60')
  const [transport, setTransport] = useState('udp')
  const [listen, setListen] = useState('127.0.0.1:65506')
  const [source, setSource] = useState('')
  const [serial, setSerial] = useState('')
  const [baud, setBaud] = useState('115200')
  const [universe, setUniverse] = useState('1')
  const [channel, setChannel] = useState('1')
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')
  const [refresh, setRefresh] = useState(0)
  const generation = useRef(0)
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const dmx = protocol === 'artnet' || protocol === 'sacn'
  const valid = /^\d+$/.test(pixels) && Number(pixels) >= 1 && Number(pixels) <= 100 &&
    /^\d+$/.test(fps) && Number(fps) >= 1 && Number(fps) <= 120 &&
    (transport === 'serial' ? serial.trim().length > 0 && /^\d+$/.test(baud) && Number(baud) >= 1200 && Number(baud) <= 2000000 && serialProtocols.has(protocol) : !!listen.trim()) &&
    (!dmx || /^\d+$/.test(universe) && Number(universe) >= (protocol === 'sacn' ? 1 : 0) && Number(universe) <= (protocol === 'sacn' ? 63999 : 32767) && /^\d+$/.test(channel) && Number(channel) >= 1 && Number(channel) + Number(pixels) * 3 - 1 <= 512)
  useEffect(() => {
    if (pending) return
    const current = ++generation.current
    const abort = new AbortController()
    void execute('rgb-input status', abort.signal).then(result => {
      if (!abort.signal.aborted && current === generation.current) {
        if (!result.output) throw new Error('RGB input status is missing')
        const next = JSON.parse(result.output) as InputState
        if (typeof next.running !== 'boolean') throw new Error('Invalid RGB input status')
        setState(next)
      }
    }).catch(cause => { if (!abort.signal.aborted) setError(String(cause)) })
    return () => { abort.abort(); generation.current++ }
  }, [revision, refresh, pending, online])
  const change = async () => {
    if (pending || !state) return
    setPending(true); setError('')
    try {
      const route = transport === 'serial' ? ` --serial ${shellArgument(serial.trim())} --baud ${Number(baud)}` : ` --listen ${shellArgument(listen.trim())}${source.trim() ? ` --source ${shellArgument(source.trim())}` : ''}`
      await execute(state.running ? 'rgb-input stop' : `rgb-input start ${protocol} --pixels ${Number(pixels)} --fps ${Number(fps)}${route}${dmx ? ` --universe ${Number(universe)} --channel ${Number(channel)}` : ''}`)
    } catch (cause) { if (mounted.current) setError(String(cause)) }
    finally { if (mounted.current) { setPending(false); setRefresh(value => value + 1) } }
  }
  return <section aria-label={copy('External RGB input', 'ورودی RGB خارجی')}>
    <h3>{copy('External RGB input', 'ورودی RGB خارجی')}</h3>
    {!state?.running && <div className="advanced-fields">
      <label className="text-field">{copy('Transport', 'مسیر ارتباط')}
        <select value={transport} disabled={pending} onChange={event => { const next = event.target.value; setTransport(next); if (next === 'serial' && !serialProtocols.has(protocol)) setProtocol('adalight') }}>
          <option value="udp">UDP</option><option value="serial">{copy('Serial bridge', 'پل سریال')}</option>
        </select>
      </label>
      <label className="text-field">{copy('Protocol', 'پروتکل')}
        <select value={protocol} disabled={pending} onChange={event => {
          const next = event.target.value
          const oldPort = protocols.find(item => item[0] === protocol)?.[2]
          const newPort = protocols.find(item => item[0] === next)?.[2]
          if (listen === `127.0.0.1:${oldPort}`) setListen(`127.0.0.1:${newPort}`)
          setProtocol(next)
        }}>
          {protocols.filter(item => transport !== 'serial' || serialProtocols.has(item[0])).map(item => <option key={item[0]} value={item[0]}>{item[1]}</option>)}
        </select>
      </label>
      <TextField label={copy('LED count', 'تعداد LED')} type="number" min={1} max={100} value={pixels} disabled={pending} onChange={event => setPixels(event.target.value)} />
      <TextField label="FPS" type="number" min={1} max={120} value={fps} disabled={pending} onChange={event => setFPS(event.target.value)} />
      {transport === 'serial' ? <>
        <TextField label={copy('Virtual COM / PTY port', 'درگاه مجازی COM / PTY')} value={serial} disabled={pending} onChange={event => setSerial(event.target.value)} />
        <TextField label={copy('Baud rate', 'نرخ باود')} type="number" min={1200} max={2000000} value={baud} disabled={pending} onChange={event => setBaud(event.target.value)} />
      </> : <>
        <TextField label={copy('Listen endpoint', 'آدرس دریافت')} value={listen} disabled={pending} onChange={event => setListen(event.target.value)} />
        <TextField label={copy('Trusted sender IP (required for LAN)', 'IP فرستنده مجاز (برای شبکه الزامی)')} value={source} disabled={pending} onChange={event => setSource(event.target.value)} />
      </>}
      {dmx && <>
        <TextField label={copy('Universe', 'یونیورس')} type="number" min={protocol === 'sacn' ? 1 : 0} max={protocol === 'sacn' ? 63999 : 32767} value={universe} disabled={pending} onChange={event => setUniverse(event.target.value)} />
        <TextField label={copy('First DMX slot', 'اولین کانال DMX')} type="number" min={1} max={513 - Number(pixels) * 3} value={channel} disabled={pending} onChange={event => setChannel(event.target.value)} />
      </>}
    </div>}
    {state?.running && <div className="advanced-fields"><strong>{state.protocol}</strong><code>{state.serial || state.listen}</code>{state.sender && state.sender !== state.serial && <code>{state.sender}</code>}</div>}
    <div className="advanced-actions">
      <Button icon={state?.running ? CircleStop : Play} busy={pending} disabled={!state || pending || (!state.running && (!online || !valid))} onClick={() => void change()}>
        {state?.running ? copy('Stop RGB input', 'توقف ورودی RGB') : copy('Start RGB input', 'شروع ورودی RGB')}
      </Button>
      <Button compact icon={RefreshCw} disabled={pending} onClick={() => { setError(''); setRefresh(value => value + 1) }}>{copy('Refresh', 'تازه‌سازی')}</Button>
    </div>
    {!!state?.received && <output>{copy('Received', 'دریافت')}: {state.received} · {copy('Acknowledged', 'تأییدشده')}: {state.applied ?? 0} · {copy('Coalesced', 'ادغام‌شده')}: {state.coalesced ?? 0} · {copy('Rejected', 'ردشده')}: {state.rejected ?? 0}</output>}
    {(error || state?.last_error) && <p role="alert">{error || state?.last_error}</p>}
  </section>
}
