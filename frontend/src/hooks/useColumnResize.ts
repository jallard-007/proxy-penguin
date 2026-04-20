import { useState, useRef, useCallback } from 'react';

export function useColumnResize(defaultWidths: number[]) {
  const [columnWidths, setColumnWidths] = useState<number[]>(defaultWidths);
  const resizing = useRef<{ colIndex: number; startX: number; startWidth: number } | null>(null);
  const didResize = useRef(false);

  const handleResizeStart = useCallback((e: React.MouseEvent, colIndex: number) => {
    e.preventDefault();
    e.stopPropagation();
    resizing.current = { colIndex, startX: e.clientX, startWidth: columnWidths[colIndex] };

    const handleMouseMove = (ev: MouseEvent) => {
      if (!resizing.current) return;
      const diff = ev.clientX - resizing.current.startX;
      const newWidth = Math.max(30, resizing.current.startWidth + diff);
      setColumnWidths((prev) => {
        const next = [...prev];
        next[resizing.current!.colIndex] = newWidth;
        return next;
      });
    };

    const handleMouseUp = () => {
      resizing.current = null;
      didResize.current = true;
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  }, [columnWidths]);

  const consumeResize = useCallback(() => {
    if (didResize.current) {
      didResize.current = false;
      return true;
    }
    return false;
  }, []);

  return { columnWidths, handleResizeStart, consumeResize };
}
