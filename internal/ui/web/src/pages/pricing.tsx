import { Plus } from 'lucide-react'
import { useState } from 'react'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'
import { Button } from '@/components/ui/button'
import type { Override } from '@/features/pricing/api'
import { OverrideSheet } from '@/features/pricing/override-sheet'
import { OverridesTable } from '@/features/pricing/overrides-table'

// 03 · Pricing overrides: only the custom overrides; base prices live in the Catalog.
export default function PricingPage() {
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Override | null>(null)

  return (
    <>
      <PageHeader
        title="Pricing overrides"
        actions={
          <Button
            onClick={() => {
              setEditing(null)
              setOpen(true)
            }}
          >
            <Plus />
            Add override
          </Button>
        }
      />
      <PageBody>
        <OverridesTable
          onEdit={(o) => {
            setEditing(o)
            setOpen(true)
          }}
        />
      </PageBody>
      <OverrideSheet open={open} editing={editing} onOpenChange={setOpen} />
    </>
  )
}
