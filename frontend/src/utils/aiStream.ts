// AI / LLM 流式响应聚合（docs/advanced.md 调试可观测性）：
// 把 SSE 事件流中的主流大模型增量字段拼接为完整回复，并从消息时间戳
// 计算事件层耗时指标（首事件延迟、事件数、流式时长、usage token 数）。
// 纯函数：输入 WsPanel 已过滤的单会话消息序列，无副作用。
import type { InboundMsg } from '../ipc';

export interface AiStreamMetrics {
  // 首个产生内容的事件相对 open 消息的延迟（TTFT 语义，毫秒）；无 open 或无内容为 null
  firstEventMs: number | null;
  // 被识别并拼接的事件数
  eventCount: number;
  // 最后一个数据事件相对 open 的流逝时间（毫秒）；近似"流式总时长"
  streamDurationMs: number | null;
  // 服务商在流末尾下发的 usage 统计（OpenAI include_usage / Anthropic message_delta）
  usage: Record<string, number> | null;
  // 非 JSON 或未命中已知字段的数据事件数（含 [DONE] 控制帧）
  unhandled: number;
}

export interface AiStreamResult {
  // 拼接后的完整回复文本
  text: string;
  // 命中的增量字段路径（首个成功事件的）；未命中任何事件为 null
  matchedField: string | null;
  metrics: AiStreamMetrics;
}

// 主流增量字段（按序尝试，单事件内首个命中生效）：
// OpenAI chat / 旧版 completions / Anthropic text_delta / Ollama chat
const FIELD_PATHS: Array<{ path: string; get: (v: any) => unknown }> = [
  { path: 'choices[0].delta.content', get: (v) => v?.choices?.[0]?.delta?.content },
  { path: 'choices[0].text', get: (v) => v?.choices?.[0]?.text },
  { path: 'delta.text', get: (v) => v?.delta?.text },
  { path: 'message.content', get: (v) => v?.message?.content },
];

const extractText = (value: unknown): string | null =>
  typeof value === 'string' && value.length > 0 ? value : null;

const extractUsage = (value: unknown): Record<string, number> | null => {
  if (!value || typeof value !== 'object') return null;
  const usage = (value as Record<string, unknown>).usage;
  if (!usage || typeof usage !== 'object') return null;
  const out: Record<string, number> = {};
  for (const [key, val] of Object.entries(usage as Record<string, unknown>)) {
    if (typeof val === 'number') out[key] = val;
  }
  return Object.keys(out).length > 0 ? out : null;
};

export function aggregateAiStream(messages: InboundMsg[]): AiStreamResult {
  const open = messages.find((m) => m.kind === 'open');
  const openTs = open?.ts ?? null;

  let text = '';
  let matchedField: string | null = null;
  let eventCount = 0;
  let unhandled = 0;
  let firstEventTs: number | null = null;
  let lastEventTs: number | null = null;
  let usage: Record<string, number> | null = null;

  for (const m of messages) {
    if (m.direction !== 'in' || m.kind !== 'event' || m.data === '') continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(m.data);
    } catch {
      unhandled++;
      continue;
    }
    if (parsed === null || typeof parsed !== 'object') {
      unhandled++;
      continue;
    }
    const obj = parsed as Record<string, unknown>;
    const candidateUsage = extractUsage(obj);
    if (candidateUsage) usage = candidateUsage;

    let piece: string | null = null;
    for (const { path, get } of FIELD_PATHS) {
      piece = extractText(get(obj));
      if (piece !== null) {
        if (!matchedField) matchedField = path;
        break;
      }
    }
    if (piece === null) {
      // 有 usage 无增量的收尾事件（choices 空数组）不算未处理
      if (!candidateUsage) unhandled++;
      continue;
    }
    eventCount++;
    if (firstEventTs === null) firstEventTs = m.ts;
    lastEventTs = m.ts;
    text += piece;
  }

  return {
    text,
    matchedField,
    metrics: {
      firstEventMs: openTs !== null && firstEventTs !== null ? firstEventTs - openTs : null,
      eventCount,
      streamDurationMs: openTs !== null && lastEventTs !== null ? lastEventTs - openTs : null,
      usage,
      unhandled,
    },
  };
}
