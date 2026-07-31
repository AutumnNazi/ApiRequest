// 主题弹窗：预设画廊 + 自定义主题编辑（基于任意主题创建副本改色）
import { useState } from 'react';
import { useTheme } from '../theme/store';
import type { ThemeDefinition, ThemePalette } from '../theme/themes';
import { formatMessage, Verbatim } from '../i18n/locale';
import { useDialog } from './DialogProvider';

interface Props {
  onClose(): void;
}

// 自定义编辑器暴露的可调色项（key, 中文名）
const EDITABLE_COLORS: [keyof ThemePalette, string][] = [
  ['bgApp', '应用背景'],
  ['bgChrome', '顶栏/侧栏'],
  ['bgPanel', '面板'],
  ['bgInput', '输入框'],
  ['fg1', '主文本'],
  ['fg3', '次要文本'],
  ['accent', '强调色'],
  ['accentHover', '强调色(悬停)'],
  ['success', '成功色'],
  ['warn', '警告色'],
  ['danger', '危险色'],
];

export default function ThemeDialog({ onClose }: Props) {
  const dialog = useDialog();
  const { activeId, custom, themes, select, duplicate, updateCustom, removeCustom } = useTheme();
  const [editingId, setEditingId] = useState<string | null>(null);
  const editing = custom.find((t) => t.id === editingId) ?? null;

  const startCustomize = (base: ThemeDefinition) => {
    const id = duplicate(base.id);
    if (!id) {
      void dialog.alert('自定义主题已达上限（12 个）');
      return;
    }
    select(id);
    setEditingId(id);
  };

  return (
    <div className="fixed inset-0 bg-black/30 flex items-center justify-center z-50" onClick={onClose}>
      <div
        className="bg-white rounded-lg shadow-xl w-[720px] max-h-[85vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center px-4 py-3 border-b">
          <h2 className="font-semibold text-sm">主题</h2>
          <span className="ml-2 text-xs text-gray-400">
            预设即点即用；「自定义」基于所选主题创建可编辑副本
          </span>
          <button className="ml-auto text-gray-400 hover:text-gray-700" onClick={onClose}>
            ×
          </button>
        </div>

        <div className="flex-1 overflow-auto p-4">
          {/* 预设画廊 */}
          <div className="grid grid-cols-3 gap-3">
            {themes().map((t) => (
              <ThemeCard
                key={t.id}
                theme={t}
                active={t.id === activeId}
                onSelect={() => select(t.id)}
                onCustomize={() => startCustomize(t)}
                onEdit={t.builtin ? undefined : () => setEditingId(t.id)}
                onRemove={
                  t.builtin
                    ? undefined
                    : () => {
                        void dialog.confirm(formatMessage('删除自定义主题「{name}」？', { name: t.name })).then((ok) => {
                          if (ok) {
                          if (editingId === t.id) setEditingId(null);
                          removeCustom(t.id);
                          }
                        });
                      }
                }
              />
            ))}
          </div>

          {/* 自定义编辑器 */}
          {editing && (
            <div className="mt-4 border rounded p-3 space-y-3">
              <div className="flex items-center gap-2">
                <span className="text-sm text-gray-600">名称</span>
                <input
                  className="border rounded px-2 py-1 text-sm flex-1"
                  value={editing.name}
                  onChange={(e) => updateCustom(editing.id, { name: e.target.value })}
                />
                <button
                  className="text-xs text-gray-500 hover:text-gray-800 border rounded px-2 py-1"
                  onClick={() => setEditingId(null)}
                >
                  收起
                </button>
              </div>
              <div className="grid grid-cols-3 gap-x-4 gap-y-2">
                {EDITABLE_COLORS.map(([key, label]) => (
                  <label key={key} className="flex items-center gap-2 text-xs text-gray-600">
                    <input
                      type="color"
                      className="w-7 h-7 border rounded cursor-pointer p-0"
                      value={toHex(editing.palette[key] as string)}
                      onChange={(e) =>
                        updateCustom(editing.id, { palette: { [key]: e.target.value } })
                      }
                    />
                    <span className="flex-1">{label}</span>
                  </label>
                ))}
              </div>
              <div className="flex items-center gap-2 text-xs text-gray-500">
                <span>基调</span>
                {(['light', 'dark'] as const).map((m) => (
                  <label key={m} className="flex items-center gap-1">
                    <input
                      type="radio"
                      checked={editing.palette.mode === m}
                      onChange={() => updateCustom(editing.id, { palette: { mode: m } })}
                    />
                    {m === 'light' ? '亮色' : '暗色'}
                  </label>
                ))}
                <span className="ml-auto text-gray-400">改动实时生效并自动保存</span>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// 主题卡片：色板缩略 + 操作
function ThemeCard({
  theme,
  active,
  onSelect,
  onCustomize,
  onEdit,
  onRemove,
}: {
  theme: ThemeDefinition;
  active: boolean;
  onSelect(): void;
  onCustomize(): void;
  onEdit?(): void;
  onRemove?(): void;
}) {
  const p = theme.palette;
  return (
    <div
      className={`border rounded-lg overflow-hidden cursor-pointer transition-shadow hover:shadow-md ${
        active ? 'ring-2 ring-blue-500 border-transparent' : ''
      }`}
      onClick={onSelect}
    >
      {/* 迷你界面缩略图 */}
      <div className="h-20 flex" style={{ background: p.bgApp }}>
        <div className="w-1/4 border-r" style={{ background: p.bgChrome, borderColor: p.br1 }} />
        <div className="flex-1 p-1.5 space-y-1">
          <div className="h-2 rounded-sm w-3/4" style={{ background: p.bgPanel, border: `1px solid ${p.br1}` }} />
          <div className="h-2 rounded-sm w-1/2" style={{ background: p.accent }} />
          <div className="h-2 rounded-sm w-2/3" style={{ background: p.bgPanel }} />
          <div className="flex gap-1">
            <span className="h-2 w-2 rounded-full" style={{ background: p.success }} />
            <span className="h-2 w-2 rounded-full" style={{ background: p.warn }} />
            <span className="h-2 w-2 rounded-full" style={{ background: p.danger }} />
          </div>
        </div>
      </div>
      <div className="flex items-center px-2 py-1.5 text-xs bg-white">
        <span className="truncate flex-1">{theme.builtin ? theme.name : <Verbatim value={theme.name} />}</span>
        {active && <span className="text-blue-600 mr-1">✓</span>}
        <button
          className="text-gray-400 hover:text-gray-700 px-1"
          title="以此为底自定义"
          onClick={(e) => {
            e.stopPropagation();
            onCustomize();
          }}
        >
          🎨
        </button>
        {onEdit && (
          <button
            className="text-gray-400 hover:text-gray-700 px-1"
            title="编辑"
            onClick={(e) => {
              e.stopPropagation();
              onEdit();
            }}
          >
            ✎
          </button>
        )}
        {onRemove && (
          <button
            className="text-gray-400 hover:text-red-500 px-1"
            title="删除"
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
          >
            ×
          </button>
        )}
      </div>
    </div>
  );
}

// input[type=color] 只接受 #rrggbb；rgba()/color-mix 等回退到近似值
function toHex(color: string): string {
  if (/^#[0-9a-fA-F]{6}$/.test(color)) return color;
  if (/^#[0-9a-fA-F]{3}$/.test(color)) {
    return '#' + [...color.slice(1)].map((c) => c + c).join('');
  }
  const m = color.match(/rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/);
  if (m) {
    const hex = (n: string) => Number(n).toString(16).padStart(2, '0');
    return '#' + hex(m[1]) + hex(m[2]) + hex(m[3]);
  }
  return '#888888';
}
