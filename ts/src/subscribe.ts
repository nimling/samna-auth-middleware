import { getBffPath } from './session'

/**
 * Adds an address to a feed through the BFF, which holds the app's subscribe
 * client credentials. The reader needs no account and no secret reaches the
 * browser. The prophet applies the per app per IP limit the client's claim
 * carries, and answers 429 when it is spent.
 */
export async function postSubscription(email: string, feed: string): Promise<void> {
  const url = `${getBffPath()}/subscribe?email=${encodeURIComponent(email)}`
  const res = await fetch(url, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/rss+xml' },
    body: feed
  })
  if (!res.ok) {
    throw new Error(`subscription request returned ${res.status}`)
  }
}
