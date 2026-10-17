import { Check, ChevronDown, Plus } from 'lucide-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import {
  Command,
  CommandEmpty,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useBudgets, type Budget } from '@/features/budgets/api'
import { money, periodUnit } from '@/features/budgets/budget'
import { inputLike } from '@/features/providers/form-utils'
import { cn } from '@/lib/utils'

const label = (b: Budget) => `${money(b.budget_limit)} / ${periodUnit(b.budget_duration)}`

// Picks the budget a new key spends from. A budget serves one active key, so only unused ones
// are offered; "Create new budget" opens the Budgets screen (design 05.3a).
export function BudgetPicker({
  value,
  onChange,
}: {
  value: string
  onChange: (id: string) => void
}) {
  const budgets = useBudgets()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const free = (budgets.data ?? []).filter((b) => b.status !== 'bound')
  const chosen = (budgets.data ?? []).find((b) => b.id === value)

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={cn(inputLike, 'justify-between text-left')}>
          {chosen ? (
            <span className="truncate font-mono text-xs">
              {chosen.name} · {label(chosen)}
            </span>
          ) : (
            <span className="truncate text-muted-foreground">
              {budgets.isPending ? 'Loading budgets…' : 'Choose a budget'}
            </span>
          )}
          <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-(--radix-popover-trigger-width) p-1.5">
        <Command>
          {free.length > 5 && <CommandInput placeholder="Search budgets" />}
          <CommandList>
            <CommandEmpty className="py-4 text-sm text-muted-foreground">
              No unused budget.
            </CommandEmpty>
            {free.map((b) => (
              <CommandItem
                key={b.id}
                value={b.name}
                onSelect={() => {
                  onChange(b.id)
                  setOpen(false)
                }}
                className="gap-3"
              >
                <span className="flex-1 truncate font-mono text-xs">{b.name}</span>
                <span className="font-mono text-xs text-muted-foreground">{label(b)}</span>
                {b.id === value && <Check className="size-3.5 text-primary" />}
              </CommandItem>
            ))}
            <CommandSeparator className="my-1" />
            <CommandItem
              forceMount
              value="create-new-budget"
              onSelect={() => {
                setOpen(false)
                void navigate('/governance/budgets?new=1')
              }}
              className="gap-2 text-primary"
            >
              <Plus className="size-3.5" />
              <span className="flex-1 font-medium">Create new budget</span>
              <span className="text-[11px] text-muted-foreground">opens Budgets</span>
            </CommandItem>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}
