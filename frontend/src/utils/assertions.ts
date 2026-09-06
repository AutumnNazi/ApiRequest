// 断言助手：从当前响应生成 pm.test 断言骨架（scripts 面板"插入断言"）。
// 生成的是可用起点而非完备覆盖——用户仍需按业务调整。
import type { ResponseResult } from '../ipc';

export interface ResponseLike {
  status: number;
  headers: Array<{ key: string; value: string; enabled?: boolean }>;
  bodyText: string;
  durationMs: number;
}

export interface AssertionSnippet {
  label: string;
  code: string;
}

const MAX_PROPERTY_ASSERTIONS = 5;

function wrap(name: string, body: string): string {
  return `pm.test('${name}', function () {\n  ${body.replace(/\n/g, '\n  ')}\n});`;
}

// 从响应生成断言片段列表（顺序：状态码 → Content-Type → JSON 字段 → 耗时）
export function buildAssertions(response: ResponseLike): AssertionSnippet[] {
  const out: AssertionSnippet[] = [];

  out.push({
    label: `状态码 ${response.status}`,
    code: wrap(`status is ${response.status}`, `pm.response.to.have.status(${response.status});`),
  });

  const contentType = response.headers.find(
    (h) => h.enabled !== false && h.key.toLowerCase() === 'content-type',
  );
  if (contentType) {
    // 断言主类型即可（忽略 charset 等参数）
    const main = contentType.value.split(';')[0].trim();
    out.push({
      label: `Content-Type 包含 ${main}`,
      code: wrap(
        `content-type is ${main}`,
        `pm.expect(pm.response.headers.get('content-type')).to.include('${main}');`,
      ),
    });
  }

  const trimmed = response.bodyText.trim();
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      const parsed = JSON.parse(trimmed) as unknown;
      if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) {
        const keys = Object.keys(parsed as Record<string, unknown>).slice(0, MAX_PROPERTY_ASSERTIONS);
        for (const key of keys) {
          out.push({
            label: `字段 ${key} 存在`,
            code: wrap(
              `body has ${key}`,
              `pm.expect(pm.response.json()).to.have.property('${key}');`,
            ),
          });
        }
      } else if (Array.isArray(parsed)) {
        out.push({
          label: 'JSON 是非空数组',
          code: wrap(
            'body is a non-empty array',
            "pm.expect(pm.response.json().length).to.be.above(0);",
          ),
        });
      }
    } catch {
      // 非法 JSON：跳过字段断言
    }
  }

  if (response.durationMs > 0) {
    // 取整到 500ms 的整数倍上限，避免脆弱的精确断言
    const budget = Math.max(500, Math.ceil(response.durationMs / 500) * 500);
    out.push({
      label: `耗时低于 ${budget}ms`,
      code: wrap(`responds under ${budget}ms`, `pm.expect(pm.response.duration).to.be.below(${budget});`),
    });
  }

  return out;
}

// ResponseResult（ipc 类型）→ 助手入参（避免组件层做字段搬运）
export function fromResponseResult(r: ResponseResult): ResponseLike {
  return {
    status: r.status,
    headers: r.headers.map((h) => ({ key: h.key, value: h.value, enabled: true })),
    bodyText: r.body?.text ?? '',
    durationMs: Math.round(r.timing.totalMs),
  };
}
