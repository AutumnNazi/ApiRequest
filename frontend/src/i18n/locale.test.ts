import { afterEach, describe, expect, it } from 'vitest';
import { formatMessage, translate, translateExact, useLocale } from './locale';

afterEach(() => useLocale.getState().setLocale('zh-CN'));

describe('English localization boundaries', () => {
  it('only auto-translates exact UI catalog entries', () => {
    useLocale.getState().setLocale('en');

    expect(translateExact('保存')).toBe('Save');
    expect(translateExact('响应正文包含保存按钮')).toBe('响应正文包含保存按钮');
    expect(translate('响应正文包含保存按钮')).toBe('响应正文包含保存按钮');
  });

  it('formats translated templates without translating inserted data', () => {
    useLocale.getState().setLocale('en');

    expect(formatMessage('删除集合「{name}」及其全部请求？', { name: '保存' })).toBe(
      'Delete collection "保存" and all its requests?',
    );
  });

  it('translates application close messages', () => {
    useLocale.getState().setLocale('en');

    expect(formatMessage('仍有未保存的请求草稿，确认退出应用？')).toBe(
      'There are unsaved request drafts. Quit the application?',
    );
    expect(formatMessage('退出应用失败: {detail}', { detail: 'disk full' })).toBe(
      'Failed to quit the application: disk full',
    );
    expect(translate('退出失败')).toBe('Quit failed');
  });

  // 目录键可能只含全角标点（如全角冒号），英文环境仍需替换为半角形式。
  it('translates catalog entries whose only CJK characters are full-width punctuation', () => {
    useLocale.getState().setLocale('en');

    expect(formatMessage('{message}：{detail}', { message: 'Load failed', detail: 'timeout' })).toBe(
      'Load failed: timeout',
    );
  });

  it('leaves punctuation-only templates untouched in both locales', () => {
    const values = { pushed: 2, pulled: 3, deleted: '' };

    expect(formatMessage('↑{pushed} ↓{pulled}{deleted}', values)).toBe('↑2 ↓3');

    useLocale.getState().setLocale('en');
    expect(formatMessage('↑{pushed} ↓{pulled}{deleted}', values)).toBe('↑2 ↓3');
  });
});
