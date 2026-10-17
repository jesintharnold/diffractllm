import { Loader2, Lock, Trash2, Wallet, X } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { floatingSheet, floatingSheetOverlay } from '@/components/floating-sheet'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  useCreateBudget,
  useDeleteBudget,
  useUpdateBudget,
  type Budget,
  type BudgetPatch,
} from '@/features/budgets/api'
import {
  WINDOW_OPTIONS,
  dollarsText,
  draftOf,
  durationOf,
  isEnforced,
  parseUSD,
  windowHint,
  type CustomUnit,
  type WindowDraft,
} from '@/features/budgets/budget'
import { Field, Group, Segmented } from '@/features/providers/form'
import { ApiError } from '@/lib/api'
import { formatInZone, useTimezone } from '@/lib/timezone'
import { cn } from '@/lib/utils'

const MIN_SAVING_MS = 600 // a local save answers in milliseconds; keep the spinner visible

// A label above a control that is not a single input, so clicking the label picks nothing.
function Block({ label, hint, children }: { label: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs text-muted-foreground">
        {label}
        <span className="text-destructive"> *</span>
      </span>
      {children}
      {hint && <span className="text-[11px] text-muted-foreground">{hint}</span>}
    </div>
  )
}

function OptionCard({
  on,
  title,
  detail,
  onSelect,
}: {
  on: boolean
  title: string
  detail: string
  onSelect: () => void
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={on}
      onClick={onSelect}
      className={cn(
        'flex items-start gap-3 rounded-lg border bg-muted px-3.5 py-3 text-left transition-colors',
        on ? 'border-primary' : 'hover:border-ring/60',
      )}
    >
      <span
        className={cn(
          'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full border-[1.5px]',
          on ? 'border-primary' : 'border-muted-foreground',
        )}
      >
        {on && <span className="size-2 rounded-full bg-primary" />}
      </span>
      <span className="flex flex-col gap-0.5">
        <span className="text-xs font-semibold">{title}</span>
        <span className="text-[11px] text-muted-foreground">{detail}</span>
      </span>
    </button>
  )
}

function BudgetForm({
  budget,
  boundClient,
  onSaved,
  onDeleted,
  onClose,
}: {
  budget: Budget | null
  boundClient?: string
  onSaved?: (saved: Budget) => void
  onDeleted?: () => void
  onClose: () => void
}) {
  const create = useCreateBudget()
  const update = useUpdateBudget()
  const remove = useDeleteBudget()
  const { zone } = useTimezone()
  const [name, setName] = useState(budget?.name ?? '')
  const [limit, setLimit] = useState(budget ? dollarsText(budget.budget_limit) : '')
  const [initialWindow] = useState<WindowDraft>(() =>
    budget ? draftOf(budget.budget_duration) : { kind: 'daily', count: '', unit: 'h' },
  )
  const [win, setWin] = useState<WindowDraft>(initialWindow)
  const [enforce, setEnforce] = useState(budget ? isEnforced(budget) : true)
  const [busy, setBusy] = useState<'save' | 'delete' | null>(null)
  const [armed, setArmed] = useState(false)
  const [nameTaken, setNameTaken] = useState(false)

  const nano = parseUSD(limit)
  const duration = durationOf(win)
  const limitError = limit.trim() !== '' && nano === null ? 'Enter an amount above $0.' : undefined
  const problem =
    budget === null && name.trim() === ''
      ? 'Name is required'
      : nano === null
        ? 'Enter a spend limit above $0'
        : duration === null
          ? 'Enter a window between 1 hour and 365 days'
          : null

  // An edit sends only what changed; nothing changed means nothing to save.
  const patch: BudgetPatch = {}
  if (budget && nano !== null && nano !== budget.budget_limit) patch.budget_limit = nano
  if (budget && duration !== null && JSON.stringify(win) !== JSON.stringify(initialWindow)) {
    patch.budget_duration = duration
  }
  if (budget && enforce !== isEnforced(budget)) patch.enforce = enforce
  const dirty = budget === null || Object.keys(patch).length > 0

  const save = async () => {
    if (problem || nano === null || duration === null) return
    setBusy('save')
    try {
      const [saved] = await Promise.all([
        budget
          ? update.mutateAsync({ id: budget.id, patch })
          : create.mutateAsync({
              name: name.trim(),
              budget_limit: nano,
              budget_duration: duration,
              enforce,
            }),
        new Promise((r) => setTimeout(r, MIN_SAVING_MS)),
      ])
      toast.success(budget ? 'Budget saved' : 'Budget created')
      onSaved?.(saved)
      onClose()
    } catch (err) {
      if (budget === null && err instanceof ApiError && err.status === 409) setNameTaken(true)
      else
        toast.error(
          `Could not save the budget: ${err instanceof Error ? err.message : String(err)}`,
        )
    } finally {
      setBusy(null)
    }
  }

  const del = () => {
    if (!budget) return
    if (!armed) {
      setArmed(true)
      return
    }
    setBusy('delete')
    remove.mutate(budget.id, {
      onSuccess: () => {
        toast.success(`${budget.name} deleted`)
        onDeleted?.()
        onClose()
      },
      onError: (err) => {
        setBusy(null)
        setArmed(false)
        toast.error(`Could not delete the budget: ${err.message}`)
      },
    })
  }

  const bound = budget?.status === 'bound'
  const started = budget ? formatInZone(new Date(budget.last_budget_refresh_at), zone) : undefined

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-[22px] overflow-y-auto px-5 py-5 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
        <Group label="Identity">
          <Field
            label="Name"
            required={budget === null}
            hint={budget ? 'The name cannot be changed.' : 'Unique. It cannot be changed later.'}
            error={nameTaken ? 'A budget with this name already exists.' : undefined}
          >
            <div className="relative">
              {budget && (
                <Lock
                  className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
                  aria-hidden
                />
              )}
              <Input
                value={name}
                disabled={budget !== null}
                onChange={(e) => {
                  setName(e.target.value)
                  setNameTaken(false)
                }}
                placeholder="e.g. acme-prod"
                aria-invalid={nameTaken}
                className={cn('h-9 font-mono text-xs md:text-xs', budget && 'pl-8')}
              />
            </div>
          </Field>
        </Group>

        <Group label="Limit">
          <div className="flex flex-col gap-4">
            <Field
              label="Spend limit"
              required
              hint="The most this budget can spend in one window."
              error={limitError}
            >
              <div className="relative">
                <span className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 font-mono text-xs text-muted-foreground">
                  $
                </span>
                <Input
                  value={limit}
                  onChange={(e) => {
                    setLimit(e.target.value)
                  }}
                  inputMode="decimal"
                  placeholder="0.00"
                  aria-invalid={limitError !== undefined}
                  className="h-9 pr-12 pl-7 font-mono text-xs md:text-xs"
                />
                <span className="pointer-events-none absolute top-1/2 right-3 -translate-y-1/2 text-[11px] text-muted-foreground">
                  USD
                </span>
              </div>
            </Field>

            <Block label="Window" hint={windowHint(win, budget ? started : undefined)}>
              <Segmented
                label="Window"
                value={win.kind}
                options={WINDOW_OPTIONS}
                onChange={(kind) => {
                  setWin({ ...win, kind })
                }}
              />
              {win.kind === 'custom' && (
                <div className="flex gap-2">
                  <Input
                    value={win.count}
                    onChange={(e) => {
                      setWin({ ...win, count: e.target.value })
                    }}
                    inputMode="numeric"
                    placeholder="6"
                    aria-label="Window length"
                    aria-invalid={win.count !== '' && duration === null}
                    className="h-9 w-28 font-mono text-xs md:text-xs"
                  />
                  <Select
                    value={win.unit}
                    onValueChange={(unit) => {
                      setWin({ ...win, unit: unit as CustomUnit })
                    }}
                  >
                    <SelectTrigger className="h-9 flex-1" aria-label="Window unit">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="h">hours</SelectItem>
                      <SelectItem value="d">days</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            </Block>
          </div>
        </Group>

        <Group label="Enforcement">
          <div className="flex flex-col gap-2" role="radiogroup" aria-label="Enforcement">
            <OptionCard
              on={enforce}
              title="Enforced"
              detail="Requests are blocked once spend reaches the limit. They work again when the window resets."
              onSelect={() => {
                setEnforce(true)
              }}
            />
            <OptionCard
              on={!enforce}
              title="Not enforced"
              detail="Spend is tracked, but requests are never blocked."
              onSelect={() => {
                setEnforce(false)
              }}
            />
          </div>
        </Group>

        {budget && (
          <Group label="Danger zone" tone="bad">
            <div className="flex items-center gap-3 rounded-lg border border-destructive/40 bg-muted px-3.5 py-3">
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="text-xs font-semibold">Delete budget</span>
                <span className="text-[11px] text-muted-foreground">
                  {bound
                    ? `${boundClient ?? 'A key'} uses this budget. Disable or delete that key first.`
                    : 'Removes the budget and its spend counter. Past spend stays in Requests.'}
                </span>
              </span>
              <Button
                variant={armed ? 'destructive' : 'outline'}
                disabled={bound || busy !== null}
                onClick={del}
                onBlur={() => {
                  setArmed(false)
                }}
                className={
                  armed ? '' : 'border-destructive/60 text-destructive hover:text-destructive'
                }
              >
                {busy === 'delete' ? <Loader2 className="animate-spin" /> : <Trash2 />}
                {armed ? 'Confirm delete' : 'Delete'}
              </Button>
            </div>
          </Group>
        )}
      </div>

      <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3.5">
        <span className="flex-1 truncate text-[11px] text-destructive">
          {problem && (budget !== null || name !== '') ? problem : ''}
        </span>
        <SheetClose asChild>
          <Button variant="outline" disabled={busy !== null}>
            Cancel
          </Button>
        </SheetClose>
        <Button
          onClick={() => void save()}
          disabled={problem !== null || !dirty || busy !== null}
          className="min-w-28"
        >
          {busy === 'save' ? (
            <>
              <Loader2 className="animate-spin" />
              {budget ? 'Saving…' : 'Creating…'}
            </>
          ) : budget ? (
            'Save changes'
          ) : (
            'Create budget'
          )}
        </Button>
      </footer>
    </>
  )
}

// Create a budget (design 06.3) or edit one (06.4). `boundClient` names the key using it, so the
// Delete button can say why it is off.
export function BudgetSheet({
  open,
  onOpenChange,
  budget,
  boundClient,
  onSaved,
  onDeleted,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  budget: Budget | null
  boundClient?: string
  onSaved?: (saved: Budget) => void
  onDeleted?: () => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        showCloseButton={false}
        overlayClassName={floatingSheetOverlay}
        className={floatingSheet}
      >
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] border bg-muted">
            <Wallet className="size-[22px] text-primary" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">
              {budget ? 'Edit budget' : 'Create budget'}
            </SheetTitle>
            <SheetDescription className="truncate text-xs">
              {budget
                ? budget.name
                : 'A spend limit for one key. Assign it when you create the key.'}
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>
        {/* Remount per opening (and per budget) so the form always starts from what is stored. */}
        <BudgetForm
          key={`${String(open)}:${budget?.id ?? 'new'}`}
          budget={budget}
          boundClient={boundClient}
          onSaved={onSaved}
          onDeleted={onDeleted}
          onClose={() => {
            onOpenChange(false)
          }}
        />
      </SheetContent>
    </Sheet>
  )
}
