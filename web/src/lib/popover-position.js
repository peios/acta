/** @param {{left:number,right:number,top:number,bottom:number,width:number}} anchor
 * @param {{width:number,height:number,left?:number,top?:number}} viewport
 * @param {{width:number,height:number,align?:'start'|'end',margin?:number,gap?:number}} options
 */
export function popoverPosition(
  anchor,
  viewport,
  { width, height, align = "start", margin = 12, gap = 8 },
) {
  const x = viewport.left ?? 0,
    y = viewport.top ?? 0;
  width = Math.max(0, Math.min(width, viewport.width - 2 * margin));
  const left = Math.max(
    x + margin,
    Math.min(
      align === "end" ? anchor.right - width : anchor.left,
      x + viewport.width - margin - width,
    ),
  );
  const below = Math.max(0, y + viewport.height - margin - anchor.bottom - gap);
  const above = Math.max(0, anchor.top - gap - y - margin);
  const flip = below < height && above > below;
  const maxHeight = Math.min(
    height,
    flip ? above : below,
    Math.max(0, viewport.height - 2 * margin),
  );
  const top = Math.max(
    y + margin,
    Math.min(
      flip ? anchor.top - gap - maxHeight : anchor.bottom + gap,
      y + viewport.height - margin - maxHeight,
    ),
  );
  return { left, top, width, maxHeight, flip };
}
