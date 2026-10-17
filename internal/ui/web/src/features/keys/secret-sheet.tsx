import { Check, CircleCheck, Copy, TriangleAlert, X } from 'lucide-react'
import { useState } from 'react'
import { floatingSheet, floatingSheetOverlay } from '@/components/floating-sheet'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import { Group } from '@/features/providers/form'

// The full secret, shown once after a key is created or rotated (design 05.3b). The page keeps it
// in state only while this is open; nothing here is stored.
export function SecretSheet({
  secret,
  title,
  subtitle,
  onClose,
}: {
  secret: string | null
  title: string
  subtitle: string
  onClose: () => void
}) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    if (!secret) return
    await navigator.clipboard.writeText(secret)
    setCopied(true)
    setTimeout(() => {
      setCopied(false)
    }, 2000)
  }

  return (
    <Sheet
      open={secret !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <SheetContent
        showCloseButton={false}
        overlayClassName={floatingSheetOverlay}
        className={floatingSheet}
      >
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-[10px] border bg-muted">
            <CircleCheck className="size-[22px] text-success" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">{title}</SheetTitle>
            <SheetDescription className="truncate text-xs">{subtitle}</SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>

        <div className="flex min-h-0 flex-1 flex-col gap-6 px-5 py-5">
          <Group label="Secret">
            <div className="flex items-center gap-3 rounded-lg border bg-muted py-3 pr-3 pl-3.5">
              <code className="min-w-0 flex-1 font-mono text-xs break-all" data-testid="secret">
                {secret}
              </code>
              <Button variant="outline" size="sm" onClick={() => void copy()}>
                {copied ? <Check /> : <Copy className="text-muted-foreground" />}
                {copied ? 'Copied' : 'Copy'}
              </Button>
            </div>
            <div className="flex gap-2.5 rounded-lg border border-warning/50 bg-muted px-3 py-2.5">
              <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warning" />
              <p className="text-[11px] leading-relaxed text-muted-foreground">
                This is the only time the full key is shown. If it is lost, rotate the secret to get
                a new one.
              </p>
            </div>
          </Group>
          <Group label="Use it">
            <code className="rounded-lg border bg-muted px-3.5 py-3 font-mono text-[11px] break-all text-muted-foreground">
              Authorization: Bearer {secret ? `${secret.slice(0, 11)}…` : ''}
            </code>
            <p className="text-[11px] text-muted-foreground">
              Send it as a bearer token on every request to the gateway.
            </p>
          </Group>
        </div>

        <footer className="flex items-center justify-end gap-2.5 border-t bg-muted px-5 py-3.5">
          <Button onClick={onClose} className="min-w-20">
            Done
          </Button>
        </footer>
      </SheetContent>
    </Sheet>
  )
}
