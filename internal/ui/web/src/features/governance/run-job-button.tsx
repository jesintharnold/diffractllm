import { Loader2 } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { useRunJob, type JobName } from '@/features/governance/api'
import { ACTIONS, JOBS } from '@/features/governance/jobs'

// "Sync now" / "Flush now" for one governance job. One toast follows the run: running, then what it
// did, or why it failed. The button stays busy until the run has finished.
export function RunJobButton({ job }: { job: JobName }) {
  const meta = JOBS[job]
  const action = ACTIONS[meta.action]
  const run = useRunJob(job)
  const Icon = action.icon

  return (
    <Button
      variant="outline"
      size="sm"
      disabled={run.isPending}
      onClick={() => {
        toast.promise(run.mutateAsync(), {
          loading: meta.running,
          success: (stats) => ({ message: meta.done, description: meta.detail(stats.last_detail) }),
          error: (err: Error) => ({ message: meta.failed, description: err.message }),
        })
      }}
    >
      {run.isPending ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : <Icon />}
      {run.isPending ? action.busy : action.label}
    </Button>
  )
}
