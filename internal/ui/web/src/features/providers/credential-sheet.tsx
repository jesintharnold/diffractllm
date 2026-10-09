import { format } from 'date-fns'
import { ChevronRight, Loader2, Lock, Plus, Trash2, TriangleAlert, X } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { floatingSheetOverlay, floatingSheetWide } from '@/components/floating-sheet'
import { ProviderLogo } from '@/components/provider-logo'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  DEFAULT_ENDPOINT,
  isSupported,
  useCreateCredential,
  useDeleteCredential,
  useUpdateCredential,
  type Alias,
  type AzureAuthMode,
  type AzureSettings,
  type Credential,
  type CredentialUpdate,
} from '@/features/providers/api'
import { issueOf } from '@/features/providers/attention'
import {
  ExpiryPicker,
  Field,
  Group,
  ModelChips,
  Segmented,
  SwitchRow,
} from '@/features/providers/form'
import { providerLabel } from '@/lib/provider-colors'

const MIN_SAVING_MS = 600 // a local save answers in milliseconds; keep the spinner visible

const AUTH_MODES = [
  ['azure_default_credential', 'Default credential'],
  ['azure_api_key', 'API key'],
  ['azure_service_principal', 'Entra ID'],
] as const

interface Deployment {
  model: string
  deploymentID: string
  protocol: 'openai' | 'anthropic'
  route: 'v1' | 'deployment'
  version: string
  open: boolean
}

const toDeployments = (aliases: Credential['aliases']): Deployment[] =>
  Object.entries(aliases ?? {}).map(([model, a]) => ({
    model,
    deploymentID: a.deploymentID,
    protocol: a.endpoint_protocol ?? 'openai',
    route: a.route_style ?? 'v1',
    version: a.api_version ?? '',
    open: false,
  }))

const toAliases = (rows: Deployment[]): Record<string, Alias> =>
  Object.fromEntries(
    rows.map((d) => [
      d.model.trim(),
      {
        deploymentID: d.deploymentID.trim(),
        endpoint_protocol: d.protocol,
        ...(d.protocol === 'openai' ? { route_style: d.route } : {}),
        ...(d.protocol === 'openai' && d.route === 'deployment'
          ? { api_version: d.version.trim() }
          : {}),
      },
    ]),
  )

const isAll = (models: string[]) => models.length === 0 || models.includes('*')
// The date input speaks local yyyy-mm-dd; a credential expires at the end of that day.
// A credential expires at the end of the chosen day, local time.
const endOfDayISO = (d: Date) => {
  const end = new Date(d)
  end.setHours(23, 59, 59, 0)
  return end.toISOString()
}

function DeploymentsTable({
  rows,
  onChange,
}: {
  rows: Deployment[]
  onChange: (rows: Deployment[]) => void
}) {
  const set = (i: number, patch: Partial<Deployment>) => {
    onChange(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)))
  }
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="grid grid-cols-[16px_1fr_1fr_130px_20px] gap-2.5 bg-muted px-3 py-2 text-[10px] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
        <span />
        <span>Model name</span>
        <span>Deployment</span>
        <span>API</span>
        <span />
      </div>
      {rows.map((d, i) => (
        <div key={i} className="border-t">
          <div className="grid grid-cols-[16px_1fr_1fr_130px_20px] items-center gap-2.5 px-3 py-2">
            <button
              type="button"
              aria-label={d.open ? 'Collapse' : 'Expand'}
              aria-expanded={d.open}
              onClick={() => {
                set(i, { open: !d.open })
              }}
              className="text-muted-foreground"
            >
              <ChevronRight
                className={`size-3.5 transition-transform duration-200 ${d.open ? 'rotate-90' : ''}`}
              />
            </button>
            <Input
              value={d.model}
              placeholder="gpt-4o"
              aria-label="Model name"
              onChange={(e) => {
                set(i, { model: e.target.value })
              }}
              className="h-8 font-mono text-xs md:text-xs"
            />
            <Input
              value={d.deploymentID}
              placeholder="deployment name"
              aria-label="Deployment"
              onChange={(e) => {
                set(i, { deploymentID: e.target.value })
              }}
              className="h-8 font-mono text-xs md:text-xs"
            />
            <span className="truncate text-[11px] text-muted-foreground">
              {d.protocol === 'anthropic' ? 'Anthropic' : `OpenAI · ${d.route}`}
            </span>
            <button
              type="button"
              aria-label={`Remove ${d.model || 'deployment'}`}
              onClick={() => {
                onChange(rows.filter((_, j) => j !== i))
              }}
              className="text-muted-foreground hover:text-destructive"
            >
              <Trash2 className="size-3.5" />
            </button>
          </div>
          {d.open && (
            <div className="flex gap-3 bg-muted px-3 pt-2 pb-3 pl-9 duration-200 ease-out animate-in fade-in-0 slide-in-from-top-1">
              <Field label="API">
                <Segmented
                  label="API"
                  value={d.protocol}
                  options={[
                    ['openai', 'OpenAI'],
                    ['anthropic', 'Anthropic'],
                  ]}
                  onChange={(protocol) => {
                    set(i, { protocol })
                  }}
                />
              </Field>
              {d.protocol === 'openai' && (
                <>
                  <Field label="Route style">
                    <Segmented
                      label="Route style"
                      value={d.route}
                      options={[
                        ['v1', 'v1'],
                        ['deployment', 'deployment'],
                      ]}
                      onChange={(route) => {
                        set(i, { route })
                      }}
                    />
                  </Field>
                  {d.route === 'deployment' && (
                    <Field label="API version" required>
                      <Input
                        value={d.version}
                        placeholder="2024-10-21"
                        onChange={(e) => {
                          set(i, { version: e.target.value })
                        }}
                        className="h-9 font-mono text-xs md:text-xs"
                      />
                    </Field>
                  )}
                </>
              )}
            </div>
          )}
        </div>
      ))}
      <button
        type="button"
        onClick={() => {
          onChange([
            ...rows,
            {
              model: '',
              deploymentID: '',
              protocol: 'openai',
              route: 'v1',
              version: '',
              open: false,
            },
          ])
        }}
        className="flex w-full items-center gap-1.5 border-t px-3 py-2.5 text-xs font-medium text-primary hover:bg-muted"
      >
        <Plus className="size-3.5" />
        Add deployment
      </button>
    </div>
  )
}

function SecretInput({
  label,
  stored,
  value,
  onChange,
  placeholder,
}: {
  label: string
  stored: boolean
  value: string | null
  onChange: (v: string | null) => void
  placeholder: string
}) {
  // value null = keep what is stored; a string = the new secret being typed.
  if (stored && value === null) {
    return (
      <Field label={label} required hint="Leave as is to keep the current one.">
        <div className="flex h-11 items-center gap-2.5 rounded-md border bg-muted px-3">
          <Lock className="size-3.5 text-success" />
          <div className="flex flex-1 flex-col">
            <span className="text-xs font-medium">Stored</span>
            <span className="text-[11px] text-muted-foreground">Encrypted at rest</span>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              onChange('')
            }}
          >
            Replace
          </Button>
        </div>
      </Field>
    )
  }
  return (
    <Field label={label} required hint="Encrypted at rest and never shown again.">
      <Input
        type="password"
        autoComplete="new-password"
        value={value ?? ''}
        placeholder={placeholder}
        onChange={(e) => {
          onChange(e.target.value)
        }}
        className="h-9 font-mono text-xs md:text-xs"
      />
    </Field>
  )
}

function CredentialForm({
  provider,
  editing,
  onDone,
}: {
  provider: string
  editing: Credential | null
  onDone: () => void
}) {
  const create = useCreateCredential()
  const update = useUpdateCredential()
  const remove = useDeleteCredential()
  const azure = provider === 'azure'
  const label = providerLabel(provider)
  const before = editing?.settings?.azure

  const [name, setName] = useState(editing?.name ?? '')
  const [endpoint, setEndpoint] = useState(
    editing?.endpoint ?? (isSupported(provider) ? DEFAULT_ENDPOINT[provider] : ''),
  )
  const [apiKey, setApiKey] = useState<string | null>(editing?.api_key ? null : '')
  const [mode, setMode] = useState<AzureAuthMode>(before?.auth_mode ?? 'azure_api_key')
  const [tenant, setTenant] = useState(before?.tenant_id ?? '')
  const [clientId, setClientId] = useState(before?.client_id ?? '')
  const [secret, setSecret] = useState<string | null>(before?.client_secret ? null : '')
  const [scopes, setScopes] = useState((before?.scopes ?? []).join(' '))
  const [deployments, setDeployments] = useState<Deployment[]>(toDeployments(editing?.aliases))
  const [allModels, setAllModels] = useState(isAll(editing?.allowed_models ?? []))
  const [allowed, setAllowed] = useState(
    isAll(editing?.allowed_models ?? []) ? [] : (editing?.allowed_models ?? []),
  )
  const [blocked, setBlocked] = useState(editing?.blocked_models ?? [])
  const [expiry, setExpiry] = useState<Date | null>(
    editing?.expires_at ? new Date(editing.expires_at) : null,
  )
  const [enabled, setEnabled] = useState(editing?.enabled ?? true)
  const [busy, setBusy] = useState<'save' | 'delete' | null>(null)
  const [confirmDelete, setConfirmDelete] = useState(false)
  // Everything the form would save; an edit saves only once something differs from the start.
  const snapshot = JSON.stringify([
    name,
    endpoint,
    apiKey,
    mode,
    tenant,
    clientId,
    secret,
    scopes,
    toAliases(deployments),
    allModels,
    allowed,
    blocked,
    expiry && format(expiry, 'yyyy-MM-dd'),
    enabled,
  ])
  const [initialSnapshot] = useState(snapshot)
  const dirty = editing === null || snapshot !== initialSnapshot

  const usesKey = !azure || mode === 'azure_api_key'
  const keyStored = Boolean(editing?.api_key)
  const secretStored =
    before?.auth_mode === 'azure_service_principal' && Boolean(before.client_secret)
  const issue = editing ? issueOf(editing) : null

  const problem = (() => {
    if (name.trim() === '') return 'Name is required'
    if (!/^https?:\/\/\S+$/.test(endpoint.trim())) return 'Endpoint must be an http(s) URL'
    if (usesKey && !(keyStored && apiKey === null) && (apiKey ?? '').trim() === '')
      return 'API key is required'
    if (azure && mode === 'azure_service_principal') {
      if (tenant.trim() === '' || clientId.trim() === '')
        return 'Tenant ID and client ID are required'
      if (!(secretStored && secret === null) && (secret ?? '').trim() === '')
        return 'Client secret is required'
    }
    if (!allModels && allowed.length === 0) return 'Pick at least one model, or allow all'
    const models = deployments.map((d) => d.model.trim())
    if (deployments.some((d) => d.model.trim() === '' || d.deploymentID.trim() === '')) {
      return 'Every deployment needs a model name and a deployment'
    }
    if (new Set(models).size !== models.length) return 'A model name is mapped twice'
    if (
      deployments.some(
        (d) => d.protocol === 'openai' && d.route === 'deployment' && d.version.trim() === '',
      )
    ) {
      return 'Deployment route style needs an API version'
    }
    return null
  })()

  const settings = (): { azure?: AzureSettings } | undefined => {
    if (!azure) return undefined
    const scopeList = scopes.split(/[\s,]+/).filter(Boolean)
    if (mode === 'azure_service_principal') {
      return {
        azure: {
          auth_mode: mode,
          tenant_id: tenant.trim(),
          client_id: clientId.trim(),
          ...(secret ? { client_secret: secret } : {}),
          ...(scopeList.length ? { scopes: scopeList } : {}),
        },
      }
    }
    if (mode === 'azure_default_credential') {
      return {
        azure: {
          auth_mode: mode,
          ...(clientId.trim() ? { client_id: clientId.trim() } : {}),
          ...(scopeList.length ? { scopes: scopeList } : {}),
        },
      }
    }
    return { azure: { auth_mode: mode } }
  }

  const save = async () => {
    if (problem) {
      toast.error(problem)
      return
    }
    const allowedModels = allModels ? ['*'] : allowed
    const aliases = toAliases(deployments)
    setBusy('save')
    try {
      let request: Promise<unknown>
      if (editing) {
        const patch: CredentialUpdate = {
          name: name.trim(),
          enabled,
          endpoint: endpoint.trim(),
          allowed_models: allowedModels,
          blocked_models: blocked,
          aliases,
          ...(azure ? { settings: settings() } : {}),
          ...(usesKey && apiKey ? { api_key: apiKey } : {}),
          ...(expiry
            ? { expires_at: endOfDayISO(expiry) }
            : editing.expires_at
              ? { clear_expiry: true }
              : {}),
        }
        request = update.mutateAsync({ provider, id: editing.id, patch })
      } else {
        request = create.mutateAsync({
          provider,
          name: name.trim(),
          enabled,
          endpoint: endpoint.trim(),
          allowed_models: allowedModels,
          blocked_models: blocked,
          aliases,
          settings: settings(),
          ...(usesKey && apiKey ? { api_key: apiKey } : {}),
          ...(expiry ? { expires_at: endOfDayISO(expiry) } : {}),
        })
      }
      await Promise.all([request, new Promise((r) => setTimeout(r, MIN_SAVING_MS))])
      toast.success(editing ? 'Credential saved' : 'Credential created')
      onDone()
    } catch (err) {
      toast.error(
        `Could not save the credential: ${err instanceof Error ? err.message : String(err)}`,
      )
    } finally {
      setBusy(null)
    }
  }

  const del = async () => {
    if (!editing) return
    if (!confirmDelete) {
      setConfirmDelete(true)
      return
    }
    setBusy('delete')
    try {
      await remove.mutateAsync({ provider, id: editing.id })
      toast.success('Credential deleted')
      onDone()
    } catch (err) {
      toast.error(
        `Could not delete the credential: ${err instanceof Error ? err.message : String(err)}`,
      )
    } finally {
      setBusy(null)
    }
  }

  return (
    <>
      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-5 py-5 [scrollbar-color:var(--border)_transparent] [scrollbar-width:thin]">
        {issue && issue.kind !== 'disabled' && (
          <div className="flex gap-2.5 rounded-lg border border-warning/50 bg-muted px-3.5 py-3">
            <TriangleAlert className="mt-0.5 size-3.5 shrink-0 text-warning" />
            <div className="flex flex-col gap-0.5">
              <span className="text-xs font-medium text-warning">{issue.title}</span>
              <span className="text-[11px] text-muted-foreground">
                Replace the {usesKey ? 'API key' : 'secret'} below, then move the expiry date
                forward.
              </span>
            </div>
          </div>
        )}

        <Group label="Identity">
          <Field
            label="Name"
            required
            hint={`Unique within ${label}. Shown in logs and the request detail.`}
          >
            <Input
              value={name}
              onChange={(e) => {
                setName(e.target.value)
              }}
              placeholder={`${provider}-prod`}
              className="h-9"
            />
          </Field>
        </Group>

        <Group label="Authentication">
          {azure && (
            <Segmented
              label="Authentication method"
              value={mode}
              options={AUTH_MODES}
              onChange={setMode}
            />
          )}
          {azure && mode === 'azure_default_credential' && (
            <p className="text-[11px] text-muted-foreground">
              Uses DefaultAzureCredential: managed identity on Azure VMs and AKS, environment
              variables, or the Azure CLI. Set a client ID only for a user-assigned identity.
            </p>
          )}
          {usesKey && (
            <SecretInput
              label="API key"
              stored={keyStored}
              value={apiKey}
              onChange={setApiKey}
              placeholder={azure ? 'Azure resource key' : 'sk-…'}
            />
          )}
          {azure && mode !== 'azure_api_key' && (
            <div className="flex gap-3">
              {mode === 'azure_service_principal' && (
                <Field label="Tenant ID" required>
                  <Input
                    value={tenant}
                    onChange={(e) => {
                      setTenant(e.target.value)
                    }}
                    className="h-9 font-mono text-xs md:text-xs"
                  />
                </Field>
              )}
              <Field
                label="Client ID"
                required={mode === 'azure_service_principal'}
                optional={mode === 'azure_default_credential'}
              >
                <Input
                  value={clientId}
                  onChange={(e) => {
                    setClientId(e.target.value)
                  }}
                  className="h-9 font-mono text-xs md:text-xs"
                />
              </Field>
            </div>
          )}
          {azure && mode === 'azure_service_principal' && (
            <SecretInput
              label="Client secret"
              stored={secretStored}
              value={secret}
              onChange={setSecret}
              placeholder="client secret"
            />
          )}
          {azure && mode !== 'azure_api_key' && (
            <Field label="Scopes" optional hint="Leave empty for the Cognitive Services default.">
              <Input
                value={scopes}
                onChange={(e) => {
                  setScopes(e.target.value)
                }}
                placeholder="https://cognitiveservices.azure.com/.default"
                className="h-9 font-mono text-xs md:text-xs"
              />
            </Field>
          )}
        </Group>

        <Group label="Endpoint">
          <Field
            label={azure ? 'Resource endpoint' : 'Endpoint'}
            required
            hint={
              azure
                ? undefined
                : `The host only; the gateway adds /v1/… itself. Change it for a regional or proxied endpoint.`
            }
          >
            <Input
              value={endpoint}
              onChange={(e) => {
                setEndpoint(e.target.value)
              }}
              placeholder={
                azure ? 'https://your-resource.openai.azure.com' : 'https://api.openai.com'
              }
              className="h-9 font-mono text-xs md:text-xs"
            />
          </Field>
        </Group>

        {azure && (
          <Group label="Deployments">
            <p className="text-[11px] text-muted-foreground">
              Map the model name clients send to the deployment name in your Azure resource.
            </p>
            <DeploymentsTable rows={deployments} onChange={setDeployments} />
          </Group>
        )}

        <Group label="Models">
          <Segmented
            label="Models"
            value={allModels ? 'all' : 'specific'}
            options={[
              ['all', 'All models'],
              ['specific', 'Specific models'],
            ]}
            onChange={(v) => {
              setAllModels(v === 'all')
            }}
          />
          {!allModels && (
            <Field label="Allowed models" required>
              <ModelChips
                provider={provider}
                value={allowed}
                onChange={setAllowed}
                placeholder={`Search ${label} models to add`}
              />
            </Field>
          )}
          <Field label="Blocked models" optional>
            <ModelChips
              provider={provider}
              value={blocked}
              onChange={setBlocked}
              placeholder={`Search ${label} models to block`}
            />
          </Field>
        </Group>

        <Group label="Lifecycle">
          <Field
            label="Expires on"
            optional
            hint="Listed under Needs attention 7 days before. Routing stops using it after this date."
          >
            <ExpiryPicker value={expiry} onChange={setExpiry} />
          </Field>
          <SwitchRow
            title="Enabled"
            detail={
              editing
                ? 'Turn off to pause it. Routing skips a disabled credential; nothing is deleted.'
                : 'Routing uses this credential as soon as it is saved.'
            }
            checked={enabled}
            onChange={setEnabled}
          />
        </Group>

        {editing && (
          <Group label="Danger zone" tone="bad">
            <div className="flex items-center gap-3 rounded-lg border border-destructive/50 bg-muted px-3.5 py-3">
              <div className="flex flex-1 flex-col gap-0.5">
                <span className="text-[13px] font-medium">Delete credential</span>
                <span className="text-[11px] text-muted-foreground">
                  Removes it from routing immediately. To pause it instead, turn Enabled off.
                </span>
              </div>
              <Button
                variant="destructive"
                disabled={busy !== null}
                onClick={() => void del()}
                onBlur={() => {
                  setConfirmDelete(false)
                }}
              >
                {busy === 'delete' && <Loader2 className="animate-spin" />}
                {confirmDelete ? 'Confirm delete' : 'Delete'}
              </Button>
            </div>
          </Group>
        )}
      </div>

      <footer className="flex items-center gap-2.5 border-t bg-muted px-5 py-3.5">
        <span className="flex-1 truncate text-[11px] text-destructive">
          {problem && name !== '' ? problem : ''}
        </span>
        <SheetClose asChild>
          <Button variant="outline" disabled={busy !== null}>
            Cancel
          </Button>
        </SheetClose>
        <Button
          onClick={() => void save()}
          disabled={!dirty || problem !== null || busy !== null}
          className="min-w-28"
        >
          {busy === 'save' ? (
            <>
              <Loader2 className="animate-spin" />
              Saving…
            </>
          ) : editing ? (
            'Save changes'
          ) : (
            'Create credential'
          )}
        </Button>
      </footer>
    </>
  )
}

// Add or edit a credential (design 04.3 / 04.3b / 04.4); the fields follow the provider.
export function CredentialSheet({
  provider,
  editing,
  open,
  onOpenChange,
}: {
  provider: string
  editing: Credential | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        showCloseButton={false}
        overlayClassName={floatingSheetOverlay}
        className={floatingSheetWide}
      >
        <header className="flex items-center gap-3 border-b px-5 py-4">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-lg border bg-muted">
            <ProviderLogo provider={provider} className="size-6" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <SheetTitle className="text-base font-semibold">
              {editing ? 'Edit credential' : 'Add credential'}
            </SheetTitle>
            <SheetDescription className="truncate text-xs">
              {editing
                ? `${providerLabel(provider)} · ${editing.name}`
                : `${providerLabel(provider)} · routing rotates through it alongside the other enabled credentials`}
            </SheetDescription>
          </div>
          <SheetClose asChild>
            <Button variant="ghost" size="icon-sm" aria-label="Close">
              <X />
            </Button>
          </SheetClose>
        </header>
        {/* Remount per opening so the form always starts from the saved values. */}
        <CredentialForm
          key={`${String(open)}-${provider}-${editing?.id ?? 'new'}`}
          provider={provider}
          editing={editing}
          onDone={() => {
            onOpenChange(false)
          }}
        />
      </SheetContent>
    </Sheet>
  )
}
