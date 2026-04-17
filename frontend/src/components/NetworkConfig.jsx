import { useState, useEffect } from 'react'
import { Network, Save, FolderOpen, Cpu, Terminal, Layers, Wifi, ScreenShare } from 'lucide-react'
import {
  GetNetworkInterfaces,
  GetSelectedInterface,
  SetNetworkInterface,
  GetISODirectory,
  BrowseISODirectory,
  GetBootProtocol,
  SetBootProtocol,
  GetWinPERemote,
  SetWinPERemote,
} from '../../wailsjs/go/main/App'
import clsx from 'clsx'

const bootProtocols = [
  {
    id: 'ipxe',
    name: 'iPXE',
    icon: Layers,
    color: 'blue',
    description: 'Chainloading completo con menú iPXE interactivo. Compatible con BIOS y UEFI. Recomendado para la mayoría de escenarios.',
    badge: 'Recomendado',
  },
  {
    id: 'grub',
    name: 'GRUB',
    icon: Terminal,
    color: 'amber',
    description: 'Usa GRUB2 como bootloader. Menú nativo con soporte para Secure Boot. Ideal para entornos UEFI estrictos.',
    badge: 'UEFI',
  },
  {
    id: 'undionly',
    name: 'Undionly',
    icon: Cpu,
    color: 'purple',
    description: 'Sirve solo el binario iPXE sin menú. El cliente obtiene un shell iPXE para booteo manual. Para diagnóstico.',
    badge: 'Debug',
  },
]

const colorClasses = {
  blue: {
    active: 'bg-blue-500/15 border-blue-500/50 ring-2 ring-blue-500/30',
    badge: 'bg-blue-500/20 text-blue-400 border-blue-500/30',
    icon: 'text-blue-400',
    glow: 'shadow-[0_0_20px_rgba(59,130,246,0.15)]',
  },
  amber: {
    active: 'bg-amber-500/15 border-amber-500/50 ring-2 ring-amber-500/30',
    badge: 'bg-amber-500/20 text-amber-400 border-amber-500/30',
    icon: 'text-amber-400',
    glow: 'shadow-[0_0_20px_rgba(245,158,11,0.15)]',
  },
  purple: {
    active: 'bg-purple-500/15 border-purple-500/50 ring-2 ring-purple-500/30',
    badge: 'bg-purple-500/20 text-purple-400 border-purple-500/30',
    icon: 'text-purple-400',
    glow: 'shadow-[0_0_20px_rgba(168,85,247,0.15)]',
  },
}

export default function NetworkConfig() {
  const [interfaces, setInterfaces] = useState([])
  const [selectedIface, setSelectedIface] = useState('')
  const [isoDir, setIsoDir] = useState('')
  const [bootProtocol, setBootProtocol] = useState('ipxe')
  const [winpeRemote, setWinpeRemote] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    Promise.all([
      GetNetworkInterfaces(),
      GetSelectedInterface(),
      GetISODirectory(),
      GetBootProtocol(),
      GetWinPERemote(),
    ]).then(([ifaces, iface, dir, proto, remote]) => {
      setInterfaces(ifaces || [])
      setSelectedIface(iface)
      setIsoDir(dir)
      setBootProtocol(proto)
      setWinpeRemote(!!remote)
    })
  }, [])

  const handleSave = async () => {
    setError('')
    setSaved(false)
    try {
      await SetNetworkInterface(selectedIface)
      await SetBootProtocol(bootProtocol)
      await SetWinPERemote(winpeRemote)
      setSaved(true)
      setTimeout(() => setSaved(false), 3000)
    } catch (e) {
      setError(String(e))
    }
  }

  const handleBrowse = async () => {
    try {
      const dir = await BrowseISODirectory()
      if (dir) setIsoDir(dir)
    } catch (e) {
      console.error(e)
    }
  }

  return (
    <div className="space-y-6">
      <h2 className="text-2xl font-bold text-white">Configuración</h2>

      {/* ====== MODE INFO ====== */}
      <div className="flex items-center gap-2 px-4 py-3 rounded-xl border bg-emerald-500/10 border-emerald-500/30">
        <Wifi size={16} className="text-emerald-400 shrink-0" />
        <p className="text-xs text-emerald-400">
          <strong>Modo Proxy PXE</strong> — El router asigna las IPs. IBootTime solo responde cuando un cliente pide arrancar por red (PXE boot).
        </p>
      </div>

      {/* ====== BOOT PROTOCOL ====== */}
      <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
        <div className="flex items-center gap-2 mb-1">
          <Layers size={18} className="text-blue-400" />
          <h3 className="text-sm font-semibold text-white">Protocolo de Boot</h3>
          <span className="ml-auto text-xs text-slate-500">Selecciona cómo bootean los clientes</span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          {bootProtocols.map((proto) => {
            const Icon = proto.icon
            const isActive = bootProtocol === proto.id
            const colors = colorClasses[proto.color]
            return (
              <button
                key={proto.id}
                onClick={() => setBootProtocol(proto.id)}
                className={clsx(
                  'relative flex flex-col items-start gap-3 p-4 rounded-xl border text-left transition-all duration-200',
                  isActive
                    ? `${colors.active} ${colors.glow}`
                    : 'bg-slate-800/50 border-slate-700 hover:border-slate-600 hover:bg-slate-800'
                )}
              >
                <div className="flex items-center gap-3 w-full">
                  <div className={clsx(
                    'w-10 h-10 rounded-lg flex items-center justify-center',
                    isActive ? `${colors.badge}` : 'bg-slate-700/50'
                  )}>
                    <Icon size={20} className={isActive ? colors.icon : 'text-slate-400'} />
                  </div>
                  <div className="flex-1">
                    <p className={clsx('text-sm font-bold', isActive ? 'text-white' : 'text-slate-300')}>
                      {proto.name}
                    </p>
                    <span className={clsx(
                      'text-[10px] px-1.5 py-0.5 rounded border',
                      isActive ? colors.badge : 'bg-slate-700/50 text-slate-500 border-slate-600'
                    )}>
                      {proto.badge}
                    </span>
                  </div>
                  {isActive && (
                    <div className={clsx('w-2.5 h-2.5 rounded-full', `bg-${proto.color}-400`)}
                      style={{boxShadow: `0 0 8px var(--color-${proto.color}-400, #60a5fa)`}} />
                  )}
                </div>
                <p className="text-xs text-slate-400 leading-relaxed">{proto.description}</p>
              </button>
            )
          })}
        </div>
      </div>

      {/* ====== NETWORK + ISO ====== */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
          <div className="flex items-center gap-2 mb-1">
            <Network size={18} className="text-blue-400" />
            <h3 className="text-sm font-semibold text-white">Interfaz de Red</h3>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Seleccionar Interfaz</label>
            <select
              value={selectedIface}
              onChange={(e) => setSelectedIface(e.target.value)}
              className="w-full bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-blue-500"
            >
              <option value="">-- Seleccionar --</option>
              {interfaces.map((iface) => (
                <option key={iface.name} value={iface.name}>
                  {iface.name} - {iface.ip} ({iface.mac})
                </option>
              ))}
            </select>
          </div>

          <div>
            <label className="block text-xs text-slate-400 mb-1">Directorio ISO</label>
            <div className="flex gap-2">
              <input
                type="text"
                value={isoDir}
                readOnly
                className="flex-1 bg-slate-800 border border-slate-600 rounded-lg px-3 py-2 text-sm text-slate-300 font-mono"
              />
              <button
                onClick={handleBrowse}
                className="px-3 py-2 bg-slate-700 hover:bg-slate-600 border border-slate-600 rounded-lg transition-colors"
              >
                <FolderOpen size={16} className="text-slate-300" />
              </button>
            </div>
          </div>
        </div>

        <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
          <div className="flex items-center gap-2 mb-1">
            <Wifi size={18} className="text-emerald-400" />
            <h3 className="text-sm font-semibold text-white">Cómo funciona</h3>
          </div>

          <div className="space-y-3 text-xs text-slate-400">
            <div className="flex gap-2">
              <span className="text-emerald-400 font-mono shrink-0">1.</span>
              <span>El <strong className="text-white">router</strong> asigna la IP al cliente</span>
            </div>
            <div className="flex gap-2">
              <span className="text-emerald-400 font-mono shrink-0">2.</span>
              <span><strong className="text-white">IBootTime</strong> envía la info de boot (archivo + servidor TFTP)</span>
            </div>
            <div className="flex gap-2">
              <span className="text-emerald-400 font-mono shrink-0">3.</span>
              <span>El cliente descarga <strong className="text-white">iPXE</strong> por TFTP</span>
            </div>
            <div className="flex gap-2">
              <span className="text-emerald-400 font-mono shrink-0">4.</span>
              <span>iPXE carga el <strong className="text-white">menú de ISOs</strong> por HTTP</span>
            </div>
          </div>
        </div>
      </div>

      {/* ====== REMOTE WINPE ====== */}
      <div className="bg-slate-900 rounded-2xl border border-slate-700 p-5 space-y-4">
        <div className="flex items-center gap-2 mb-1">
          <ScreenShare size={18} className="text-emerald-400" />
          <h3 className="text-sm font-semibold text-white">Control Remoto (WinPE)</h3>
          <span className="ml-auto text-xs text-slate-500">VNC durante la instalación</span>
        </div>

        <div className="flex items-center justify-between">
          <div className="space-y-1">
            <p className="text-sm text-slate-300">Habilitar control remoto en WinPE</p>
            <p className="text-xs text-slate-500">
              Inyecta un servidor VNC en el boot.wim para controlar el instalador remotamente vía noVNC.
              Requiere colocar UltraVNC portable en <code className="bg-slate-800 px-1 rounded">remote/winvnc/</code>
            </p>
          </div>
          <button
            onClick={() => setWinpeRemote(!winpeRemote)}
            className={clsx(
              'relative w-12 h-6 rounded-full transition-colors shrink-0 ml-4',
              winpeRemote ? 'bg-emerald-500' : 'bg-slate-600'
            )}
          >
            <span className={clsx(
              'absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform',
              winpeRemote ? 'translate-x-6' : 'translate-x-0.5'
            )} />
          </button>
        </div>

        {winpeRemote && (
          <div className="flex items-center gap-2 px-3 py-2 rounded-lg border bg-amber-500/10 border-amber-500/30">
            <p className="text-xs text-amber-400">
              <strong>Nota:</strong> Al habilitar esta opción, la caché de boot.wim se reconstruirá la próxima vez que inicie el servidor.
              Asegúrate de tener <code className="bg-slate-800 px-1 rounded">winvnc.exe</code> en la carpeta <code className="bg-slate-800 px-1 rounded">remote/winvnc/</code>.
              La contraseña VNC se genera automáticamente por sesión.
            </p>
          </div>
        )}
      </div>

      {/* ====== SAVE ====== */}
      <div className="flex items-center gap-3">
        <button
          onClick={handleSave}
          className="flex items-center gap-2 px-5 py-2.5 bg-blue-600 hover:bg-blue-500 rounded-lg text-sm font-medium text-white transition-all duration-200 hover:shadow-[0_0_20px_rgba(59,130,246,0.3)]"
        >
          <Save size={16} />
          Guardar y Aplicar
        </button>
        {saved && (
          <span className="text-sm text-emerald-400 flex items-center gap-1.5">
            <span className="w-2 h-2 rounded-full bg-emerald-400 animate-pulse" />
            Configuración guardada
          </span>
        )}
        {error && <span className="text-sm text-red-400">{error}</span>}
      </div>
    </div>
  )
}
