import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'

// Table footer (design: Pagination): a summary on the left, ‹ page of pages › on the right.
// Pages are 1-based.
export function Pager({
  summary,
  page,
  pages,
  onPage,
}: {
  summary: string
  page: number
  pages: number
  onPage: (page: number) => void
}) {
  return (
    <div className="flex items-center gap-3 rounded-xl border bg-card px-5 py-3">
      <span className="flex-1 truncate text-xs text-muted-foreground">{summary}</span>
      <div className="flex items-center gap-2.5">
        <Button
          variant="outline"
          size="icon-sm"
          aria-label="Previous page"
          disabled={page <= 1}
          onClick={() => {
            onPage(page - 1)
          }}
        >
          <ChevronLeft />
        </Button>
        <span className="font-mono text-xs font-medium tabular-nums">
          {page.toLocaleString('en-US')} of {Math.max(pages, 1).toLocaleString('en-US')}
        </span>
        <Button
          variant="outline"
          size="icon-sm"
          aria-label="Next page"
          disabled={page >= pages}
          onClick={() => {
            onPage(page + 1)
          }}
        >
          <ChevronRight />
        </Button>
      </div>
    </div>
  )
}
