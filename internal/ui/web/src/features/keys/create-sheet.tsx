import { KeyRound, Loader2, X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { floatingSheetOverlay, floatingSheetWide } from '@/components/floating-sheet'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import { useCreateKey, type CreatedKey } from '@/features/keys/api'
import { BudgetPicker } from '@/features/keys/budget-picker'
import { RoutingEditor } from '@/features/keys/routing'
import {
  modeOf,
  newDraft,
  routingProblem,
  toConfigs,
  type RouteDraft,
} from '@/features/keys/routing-utils'
import { useAllCredentials } from '@/features/providers/api'
import { ExpiryPicker, Field, Group } from '@/features/providers/form'

const MIN_SAVING_MS = 600 // a local save answers in milliseconds; keep the spinner visible

function CreateForm({ onCreated }: { onCreated: (created: CreatedKey) => void }) {
  const create = useCreateKey()
  const creds = useAllCredentials()
  const [client, setClient] = useState('')
  const [budgetId, setBudgetId] = useState('')
  const [edited, setEdited] = useState<RouteDraft[] | null>(null)
  const [expiry, setExpiry] = useState<Date | null>(null)
  const [busy, setBusy] = useState(false)

  // Start with the first provider that has a credential, until the person edits the list.
  const firstProvider = [...new Set((creds.data ?? []).map((c) => c.provider))].sort()[0]
  const drafts = edited ?? (firstProvider ? [newDraft(firstProvider)] : [])

  const problem =
    client.trim() === ''
      ? 'Client name is required'
      : budgetId === ''
        ? 'Choose a budget'
        : routingProblem(drafts)

  const submit = async () => {
    if (problem) return
    setBusy(true)
    try {
      const [created] = await Promise.all([
        create.mutateAsync({
          client_id: client.trim(),
          budget_id: budgetId,
          mode: modeOf(drafts),
          load_balancer: 'round_robin', // the only balancer implemented today
          provider_configs: toConfigs(drafts),
          ...(expiry ? { expires_at: expiry.toISOString() } : {}),
        }),
        new Promise((r) => setTimeout(r, MIN_SAVING_MS)),
      ])
      toast.success('Key created')
      onCreated(created)
    } catch (err) {
      toast.error(`Could not create the key: ${err instanceof Error ? err.message : String(err)}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-5 py-5 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
        <Group label="Identity">
          <Field
            label="Client name"
            required
            hint="Shown in logs and on the Requests page. It cannot be changed later."
          >
            <Input
              value={client}
              onChange={(e) => {
                setClient(e.target.value)
              }}
              placeholder="acme-prod"
              className="h-9"
            />
          </Field>
        </Group>

        <Group label="Budget">
          <Field
            label="Budget"
            required
            hint="Each budget serves one active key. Pick an unused one, or create a new budget."
          >
            <BudgetPicker value={budgetId} onChange={setBudgetId} />
          </Field>
        </Group>

        <Group label="Providers and routing">
          <RoutingEditor
            value={drafts}
            onChange={setEdited}
            lead={
              <span className="text-[11px] text-muted-foreground">
                Traffic goes to these providers.
              </span>
            }
          />
        </Group>

        <Group label="Lifecycle">
          <Field
            label="Expires on"
            optional
            hint="Listed under Needs attention 7 days before. The key stops working after this date."
          >
            <ExpiryPicker value={expiry} onChange={setExpiry} />
          </Field>
        </Group>
      </div>

      <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3.5">
        <span className="flex-1 truncate text-[11px] text-destructive">
          {problem && client !== '' ? problem : ''}
        </span>
        <SheetClose asChild>
          <Button variant="outline" disabled={busy}>
            Cancel
          </Button>
        </SheetClose>
        <Button
          onClick={() => void submit()}
          disabled={problem !== null || busy}
          className="min-w-24"
        >
          {busy ? (
            <>
              <Loader2 className="animate-spin" />
              Creating…
            </>
          ) : (
            'Create key'
          )}
        </Button>
      </footer>
    </>
  )
}

// Create a virtual key (design 05.3). The secret comes back once, handed to `onCreated`.
export function CreateKeySheet({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (created: CreatedKey) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        showCloseButton={false}
        overlayClassName={floatingSheetOverlay}
        className={floatingSheetWide}
      >
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] border bg-muted">
            <KeyRound className="size-[22px] text-primary" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">Create key</SheetTitle>
            <SheetDescription className="text-xs">
              A client calls the gateway with this key. The secret is shown once, after you create
              it.
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>
        {/* Remount per opening so the form always starts empty. */}
        <CreateForm
          key={String(open)}
          onCreated={(created) => {
            onOpenChange(false)
            onCreated(created)
          }}
        />
      </SheetContent>
    </Sheet>
  )
}
