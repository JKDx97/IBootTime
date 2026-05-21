import { useState, useEffect, useRef } from 'react'
import { Power, Wifi, Server, HardDrive, ScrollText, Monitor, Layers, Terminal, Cpu, ScreenShare } from 'lucide-react'
import { StartServer, StopServer, GetServerStatus, IsServerRunning, GetConnectedClients, GetRecentLogs, GetBootProtocol, TriggerRemote } from '../../wailsjs/go/main/App'
import { BrowserOpenURL, EventsOn } from '../../wailsjs/runtime/runtime'
import clsx from 'clsx'

function StatusCard({ label, active, icon: Icon }) {
  return (
    <div className={clsx(
      'flex items-center gap-3 px-4 py-3 rounded-xl border transition-all duration-300',
      active
        ? 'bg-emerald-500/10 border-emerald-500/30'
        : 'bg-slate-800/50 border-slate-700'
    )}>
      <Icon size={20} className={active ? 'text-emerald-400' : 'text-slate-500'} />
      <div>
        <p className="text-xs text-slate-400">{label}</p>
        <p className={clsx('text-sm font-semibold', active ? 'text-emerald-400' : 'text-slate-500')}>
          {active ? 'Online' : 'Offline'}
        </p>
      </div>
      <div className={clsx(
        'ml-auto w-2.5 h-2.5 rounded-full transition-all duration-300',
        active ? 'bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.5)]' : 'bg-slate-600'
      )} />
    </div>
  )
}

function CircularProgress({ progress, size = 128, strokeWidth = 6 }) {
  const radius = (size - strokeWidth) / 2
  const circumference = 2 * Math.PI * radius
  const offset = circumference - (progress / 100) * circumference
  return (
    <svg width={size} height={size} className="transform -rotate-90">
      <circle cx={size/2} cy={size/2} r={radius}
        fill="none" stroke="#1e293b" strokeWidth={strokeWidth} />
      <circle cx={size/2} cy={size/2} r={radius}
        fill="none" stroke="url(#progressGrad)" strokeWidth={strokeWidth}
        strokeDasharray={circumference} strokeDashoffset={offset}
        strokeLinecap="round"
        className="transition-all duration-500" />
      <defs>
        <linearGradient id="progressGrad" x1="0%" y1="0%" x2="100%" y2="0%">
          <stop offset="0%" stopColor="#34d399" />
          <stop offset="100%" stopColor="#3b82f6" />
        </linearGradient>
      </defs>
    </svg>
  )
}

const protoInfo = {
  ipxe: { name: 'iPXE', icon: Layers, color: 'text-blue-400', bg: 'bg-blue-500/10 border-blue-500/30' },
  grub: { name: 'GRUB', icon: Terminal, color: 'text-amber-400', bg: 'bg-amber-500/10 border-amber-500/30' },
  undionly: { name: 'Undionly', icon: Cpu, color: 'text-purple-400', bg: 'bg-purple-500/10 border-purple-500/30' },
}


export default function Dashboard() {
  const [running, setRunning] = useState(false)
  const [status, setStatus] = useState({ dhcp: false, tftp: false, http: false, running: false, ip: '', bootProtocol: '' })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [clients, setClients] = useState([])
  const [logs, setLogs] = useState([])
  const [bootProtocol, setBootProtocolState] = useState('ipxe')
  const logRef = useRef(null)

  useEffect(() => {
    IsServerRunning().then(setRunning)
    GetServerStatus().then((s) => {
      setStatus(s)
      if (s.bootProtocol) setBootProtocolState(s.bootProtocol)
    })
    GetConnectedClients().then((c) => setClients(c || []))
    GetRecentLogs(100).then((l) => setLogs(l || []))
    GetBootProtocol().then(setBootProtocolState)

    const unsub1 = EventsOn('server:status-changed', (s) => {
      setStatus(s)
      setRunning(!!s.running)
      if (s.bootProtocol) setBootProtocolState(s.bootProtocol)
    })

    const unsub2 = EventsOn('client:updated', () => {
      GetConnectedClients().then((c) => setClients(c || []))
    })

    const unsub3 = EventsOn('server:log', (entry) => {
      setLogs((prev) => [...prev.slice(-199), entry])
    })

    return () => { unsub1(); unsub2(); unsub3() }
  }, [])

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight
    }
  }, [logs])

  const handleToggle = async () => {
    setLoading(true)
    setError('')
    try {
      if (running) {
        await StopServer()
      } else {
        await StartServer()
      }
    } catch (e) {
      setError(String(e))
    }
    setLoading(false)
  }

  const levelColor = (level) => {
    switch (level) {
      case 'error': return 'text-red-400'
      case 'warn': return 'text-amber-400'
      case 'info': return 'text-blue-400'
      default: return 'text-slate-500'
    }
  }

  const proto = protoInfo[bootProtocol] || protoInfo.ipxe
  const ProtoIcon = proto.icon

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-bold text-white">Dashboard</h2>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
        <div className="lg:col-span-1 flex flex-col items-center justify-center bg-slate-900 rounded-2xl border border-slate-700 p-8">
          {/* Power button — always visible */}
          <button
            onClick={handleToggle}
            disabled={loading}
            className={clsx(
              'w-32 h-32 rounded-full flex items-center justify-center transition-all duration-300 border-4',
              loading && 'opacity-50 cursor-wait',
              running
                ? 'bg-red-500/20 border-red-500 hover:bg-red-500/30 shadow-[0_0_30px_rgba(239,68,68,0.3)]'
                : 'bg-emerald-500/20 border-emerald-500 hover:bg-emerald-500/30 shadow-[0_0_30px_rgba(16,185,129,0.3)]'
            )}
          >
            <Power size={48} className={running ? 'text-red-400' : 'text-emerald-400'} />
          </button>
          <p className={clsx('mt-4 text-lg font-bold', running ? 'text-red-400' : 'text-emerald-400')}>
            {loading ? 'Procesando...' : running ? 'Servidor Iniciado' : 'Iniciar Servidor'}
          </p>
          {running && (
            <p className="text-xs text-slate-500 mt-0.5">Presiona para detener</p>
          )}
          {/* Background ISO prep banner */}
          {running && status.startupPhase === 'preparing' && (
            <div className="mt-3 w-full max-w-xs">
              <div className="flex items-center gap-2 text-xs text-blue-400 mb-1">
                <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" />
                <span className="truncate">{status.startupDetail || 'Preparando ISOs...'}</span>
                <span className="ml-auto shrink-0">{status.startupProgress || 0}%</span>
              </div>
              <div className="w-full bg-slate-700 rounded-full h-1.5 overflow-hidden">
                <div
                  className="h-full bg-blue-500 rounded-full transition-all duration-500"
                  style={{ width: `${status.startupProgress || 0}%` }}
                />
              </div>
            </div>
          )}
          {status.ip && (
            <p className="text-sm text-slate-400 mt-2">
              IP: <span className="text-white font-mono">{status.ip}</span>
            </p>
          )}

          {/* Boot Protocol & Network Mode badges */}
          <div className="flex items-center gap-2 mt-3">
            <span className={clsx(
              'flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border',
              proto.bg
            )}>
              <ProtoIcon size={12} className={proto.color} />
              <span className={proto.color}>{proto.name}</span>
            </span>
            <span className="flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border bg-emerald-500/10 border-emerald-500/30">
              <Wifi size={12} className="text-emerald-400" />
              <span className="text-emerald-400">Proxy PXE</span>
            </span>
          </div>

          {error && <p className="text-sm text-red-400 mt-2 max-w-xs text-center">{error}</p>}
        </div>

        <div className="lg:col-span-2 space-y-4">
          <div className="grid grid-cols-3 gap-3">
            <StatusCard label="DHCP Server" active={status.dhcp} icon={Wifi} />
            <StatusCard label="TFTP Server" active={status.tftp} icon={Server} />
            <StatusCard label="HTTP Server" active={status.http} icon={HardDrive} />
          </div>

          <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
            <div className="flex items-center gap-2 mb-2">
              <Monitor size={16} className="text-slate-400" />
              <h3 className="text-sm font-semibold text-white">Clientes Activos</h3>
              <span className="ml-auto text-xs bg-slate-700 px-2 py-0.5 rounded-full text-slate-300">
                {clients.length}
              </span>
            </div>
            {clients.length === 0 ? (
              <p className="text-xs text-slate-500 py-2">No hay clientes conectados</p>
            ) : (
              <div className="space-y-1.5 max-h-40 overflow-auto">
                {clients.map((c) => (
                  <div key={c.mac} className="flex items-center text-xs bg-slate-800 rounded-lg px-3 py-1.5 gap-2">
                    <span className="font-mono text-slate-400 w-36">{c.mac}</span>
                    <span className="text-white w-36" title={c.ip}>{c.hostname || c.ip}</span>
                    <span className="text-blue-400 flex-1">{c.isoName || c.state}</span>
                    {c.progress > 0 && (
                      <span className="text-emerald-400 shrink-0">{c.progress.toFixed(1)}%</span>
                    )}
                    {c.remoteAvailable ? (
                      <button
                        onClick={async () => {
                          try {
                            setError('')
                            await TriggerRemote(c.ip)
                            const url = `http://${status.ip}:${status.httpPort || 8080}/novnc?host=${c.ip}&port=${c.remoteVncPort || 5900}&password=${c.remotePassword || ''}&mode=reverse`
                            BrowserOpenURL(url)
                          } catch (e) {
                            setError(`VNC trigger failed: ${e}`)
                          }
                        }}
                        className="shrink-0 flex items-center gap-1 px-2 py-0.5 rounded-md bg-emerald-500/20 border border-emerald-500/40 text-emerald-400 hover:bg-emerald-500/30 transition-colors"
                        title="Enviar orden de conexion VNC al cliente"
                      >
                        <ScreenShare size={12} />
                        <span>Conectar</span>
                      </button>
                    ) : (
                      <span className="shrink-0 text-slate-600 flex items-center gap-1">
                        <ScreenShare size={12} />
                        <span>Sin VNC</span>
                      </span>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      </div>

      <div className="bg-slate-900 rounded-xl border border-slate-700 p-4">
        <div className="flex items-center gap-2 mb-3">
          <ScrollText size={16} className="text-slate-400" />
          <h3 className="text-sm font-semibold text-white">Log del Servidor</h3>
          <span className="ml-auto text-xs text-slate-500">{logs.length} entradas</span>
        </div>
        <div
          ref={logRef}
          className="bg-slate-950 rounded-lg p-3 h-52 overflow-auto font-mono text-xs leading-relaxed"
        >
          {logs.length === 0 ? (
            <p className="text-slate-600">Esperando eventos...</p>
          ) : (
            logs.map((log, i) => (
              <div key={i} className="flex gap-2">
                <span className="text-slate-600 shrink-0">
                  {new Date(log.timestamp).toLocaleTimeString()}
                </span>
                <span className={clsx('shrink-0 uppercase w-12', levelColor(log.level))}>
                  [{log.level}]
                </span>
                <span className="text-slate-500 shrink-0">[{log.source}]</span>
                <span className="text-slate-300">{log.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
