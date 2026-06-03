import { useState, useEffect } from 'react'
import { ScreenShare, Monitor, Wifi, X, Maximize2, Minimize2 } from 'lucide-react'
import { GetConnectedClients, GetServerStatus } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import RemoteViewer from './RemoteViewer'
import clsx from 'clsx'

export default function RemoteControl() {
  const [clients, setClients] = useState([])
  const [serverStatus, setServerStatus] = useState(null)
  const [activeClient, setActiveClient] = useState(null)
  const [fullscreen, setFullscreen] = useState(false)
  const [manualIP, setManualIP] = useState('')
  useEffect(() => {
    GetConnectedClients().then((c) => setClients(c || []))
    GetServerStatus().then((s) => setServerStatus(s))

    const unsub1 = EventsOn('client:updated', () => {
      GetConnectedClients().then((c) => setClients(c || []))
    })
    const unsub2 = EventsOn('server:status-changed', (s) => setServerStatus(s))
    return () => { unsub1(); unsub2() }
  }, [])

  const remoteClients = clients

  const handleConnect = (client) => {
    setActiveClient({
      ip: client.ip,
      port: serverStatus?.httpPort || 8080,
      mac: client.mac,
      clientId: client.ip || client.mac,
    })
  }

  const handleManualConnect = () => {
    const target = manualIP || serverStatus?.ip || '127.0.0.1'
    setActiveClient({
      ip: target,
      port: serverStatus?.httpPort || 8080,
      mac: 'manual',
      clientId: target,
    })
  }

  const handleDisconnect = () => {
    setActiveClient(null)
    setFullscreen(false)
  }

  const toggleFullscreen = () => {
    setFullscreen(!fullscreen)
  }

  // Active native remote session view
  if (activeClient) {
    return (
      <div className={clsx(
        'flex flex-col',
        fullscreen ? 'fixed inset-0 z-50 bg-slate-950' : 'h-full space-y-0'
      )}>
        <div className="flex items-center gap-3 px-4 py-2 bg-slate-900 border-b border-slate-700 shrink-0">
          <ScreenShare size={16} className="text-emerald-400" />
          <span className="text-sm font-semibold text-white">Control Remoto</span>
          <span className="text-xs text-slate-400 font-mono">ws://{serverStatus?.ip || '127.0.0.1'}:{serverStatus?.httpPort || 8080}/ws/remote/{activeClient.clientId}</span>
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
          <RemoteViewer
            serverIP={serverStatus?.ip || '127.0.0.1'}
            httpPort={serverStatus?.httpPort || 8080}
            clientId={activeClient.clientId}
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
          Agente nativo
        </span>
      </div>

      <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
        <div className="flex items-center gap-2 mb-1">
          <ScreenShare size={18} className="text-emerald-400" />
          <h3 className="text-sm font-semibold text-white">Visor nativo WebSocket</h3>
          <span className="ml-auto text-xs text-slate-500">Sin VNC / noVNC</span>
        </div>
        <div className="flex flex-col gap-3 md:flex-row md:items-end">
          <div className="flex-1">
            <label className="block text-xs text-slate-400 mb-1">Servidor del agente</label>
            <input
              type="text"
              value={manualIP}
              onChange={(e) => setManualIP(e.target.value)}
              placeholder={serverStatus?.ip || '127.0.0.1'}
              className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white font-mono focus:outline-none focus:border-blue-500 placeholder:text-slate-600"
            />
          </div>
          <button
            onClick={handleManualConnect}
            disabled={!serverStatus?.ip}
            className={clsx(
              'flex items-center justify-center gap-2 px-4 py-2 text-sm font-medium rounded-lg transition-colors',
              serverStatus?.ip
                ? 'bg-emerald-600 hover:bg-emerald-500 text-white'
                : 'bg-slate-700 text-slate-500 cursor-not-allowed'
            )}
          >
            <ScreenShare size={14} />
            Abrir visor nativo
          </button>
        </div>
        <p className="text-xs text-slate-500">
          El agente se separa por cliente en ws://{serverStatus?.ip || 'SERVIDOR'}:{serverStatus?.httpPort || 8080}/ws/remote/&lt;IP-del-cliente&gt;.
        </p>
      </div>

      {/* Remote clients */}
      {remoteClients.length > 0 ? (
        <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
          <div className="flex items-center gap-2 mb-1">
            <Monitor size={18} className="text-emerald-400" />
            <h3 className="text-sm font-semibold text-white">Clientes con agente remoto</h3>
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
                  <p className="text-xs text-slate-400">Proxy: <span className="text-slate-300">{serverStatus?.httpPort || 8080}</span></p>
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
          <p className="text-slate-400 font-medium">No hay clientes detectados todavía</p>
          <p className="text-xs text-slate-500 max-w-md mx-auto">
            Los clientes que arranquen por PXE con una ISO de Windows mostrarán un botón
            de conexión aquí cuando el cliente PXE haya reportado sesión. El visor nativo de arriba no depende de VNC.
          </p>
          <div className="flex items-center justify-center gap-2 text-xs text-slate-600 mt-2">
            <Wifi size={12} />
            <span>Asegúrate de que "Control Remoto (WinPE)" esté habilitado en Settings</span>
          </div>
        </div>
      )}

      {!serverStatus?.ip && (
        <p className="text-xs text-amber-400">Inicia el servidor primero para poder abrir el visor.</p>
      )}
    </div>
  )
}
