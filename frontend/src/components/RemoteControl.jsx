import { useState, useEffect, useRef } from 'react'
import { ScreenShare, Monitor, Wifi, X, Maximize2, Minimize2, PlugZap } from 'lucide-react'
import { GetConnectedClients, GetServerStatus } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import clsx from 'clsx'

export default function RemoteControl() {
  const [clients, setClients] = useState([])
  const [serverStatus, setServerStatus] = useState(null)
  const [activeClient, setActiveClient] = useState(null)
  const [fullscreen, setFullscreen] = useState(false)
  const [manualIP, setManualIP] = useState('')
  const [manualPort, setManualPort] = useState('5900')
  const iframeRef = useRef(null)

  useEffect(() => {
    GetConnectedClients().then((c) => setClients(c || []))
    GetServerStatus().then((s) => setServerStatus(s))

    const unsub1 = EventsOn('client:updated', () => {
      GetConnectedClients().then((c) => setClients(c || []))
    })
    const unsub2 = EventsOn('server:status-changed', (s) => setServerStatus(s))
    return () => { unsub1(); unsub2() }
  }, [])

  const remoteClients = clients.filter(c => c.remoteAvailable)

  // Password is injected server-side; the UI never asks for it.
  const buildNoVNCUrl = (ip, port) => {
    const serverIP = serverStatus?.ip || '127.0.0.1'
    const httpPort = serverStatus?.httpPort || 8080
    return `http://${serverIP}:${httpPort}/novnc?host=${ip}&port=${port}`
  }

  const handleConnect = (client) => {
    setActiveClient({
      ip: client.ip,
      port: client.remoteVncPort || 5900,
      mac: client.mac,
      url: buildNoVNCUrl(client.ip, client.remoteVncPort || 5900),
    })
  }

  const handleManualConnect = () => {
    if (!manualIP) return
    setActiveClient({
      ip: manualIP,
      port: parseInt(manualPort) || 5900,
      mac: 'manual',
      url: buildNoVNCUrl(manualIP, manualPort),
    })
  }

  const handleDisconnect = () => {
    setActiveClient(null)
    setFullscreen(false)
  }

  const toggleFullscreen = () => {
    setFullscreen(!fullscreen)
  }

  // Active VNC session view
  if (activeClient) {
    return (
      <div className={clsx(
        'flex flex-col',
        fullscreen ? 'fixed inset-0 z-50 bg-slate-950' : 'h-full space-y-0'
      )}>
        <div className="flex items-center gap-3 px-4 py-2 bg-slate-900 border-b border-slate-700 shrink-0">
          <ScreenShare size={16} className="text-emerald-400" />
          <span className="text-sm font-semibold text-white">Control Remoto</span>
          <span className="text-xs text-slate-400 font-mono">{activeClient.ip}:{activeClient.port}</span>
          <span className="flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-emerald-500/15 border border-emerald-500/30 text-emerald-400">
            <span className="w-1.5 h-1.5 rounded-full bg-emerald-400 animate-pulse" />
            Conectado
          </span>
          <span className="flex-1" />
          <button
            onClick={toggleFullscreen}
            className="p-1.5 rounded-lg hover:bg-slate-700 transition-colors text-slate-400 hover:text-white"
            title={fullscreen ? 'Salir de pantalla completa' : 'Pantalla completa'}
          >
            {fullscreen ? <Minimize2 size={16} /> : <Maximize2 size={16} />}
          </button>
          <button
            onClick={handleDisconnect}
            className="flex items-center gap-1 px-2.5 py-1 text-xs rounded-lg bg-red-500/15 border border-red-500/30 text-red-400 hover:bg-red-500/25 transition-colors"
          >
            <X size={13} />
            Desconectar
          </button>
        </div>
        <div className="flex-1 bg-black">
          <iframe
            ref={iframeRef}
            src={activeClient.url}
            className="w-full h-full border-0"
            title="VNC Remote"
            allow="clipboard-read; clipboard-write"
          />
        </div>
      </div>
    )
  }

  // Client selection view
  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold text-white">Control Remoto</h2>
        <span className="flex items-center gap-2 text-sm text-slate-400">
          <ScreenShare size={14} />
          {remoteClients.length} disponible{remoteClients.length !== 1 ? 's' : ''}
        </span>
      </div>

      {/* Remote clients */}
      {remoteClients.length > 0 ? (
        <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
          <div className="flex items-center gap-2 mb-1">
            <Monitor size={18} className="text-emerald-400" />
            <h3 className="text-sm font-semibold text-white">Clientes con VNC Disponible</h3>
          </div>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
            {remoteClients.map((client) => (
              <div
                key={client.mac}
                className="bg-slate-800 rounded-xl border border-slate-700 p-4 space-y-3 hover:border-emerald-500/30 transition-colors"
              >
                <div className="flex items-center gap-2">
                  <Monitor size={16} className="text-emerald-400" />
                  <span className="font-mono text-sm text-white">{client.ip}</span>
                  <span className="ml-auto w-2 h-2 rounded-full bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.5)]" />
                </div>
                <div className="space-y-1">
                  <p className="text-xs text-slate-400">MAC: <span className="font-mono text-slate-300">{client.mac}</span></p>
                  <p className="text-xs text-slate-400">Puerto VNC: <span className="text-slate-300">{client.remoteVncPort || 5900}</span></p>
                  {client.isoName && (
                    <p className="text-xs text-slate-400">ISO: <span className="text-blue-400">{client.isoName}</span></p>
                  )}
                </div>
                <button
                  onClick={() => handleConnect(client)}
                  className="w-full flex items-center justify-center gap-2 px-3 py-2 text-sm font-medium rounded-lg bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25 transition-colors"
                >
                  <ScreenShare size={14} />
                  Conectar
                </button>
              </div>
            ))}
          </div>
        </div>
      ) : (
        <div className="bg-slate-900 rounded-2xl border border-slate-700 p-10 text-center space-y-3">
          <ScreenShare size={48} className="mx-auto text-slate-600" />
          <p className="text-slate-400 font-medium">No hay clientes con VNC disponible</p>
          <p className="text-xs text-slate-500 max-w-md mx-auto">
            Los clientes que arranquen por PXE con una ISO de Windows mostrarán un botón
            de conexión aquí cuando el servidor VNC esté activo en WinPE.
          </p>
          <div className="flex items-center justify-center gap-2 text-xs text-slate-600 mt-2">
            <Wifi size={12} />
            <span>Asegúrate de que "Control Remoto (WinPE)" esté habilitado en Settings</span>
          </div>
        </div>
      )}

      {/* Manual connection */}
      <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
        <div className="flex items-center gap-2 mb-1">
          <PlugZap size={18} className="text-blue-400" />
          <h3 className="text-sm font-semibold text-white">Conexión Manual</h3>
          <span className="ml-auto text-xs text-slate-500">Conectar a cualquier servidor VNC</span>
        </div>
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          <div>
            <label className="block text-xs text-slate-400 mb-1">IP del cliente</label>
            <input
              type="text"
              value={manualIP}
              onChange={(e) => setManualIP(e.target.value)}
              placeholder="192.168.1.100"
              className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white font-mono focus:outline-none focus:border-blue-500 placeholder:text-slate-600"
            />
          </div>
          <div>
            <label className="block text-xs text-slate-400 mb-1">Puerto VNC</label>
            <input
              type="text"
              value={manualPort}
              onChange={(e) => setManualPort(e.target.value)}
              placeholder="5900"
              className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white font-mono focus:outline-none focus:border-blue-500 placeholder:text-slate-600"
            />
          </div>
          <div className="flex items-end">
            <button
              onClick={handleManualConnect}
              disabled={!manualIP || !serverStatus?.ip}
              className={clsx(
                'w-full flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium rounded-lg transition-colors',
                manualIP && serverStatus?.ip
                  ? 'bg-blue-600 hover:bg-blue-500 text-white'
                  : 'bg-slate-700 text-slate-500 cursor-not-allowed'
              )}
            >
              <ScreenShare size={14} />
              Conectar
            </button>
          </div>
        </div>
        {!serverStatus?.ip && (
          <p className="text-xs text-amber-400">Inicia el servidor primero para poder conectar.</p>
        )}
      </div>
    </div>
  )
}
