// 无限滚动触发器：进入视口时自动加载下一页
import { useEffect, useRef } from 'react';
import { formatMessage } from '../i18n/locale';

interface Props {
  isFetching: boolean;
  onLoadMore(): void;
}

// 回调放 ref：父组件每次渲染传入新引用的 onLoadMore 不再导致 observer 反复重建
export default function LoadMoreTrigger({ isFetching, onLoadMore }: Props) {
  const ref = useRef<HTMLDivElement>(null);
  const onLoadMoreRef = useRef(onLoadMore);
  onLoadMoreRef.current = onLoadMore;
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && !isFetching) onLoadMoreRef.current();
      },
      { rootMargin: '100px' },
    );
    io.observe(el);
    return () => io.disconnect();
  }, [isFetching]);
  return (
    <div ref={ref} className="w-full py-2 text-xs text-center text-gray-400">
      {isFetching ? formatMessage('加载中…') : formatMessage('滚动加载更多')}
    </div>
  );
}
