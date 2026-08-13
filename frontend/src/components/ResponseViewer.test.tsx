import { fireEvent, render, screen } from '@testing-library/react';
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

  it('renders JSON syntax colors for a parseable JSON body', () => {
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [{ key: 'Content-Type', value: 'application/json' }],
      cookies: [],
      body: { inline: true, text: '{"name":"alice","age":30,"ok":true,"n":null}', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 48,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    const pre = view.container.querySelector('pre');
    // 字符串值（green）、数字（orange）、布尔（blue）都有对应着色 span
    expect(pre?.querySelector('.text-green-700')).not.toBeNull();
    expect(pre?.querySelector('.text-orange-600')).not.toBeNull();
    expect(pre?.querySelector('.text-blue-600')).not.toBeNull();
    // key 也有着色（purple）
    expect(pre?.querySelector('.text-purple-600')).not.toBeNull();
    expect(response.body.text).toBeDefined();
    expect(pre?.textContent).toBe(JSON.stringify(JSON.parse(response.body.text!), null, 2));
  });

  it('returns to Body when a new response no longer supports Preview', () => {
    const html = {
      status: 200,
      statusText: 'OK',
      headers: [{ key: 'Content-Type', value: 'text/html' }],
      cookies: [],
      body: { inline: true, text: '<h1>Hello</h1>', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 14,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;
    const json = {
      ...html,
      headers: [{ key: 'Content-Type', value: 'application/json' }],
      body: { inline: true, text: '{"ok":true}', encoding: 'utf8' },
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={html} sending={false} />);
    fireEvent.click(view.getByRole('button', { name: 'Preview' }));
    expect(view.container.querySelector('iframe')).not.toBeNull();

    view.rerender(<ResponseViewer response={json} sending={false} />);
    expect(view.queryByRole('button', { name: 'Preview' })).toBeNull();
    expect(view.getByRole('button', { name: 'Body' })).toHaveClass('border-blue-600');
    expect(view.container.querySelector('iframe')).toBeNull();
    expect(view.container.querySelector('pre')).toHaveTextContent('"ok"');
  });

  it('does not create one syntax node per character for a large JSON body', () => {
    const text = JSON.stringify(Array.from({ length: 40_000 }, (_, index) => index % 10));
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [{ key: 'Content-Type', value: 'application/json' }],
      cookies: [],
      body: { inline: true, text, encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: text.length,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    expect(view.container.querySelectorAll('pre span')).toHaveLength(0);
    expect(view.container.querySelector('pre')?.textContent).toContain('\n');
  });

  it('keeps raw (non-JSON) body uncolored and search highlights intact', () => {
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [],
      cookies: [],
      body: { inline: true, text: 'plain text body', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 15,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    const pre = view.container.querySelector('pre');
    // 非 JSON：不出现任何 JSON 着色类别
    expect(pre?.querySelector('.text-green-700')).toBeNull();
    expect(pre?.querySelector('.text-orange-600')).toBeNull();
  });

  it('uses the same non-overlapping matches for count and highlights', () => {
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [],
      cookies: [],
      body: { inline: true, text: 'aaa', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 3,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    fireEvent.change(screen.getByPlaceholderText('搜索 body…'), { target: { value: 'aa' } });
    expect(view.container).toHaveTextContent('1 处');
    expect(view.container.querySelectorAll('mark')).toHaveLength(1);
  });

  it('colors a top-level JSON boolean', () => {
    const response = {
      status: 200,
      statusText: 'OK',
      headers: [{ key: 'Content-Type', value: 'application/json' }],
      cookies: [],
      body: { inline: true, text: 'true', encoding: 'utf8' },
      timing: { dnsMs: 0, connectMs: 0, tlsMs: 0, ttfbMs: 1, downloadMs: 1, totalMs: 2 },
      sizeBytes: 4,
      testResults: [],
      scriptLogs: [],
    } as unknown as ResponseResult;

    const view = render(<ResponseViewer response={response} sending={false} />);
    expect(view.container.querySelector('pre .text-blue-600')).toHaveTextContent('true');
  });
});
