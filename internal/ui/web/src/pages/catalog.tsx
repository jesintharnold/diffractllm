import { RefreshCw, Settings } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'
import { toast } from 'sonner'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useCatalogSummary, useSyncCatalog } from '@/features/catalog/api'
import { ModelsTab } from '@/features/catalog/models-tab'
import { ProvidersTab } from '@/features/catalog/providers-tab'
import { CatalogSettingsSheet } from '@/features/catalog/settings-sheet'
import { SummaryStrip } from '@/features/catalog/summary-strip'
import { cn } from '@/lib/utils'

const TABS = [
  { value: 'providers', label: 'Providers' },
  { value: 'models', label: 'Models' },
] as const
type Tab = (typeof TABS)[number]['value']

// One underline that slides between the tabs instead of jumping (design 02.1 / 02.2).
function useIndicator(active: Tab) {
  const list = useRef<HTMLDivElement>(null)
  const [box, setBox] = useState({ left: 0, width: 0 })
  useLayoutEffect(() => {
    const el = list.current?.querySelector<HTMLElement>(`[data-value="${active}"]`)
    if (el) setBox({ left: el.offsetLeft, width: el.offsetWidth })
  }, [active])
  return { list, box }
}

// 02 · Model catalog: the summary strip once at the top, then Providers and Models.
export default function CatalogPage() {
  const [tab, setTab] = useState<Tab>('providers')
  const [settingsOpen, setSettingsOpen] = useState(false)
  const summary = useCatalogSummary()
  const sync = useSyncCatalog()
  const { list, box } = useIndicator(tab)
  const syncing = sync.isPending || Boolean(summary.data?.syncing)

  return (
    <>
      <PageHeader
        title="Model catalog"
        actions={
          <>
            <Button
              variant="outline"
              onClick={() => {
                setSettingsOpen(true)
              }}
            >
              <Settings className="text-muted-foreground" />
              Catalog settings
            </Button>
            <Button
              variant="outline"
              disabled={syncing}
              onClick={() => {
                sync.mutate(undefined, {
                  onSuccess: () => toast.success('Catalog sync started'),
                  onError: (err) => toast.error(`Could not start the sync: ${err.message}`),
                })
              }}
            >
              <RefreshCw
                className={cn('text-muted-foreground', syncing && 'animate-spin motion-reduce:animate-none')}
              />
              Sync now
            </Button>
          </>
        }
      />
      <PageBody>
        <SummaryStrip />
        <Tabs
          value={tab}
          onValueChange={(v) => {
            setTab(v as Tab)
          }}
          className="gap-4"
        >
          <TabsList
            ref={list}
            variant="line"
            className="relative h-auto w-full justify-start gap-1 border-b p-0"
          >
            {TABS.map((t) => (
              <TabsTrigger
                key={t.value}
                value={t.value}
                data-value={t.value}
                className="h-10 flex-none px-3.5 text-[13px] after:hidden data-active:text-primary dark:data-active:text-primary"
              >
                {t.label}
              </TabsTrigger>
            ))}
            <span
              aria-hidden
              className="absolute -bottom-px h-0.5 bg-primary transition-[left,width] duration-300 ease-[cubic-bezier(0.22,1,0.36,1)] motion-reduce:transition-none"
              style={{ left: box.left, width: box.width }}
            />
          </TabsList>
          <TabsContent value="providers" className="animate-in duration-300 fade-in-0">
            <ProvidersTab />
          </TabsContent>
          <TabsContent value="models" className="animate-in duration-300 fade-in-0">
            <ModelsTab />
          </TabsContent>
        </Tabs>
      </PageBody>
      <CatalogSettingsSheet open={settingsOpen} onOpenChange={setSettingsOpen} />
    </>
  )
}
