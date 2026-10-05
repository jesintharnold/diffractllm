import { Link } from 'react-router'
import { PageBody } from '@/components/layout/page-body'
import { PageHeader } from '@/components/layout/page-header'

export default function NotFoundPage() {
  return (
    <>
      <PageHeader title="Page not found" />
      <PageBody className="block text-sm text-muted-foreground">
        Nothing lives at this address.{' '}
        <Link to="/" viewTransition className="text-primary underline-offset-4 hover:underline">
          Back to Overview
        </Link>
      </PageBody>
    </>
  )
}
