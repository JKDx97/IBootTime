import { useState, useEffect } from 'react'
import { Monitor, Wifi, ScreenShare, Lock, Send, ChevronDown } from 'lucide-react'
import { GetConnectedClients, GetServerStatus, GetISOList, AssignISO } from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import clsx from 'clsx'

const stateLabels = {
  discovery: { text: 'Discovery', color: 'text-amber-400 bg-amber-500/10 border-amber-500/30' },
  tftp: { text: 'TFTP Boot', color: 'text-blue-400 bg-blue-500/10 border-blue-500/30' },
  menu: { text: 'In Menu', color: 'text-purple-400 bg-purple-500/10 border-purple-500/30' },
  loading: { text: 'Loading', color: 'text-emerald-400 bg-emerald-500/10 border-emerald-500/30' },
  completed: { text: 'Completed', color: 'text-slate-400 bg-slate-500/10 border-slate-500/30' },
  error: { text: 'Error', color: 'text-red-400 bg-red-500/10 border-red-500/30' },
}

function ProgressBar({ value }) {
  return (
    <div className="w-full bg-slate-700 rounded-full h-2 overflow-hidden">
      <div
        className="h-full bg-emerald-500 rounded-full transition-all duration-300"
        style={{ width: `${Math.min(100, Math.max(0, value))}%` }}
      />
    </div>
  )
}

export default function ClientMonitor() {
  const [clients, setClients] = useState([])
  const [serverStatus, setServerStatus] = useState(null)
  const [isos, setIsos] = useState([])

  useEffect(() => {
    GetConnectedClients().then((c) => setClients(c || []))
    GetServerStatus().then((s) => setServerStatus(s))
    GetISOList().then((l) => setIsos((l || []).filter(i => i.enabled)))

    const unsub = EventsOn('client:updated', () => {
      GetConnectedClients().then((c) => setClients(c || []))
    })
    const unsub2 = EventsOn('server:status-changed', (s) => setServerStatus(s))
    const unsub3 = EventsOn('iso:list-changed', (l) => setIsos((l || []).filter(i => i.enabled)))
    return () => { unsub(); unsub2(); unsub3() }
  }, [])

  const handleAssignISO = async (mac, isoName) => {
    await AssignISO(mac, isoName)
  }

  const handleConnect = (client) => {
    const serverIP = serverStatus?.ip || window.location.hostname
    const httpPort = serverStatus?.httpPort || 8080
    const port = client.remoteVncPort || 5900
    const pw = client.remotePassword || ''
    const url = `http://${serverIP}:${httpPort}/novnc?host=${client.ip}&port=${port}&password=${encodeURIComponent(pw)}`
    window.open(url, '_blank')
  }

  const remoteCount = clients.filter(c => c.remoteAvailable).length

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold text-white">Client Monitor</h2>
        <div className="flex items-center gap-4">
          {remoteCount > 0 && (
            <span className="flex items-center gap-1.5 text-xs text-emerald-400">
              <ScreenShare size={13} />
              {remoteCount} remote disponible{remoteCount !== 1 ? 's' : ''}
            </span>
          )}
          <span className="flex items-center gap-2 text-sm text-slate-400">
            <Wifi size={14} />
            {clients.length} client{clients.length !== 1 ? 's' : ''} connected
          </span>
        </div>
      </div>

      {clients.length === 0 ? (
        <div className="bg-slate-900 rounded-xl border border-slate-700 p-12 text-center">
          <Monitor size={48} className="mx-auto mb-3 text-slate-600" />
          <p className="text-slate-400">No clients connected</p>
          <p className="text-xs text-slate-600 mt-1">
            Clients will appear here when they PXE boot from the network
          </p>
        </div>
      ) : (
        <div className="bg-slate-900 rounded-xl border border-slate-700 overflow-hidden">
          <table className="w-full">
            <thead>
              <tr className="border-b border-slate-700 text-xs text-slate-400 uppercase tracking-wider">
                <th className="text-left px-4 py-3">Equipo</th>
                <th className="text-left px-4 py-3">MAC Address</th>
                <th className="text-left px-4 py-3">IP Address</th>
                <th className="text-left px-4 py-3">Arch</th>
                <th className="text-left px-4 py-3">Status</th>
                <th className="text-left px-4 py-3">ISO Actual</th>
                <th className="text-left px-4 py-3">Boot Remoto</th>
                <th className="text-left px-4 py-3 w-40">Progress</th>
                <th className="text-right px-4 py-3">Speed</th>
                <th className="text-center px-4 py-3">Remote</th>
              </tr>
            </thead>
            <tbody>
              {clients.map((client) => {
                const stateInfo = stateLabels[client.state] || stateLabels.discovery
                return (
                  <tr key={client.mac} className="border-b border-slate-800 hover:bg-slate-800/50 transition-colors">
                    <td className="px-4 py-3 text-sm font-semibold text-white">{client.hostname || '-'}</td>
                    <td className="px-4 py-3 font-mono text-sm text-slate-300">{client.mac}</td>
                    <td className="px-4 py-3 font-mono text-sm text-slate-400">{client.ip}</td>
                    <td className="px-4 py-3 text-sm text-slate-400">{client.arch}</td>
                    <td className="px-4 py-3">
                      <span className={clsx(
                        'text-xs px-2 py-0.5 rounded-full border',
                        stateInfo.color
                      )}>
                        {stateInfo.text}
                      </span>
                    </td>
                    <td className="px-4 py-3 text-sm text-blue-400">
                      {client.isoName || '-'}
                    </td>
                    <td className="px-4 py-3">
                      {client.state !== 'completed' && client.state !== 'error' ? (
                        <div className="flex items-center gap-1.5">
                          <select
                            value={client.assignedISO || ''}
                            onChange={(e) => handleAssignISO(client.mac, e.target.value)}
                            className="text-xs bg-slate-800 border border-slate-600 rounded-lg px-2 py-1.5 text-white max-w-[160px] focus:border-blue-500 focus:ring-1 focus:ring-blue-500 outline-none"
                          >
                            <option value="">-- Seleccionar ISO --</option>
                            {isos.map((iso) => (
                              <option key={iso.name} value={iso.name}>{iso.name}</option>
                            ))}
                          </select>
                          {client.assignedISO && (
                            <span className="flex items-center gap-1 text-xs text-emerald-400 whitespace-nowrap">
                              <Send size={11} />
                              Enviado
                            </span>
                          )}
                        </div>
                      ) : (
                        <span className="text-xs text-slate-600">-</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      {client.state === 'loading' ? (
                        <div className="flex items-center gap-2">
                          <ProgressBar value={client.progress} />
                          <span className="text-xs text-slate-400 w-10 text-right">
                            {client.progress.toFixed(0)}%
                          </span>
                        </div>
                      ) : (
                        <span className="text-xs text-slate-600">-</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-slate-400 text-right font-mono">
                      {client.speed || '-'}
                    </td>
                    <td className="px-4 py-3 text-center">
                      {client.remoteAvailable ? (
                        <button
                          onClick={() => handleConnect(client)}
                          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-emerald-500/15 text-emerald-400 border border-emerald-500/30 hover:bg-emerald-500/25 transition-colors"
                          title={`VNC: ${client.ip}:${client.remoteVncPort}`}
                        >
                          <ScreenShare size={13} />
                          Connect
                        </button>
                      ) : (
                        <span className="text-xs text-slate-600">-</span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
