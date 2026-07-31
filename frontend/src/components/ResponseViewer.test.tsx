import { fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import type { ResponseResult } from '../ipc';
import { useLocale } from '../i18n/locale';
import ResponseViewer from './ResponseViewer';

afterEach(() => useLocale.getState().setLocale('zh-CN'));

describe('ResponseViewer body rendering', () => {
  it('keeps oversized response text out of the DOM beyond the render limit', () => {
    const tail = 'SHOULD_NOT_REACH_THE_DOM';
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [],
      cookies: [],
      body: { inline: true, text: 'x'.repeat(600_000) + tail, encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 600_000 + tail.length,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    const pre = view.container.querySelector('pre');
    expect(pre?.textContent).toHaveLength(500_000);
    expect(view.container.textContent).not.toContain(tail);
    expect(view.container.textContent).toContain('响应体过大');
  });

  it('does not translate response data in the English interface', () => {
    useLocale.getState().setLocale('en');
    const response = {
      status: 200,
      statusText: '保存',
      headers: [{ key: '保存', value: '删除' }],
      cookies: [],
      body: { inline: true, text: '保存', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 2,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    expect(view.container.querySelector('pre')).toHaveTextContent('保存');
    expect(view.container.textContent).toContain('200 保存');
    expect(view.container.textContent).not.toContain('200 Save');
  });

  it('does not render a partial HTML blob inside the preview iframe', () => {
    Object.defineProperty(window, 'go', {
      configurable: true,
      value: {
        binding: {
          RequestApi: {
            GetResponseBlobInfo: () => Promise.resolve({ ref: 'large.html', sizeBytes: 3 << 20 }),
          },
        },
      },
    });
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [{ key: 'Content-Type', value: 'text/html' }],
      cookies: [],
      body: {
        inline: false,
        blobRef: 'large.html',
        text: '<html><body>partial preview marker</body></html>',
        encoding: 'utf8',
      },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 3 << 20,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} nodeId="request-1" />);
    fireEvent.click(view.getByRole('button', { name: 'Preview' }));

    expect(view.container.querySelector('iframe')).toBeNull();
    expect(view.container).toHaveTextContent('大型 HTML 响应不加载到 Preview');
    expect(view.queryByRole('button', { name: '保存为示例' })).toBeNull();
  });
});
