import { LayoutDashboard, Disc3, Monitor, Settings, ScreenShare, Bot } from 'lucide-react'
import clsx from 'clsx'

const navItems = [
  { id: 'dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { id: 'isos', label: 'ISO Manager', icon: Disc3 },
  { id: 'clients', label: 'Clients', icon: Monitor },
  { id: 'remote', label: 'Remote', icon: ScreenShare },
  { id: 'agents', label: 'Agents', icon: Bot },
  { id: 'config', label: 'Settings', icon: Settings },
]

export default function Sidebar({ activeView, onNavigate }) {
  return (
    <aside className="w-56 bg-slate-900 border-r border-slate-700 flex flex-col">
      <div className="p-4 border-b border-slate-700">
        <h1 className="text-lg font-bold text-white tracking-tight">
          IBootTime
        </h1>
        <p className="text-xs text-slate-500 mt-0.5">Network Boot Server</p>
      </div>

      <nav className="flex-1 p-3 space-y-1">
        {navItems.map((item) => {
          const Icon = item.icon
          const isActive = activeView === item.id
          return (
            <button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={clsx(
                'w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors',
                isActive
                  ? 'bg-blue-600 text-white'
                  : 'text-slate-400 hover:bg-slate-800 hover:text-slate-200'
              )}
            >
              <Icon size={18} />
              {item.label}
            </button>
          )
        })}
      </nav>

      <div className="p-3 border-t border-slate-700">
        <p className="text-xs text-slate-600 text-center">v1.0.0</p>
      </div>
    </aside>
  )
}
