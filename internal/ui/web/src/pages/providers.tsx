import { ChevronRight, KeyRound, Plus, TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { ProviderLogo } from '@/components/provider-logo'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DESCRIPTIONS,
  SUPPORTED,
  useAllCredentials,
  useProviders,
  type Credential,
  type SupportedProvider,
} from '@/features/providers/api'
import { issueOf } from '@/features/providers/attention'
import { useClock } from '@/lib/clock'
import { providerLabel } from '@/lib/provider-colors'

// The providers this build can set up: supported here and backed by a registered adapter.
function useAvailable() {
  const providers = useProviders()
  const rows = providers.data ?? []
  const available = SUPPORTED.filter((p) => rows.some((r) => r.name === p && r.adapter_enabled))
  return { available, loading: providers.isPending }
}

// A provider's mark in a tile: 'md' in lists and menus, 'lg' on the provider cards.
function LogoBox({ provider, size = 'md' }: { provider: string; size?: 'md' | 'lg' }) {
  return (
    <span
      className={
        size === 'lg'
          ? 'flex size-[46px] shrink-0 items-center justify-center rounded-xl bg-background'
          : 'flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted'
      }
    >
      <ProviderLogo provider={provider} className={size === 'lg' ? 'size-7' : 'size-6'} />
    </span>
  )
}

// "Add provider" (design 04.1b): pick one and its page opens, where credentials are added.
function ProviderPicker({ onPick }: { onPick: (p: SupportedProvider) => void }) {
  const [open, setOpen] = useState(false)
  const { available } = useAvailable()
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button>
          <Plus />
          Add provider
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-[380px] p-1.5">
        <Command>
          <CommandInput placeholder="Search providers" />
          <CommandList className="mt-1">
            <CommandEmpty className="py-5 text-sm text-muted-foreground">
              No provider matches.
            </CommandEmpty>
            {available.map((p) => (
              <CommandItem
                key={p}
                value={p}
                keywords={[providerLabel(p)]}
                onSelect={() => {
                  setOpen(false)
                  onPick(p)
                }}
                className="gap-3 rounded-md px-2.5 py-2.5"
              >
                <LogoBox provider={p} />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="text-sm font-semibold">{providerLabel(p)}</span>
                  <span className="truncate text-xs text-muted-foreground">{DESCRIPTIONS[p]}</span>
                </span>
              </CommandItem>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function Attention({ creds }: { creds: Credential[] }) {
  const items = creds.flatMap((c) => {
    const issue = issueOf(c)
    return issue ? [{ c, issue }] : []
  })
  if (items.length === 0) return null
  return (
    <section className="overflow-hidden rounded-xl border border-warning/60 bg-card duration-300 animate-in fade-in-0">
      <header className="flex items-center gap-2 border-b px-4 py-3">
        <TriangleAlert className="size-3.5 text-warning" />
        <h2 className="text-[13px] font-semibold">Needs attention</h2>
        <span className="font-mono text-xs font-semibold text-warning">{items.length}</span>
      </header>
      {items.map(({ c, issue }) => (
        <Link
          key={c.id}
          to={`/models/api-keys/${c.provider}`}
          className="flex items-center gap-3 border-b px-4 py-2.5 transition-colors duration-200 last:border-b-0 hover:bg-nav-selected"
        >
          <ProviderLogo provider={c.provider} />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="truncate text-xs font-medium">{issue.title}</span>
            <span className="truncate text-[11px] text-muted-foreground">
              {providerLabel(c.provider)} · {issue.detail}
            </span>
          </span>
          <span className="text-[11px] font-medium text-primary">Review →</span>
        </Link>
      ))}
    </section>
  )
}

// A configured provider (design 04.1): its mark and name, left-aligned; the whole card opens it.
// Hover only changes colour: a card that moves would be clipped by the scrolling grid.
function ProviderCard({ provider }: { provider: string }) {
  return (
    <Link
      to={`/models/api-keys/${provider}`}
      className="flex h-[78px] min-w-0 items-center gap-3.5 rounded-2xl bg-card px-[18px] transition-colors duration-200 ease-out hover:bg-nav-selected focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <LogoBox provider={provider} size="lg" />
      <span className="min-w-0 truncate text-base font-semibold">{providerLabel(provider)}</span>
    </Link>
  )
}

// 04.0: nothing configured yet. Picking a provider opens its page.
function FirstProvider({ onPick }: { onPick: (p: SupportedProvider) => void }) {
  const { available } = useAvailable()
  return (
    <div className="flex flex-1 items-center justify-center duration-300 animate-in fade-in-0">
      <section className="flex w-full max-w-[720px] flex-col items-center gap-5 rounded-xl border bg-card p-7">
        <span className="flex size-11 items-center justify-center rounded-lg border border-primary/30 bg-primary/10 text-primary">
          <KeyRound className="size-5" />
        </span>
        <div className="flex flex-col items-center gap-1.5 text-center">
          <h2 className="text-lg font-semibold">Connect your first provider</h2>
          <p className="text-xs text-muted-foreground">
            Pick where your models run. Its page opens next, where you add a credential.
          </p>
        </div>
        <div className="grid w-full grid-cols-1 gap-2.5 sm:grid-cols-2">
          {available.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => {
                onPick(p)
              }}
              className="group flex items-center gap-3 rounded-lg border bg-muted p-3.5 text-left transition-colors duration-200 ease-out hover:border-primary hover:bg-nav-selected"
            >
              <LogoBox provider={p} />
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-sm font-semibold">{providerLabel(p)}</span>
                <span className="truncate text-xs text-muted-foreground">{DESCRIPTIONS[p]}</span>
              </span>
              <ChevronRight className="size-4 text-muted-foreground transition-transform duration-200 group-hover:translate-x-0.5" />
            </button>
          ))}
        </div>
        <p className="text-[11px] text-muted-foreground">
          <span className="font-semibold text-primary">1 Choose provider</span> › 2 Add credential ›
          3 Send a request
        </p>
      </section>
    </div>
  )
}

// 04 · Model Providers: what needs attention, then one card per configured provider.
export default function ProvidersPage() {
  const creds = useAllCredentials()
  const { loading } = useAvailable()
  const navigate = useNavigate()
  useClock(60_000) // keeps "expires in 3 days" current
  const open = (p: SupportedProvider) => void navigate(`/models/api-keys/${p}`)

  const all = creds.data ?? []
  const configured = [...new Set(all.map((c) => c.provider))].sort()

  return (
    <>
      <PageHeader title="Model Providers" actions={<ProviderPicker onPick={open} />} />
      <PageBody>
        {creds.isPending || loading ? (
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {Array.from({ length: 2 }, (_, i) => (
              <Skeleton key={i} className="h-[78px] rounded-2xl" />
            ))}
          </div>
        ) : creds.isError ? (
          <p className="text-sm text-destructive">
            Could not load credentials: {creds.error.message}
          </p>
        ) : configured.length === 0 ? (
          <FirstProvider onPick={open} />
        ) : (
          <>
            <Attention creds={all} />
            <h2 className="text-[11px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
              Configured providers · {configured.length}
            </h2>
            {/* The 4px padding and matching negative margin keep borders and focus rings from being clipped by the scroll area. */}
            <div className="-m-1 grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 min-h-0 flex-1 content-start overflow-y-auto p-1 duration-300 animate-in fade-in-0">
              {configured.map((p) => (
                <ProviderCard key={p} provider={p} />
              ))}
            </div>
          </>
        )}
      </PageBody>
    </>
  )
}
