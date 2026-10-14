// AI 流式聚合纯函数测试：识别主流 LLM 流式字段、拼接完整回复、事件层耗时指标
import { describe, expect, it } from 'vitest';
import { aggregateAiStream } from './aiStream';
import type { InboundMsg } from '../ipc';

const msg = (over: Partial<InboundMsg>): InboundMsg => ({
  sessionId: 's1',
  protocol: 'sse',
  direction: 'in',
  kind: 'event',
  data: '',
  ts: 0,
  ...over,
});

describe('aggregateAiStream 字段识别与拼接', () => {
  it('OpenAI chat：拼接 choices[0].delta.content', () => {
    const result = aggregateAiStream([
      msg({ ts: 1000, kind: 'open', data: 'https://api.openai.test/v1/chat' }),
      msg({ ts: 1120, data: '{"choices":[{"delta":{"role":"assistant"}}]}' }),
      msg({ ts: 1140, data: '{"choices":[{"delta":{"content":"你"}}]}' }),
      msg({ ts: 1180, data: '{"choices":[{"delta":{"content":"好"}}]}' }),
      msg({ ts: 1200, data: '[DONE]' }),
    ]);
    expect(result.text).toBe('你好');
    expect(result.matchedField).toBe('choices[0].delta.content');
    // TTFT = 首个内容 token（role 帧无内容不计）；role 帧与 [DONE] 均计入 unhandled
    expect(result.metrics.firstEventMs).toBe(140);
    expect(result.metrics.eventCount).toBe(2);
    expect(result.metrics.unhandled).toBe(2);
    expect(result.metrics.streamDurationMs).toBe(180);
  });

  it('旧版 completions 的 choices[0].text 与 Anthropic 的 delta.text', () => {
    const legacy = aggregateAiStream([
      msg({ ts: 0, kind: 'open', data: '' }),
      msg({ ts: 10, data: '{"choices":[{"text":"Hel"}]}' }),
      msg({ ts: 20, data: '{"choices":[{"text":"lo"}]}' }),
    ]);
    expect(legacy.text).toBe('Hello');
    expect(legacy.matchedField).toBe('choices[0].text');

    const anthropic = aggregateAiStream([
      msg({ ts: 0, kind: 'open', data: '' }),
      msg({ ts: 5, data: '{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hi"}}' }),
    ]);
    expect(anthropic.text).toBe('Hi');
    expect(anthropic.matchedField).toBe('delta.text');
  });

  it('Ollama 风格 message.content', () => {
    const result = aggregateAiStream([
      msg({ ts: 0, kind: 'open', data: '' }),
      msg({ ts: 8, data: '{"message":{"content":"he"}}' }),
      msg({ ts: 16, data: '{"message":{"content":"y"}}' }),
      msg({ ts: 20, data: '{"done":true}' }),
    ]);
    expect(result.text).toBe('hey');
    expect(result.matchedField).toBe('message.content');
  });

  it('usage 对象取最后一个非空事件（OpenAI include_usage 语义）', () => {
    const result = aggregateAiStream([
      msg({ ts: 0, kind: 'open', data: '' }),
      msg({ ts: 10, data: '{"choices":[{"delta":{"content":"a"}}]}' }),
      msg({ ts: 20, data: '{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2}}' }),
    ]);
    expect(result.metrics.usage).toEqual({ prompt_tokens: 10, completion_tokens: 2 });
  });

  it('无 open 消息或无数据事件时指标安全为空', () => {
    const empty = aggregateAiStream([msg({ ts: 5, data: '[DONE]' })]);
    expect(empty.text).toBe('');
    expect(empty.metrics.firstEventMs).toBeNull();
    expect(empty.metrics.streamDurationMs).toBeNull();

    const noEvents = aggregateAiStream([msg({ ts: 1, kind: 'open', data: '' })]);
    expect(noEvents.metrics.firstEventMs).toBeNull();
  });

  it('非 JSON 或无命中字段的事件计入 unhandled，不中断聚合', () => {
    const result = aggregateAiStream([
      msg({ ts: 0, kind: 'open', data: '' }),
      msg({ ts: 10, data: 'not-json' }),
      msg({ ts: 20, data: '{"unrelated":1}' }),
      msg({ ts: 30, data: '{"choices":[{"delta":{"content":"ok"}}]}' }),
    ]);
    expect(result.text).toBe('ok');
    expect(result.metrics.unhandled).toBe(2);
  });
});
