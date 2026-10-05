import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'

// Stand-in for routes whose screen is not built yet; the sidebar link still lands somewhere real.
export default function ComingSoonPage({ title }: { title: string }) {
  return (
    <>
      <PageHeader title={title} />
      <PageBody className="text-sm text-muted-foreground">This screen is not built yet.</PageBody>
    </>
  )
}
