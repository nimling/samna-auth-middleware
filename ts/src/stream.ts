export const EVENT_STREAM = 'text/event-stream'

export type StepStatus = 'running' | 'done' | 'failed'

export interface Step {
  id: string
  label: string
  parent_id?: string
  depends_on?: string[]
  status: StepStatus
  started_at: string
  ended_at?: string
  data?: unknown
  error?: string
}

export interface StreamResult<T = unknown> {
  status: number
  body: T
}

function frames(block: string): { event: string; data: string }[] {
  const out: { event: string; data: string }[] = []
  for (const chunk of block.split('\n\n')) {
    let event = 'message'
    const data: string[] = []
    for (const line of chunk.split('\n')) {
      if (line.startsWith('event: ')) event = line.slice(7)
      else if (line.startsWith('data: ')) data.push(line.slice(6))
    }
    if (data.length) out.push({ event, data: data.join('\n') })
  }
  return out
}

export async function readStream<T = unknown>(res: Response, onStep?: (step: Step) => void): Promise<StreamResult<T>> {
  const type = res.headers.get('content-type') ?? ''
  if (!type.startsWith(EVENT_STREAM) || !res.body) {
    const text = await res.text()
    return { status: res.status, body: (text ? JSON.parse(text) : null) as T }
  }
  let result: StreamResult<T> = { status: res.status, body: null as T }
  const apply = (block: string) => {
    for (const frame of frames(block)) {
      if (frame.event === 'step') onStep?.(JSON.parse(frame.data) as Step)
      else if (frame.event === 'result') result = JSON.parse(frame.data) as StreamResult<T>
    }
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })
    const cut = buffer.lastIndexOf('\n\n')
    if (cut === -1) continue
    apply(buffer.slice(0, cut + 2))
    buffer = buffer.slice(cut + 2)
  }
  if (buffer.trim()) apply(buffer)
  return result
}
