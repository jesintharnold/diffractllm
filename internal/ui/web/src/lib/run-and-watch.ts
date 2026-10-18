const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

// Queue a background job, then watch until that run has finished. A run is "ours" once last_run_at
// moves past its value just before the click and the job is no longer running. Resolves to the state
// that settled, or null when nothing settled within giveUpMs.
export async function runAndWatch<S>({
  trigger,
  read,
  probe,
  everyMs,
  giveUpMs,
}: {
  trigger: () => Promise<unknown>
  read: () => Promise<S>
  probe: (state: S) => { running: boolean; lastRunAt?: string }
  everyMs: number
  giveUpMs: number
}): Promise<S | null> {
  const before = probe(await read()).lastRunAt
  await trigger()
  const deadline = Date.now() + giveUpMs
  while (Date.now() < deadline) {
    await sleep(everyMs)
    const state = await read()
    const { running, lastRunAt } = probe(state)
    if (!running && lastRunAt && lastRunAt !== before) return state
  }
  return null
}
