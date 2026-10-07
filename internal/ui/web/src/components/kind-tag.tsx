// The bordered type tag (design: KindTag), one neutral colour for every kind.
// image_generation → IMAGE, image_edit → IMAGE EDIT; the CSS uppercases it.
const label = (kind: string) =>
  kind ? kind.replace(/_generation$/, '').replaceAll('_', ' ') : 'unknown'

export function KindTag({ kind }: { kind: string }) {
  return (
    <span className="inline-flex items-center rounded-[4px] border border-foreground/25 bg-foreground/[0.08] px-2 py-[3px] text-[11px] leading-[15px] font-semibold tracking-[0.05em] whitespace-nowrap text-foreground uppercase">
      {label(kind)}
    </span>
  )
}
