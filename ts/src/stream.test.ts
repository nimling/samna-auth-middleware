import { describe, it, expect } from 'vitest'
import { readStream, EVENT_STREAM, type Step } from './stream'

function sse(frames: string[]): Response {
  const encoder = new TextEncoder()
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      for (const frame of frames) controller.enqueue(encoder.encode(frame))
      controller.close()
    }
  })
  return new Response(body, { status: 200, headers: { 'content-type': EVENT_STREAM } })
}

describe('readStream', () => {
  it('yields each step and returns the result frame', async () => {
    const steps: Step[] = []
    const out = await readStream<{ data: string[]; count: number }>(
      sse([
        'event: step\ndata: {"id":"a","label":"A","status":"running","started_at":"2026-10-16T10:00:00Z"}\n\n',
        'event: step\ndata: {"id":"a","label":"A","status":"done","started_at":"2026-10-16T10:00:00Z","ended_at":"2026-10-16T10:00:01Z"}\n\nevent: result\ndata: {"status":201,"body":{"data":["x"],"count":1}}\n\n'
      ]),
      (step) => steps.push(step)
    )
    expect(steps.map((s) => s.status)).toEqual(['running', 'done'])
    expect(out.status).toBe(201)
    expect(out.body.count).toBe(1)
  })

  it('returns a plain json body unchanged', async () => {
    const out = await readStream<{ data: unknown[] }>(new Response('{"data":[]}', { status: 200, headers: { 'content-type': 'application/json' } }))
    expect(out.status).toBe(200)
    expect(out.body.data).toEqual([])
  })
})
