export interface SSEEvent {
  type: string;
  job_id?: string;
  total_batches?: number;
  batch_index?: number;
  committed_batch_index?: number;
  batch_summary?: any;
  cumulative_summary?: any;
  summary?: any;
  error?: string;
  data?: any;
  status?: string;
  created_at?: string;
  completed_at?: string;
  reason?: string;
  [key: string]: any;
}

interface Subscriber {
  id: string;
  writer: WritableStreamDefaultWriter<Uint8Array>;
  controller: AbortController;
}

export class SSEBroker {
  private subscribersByShop = new Map<string, Map<string, Subscriber>>();
  private encoder = new TextEncoder();

  addSubscriber(shopId: string, signal: AbortSignal): ReadableStream {
    const { readable, writable } = new TransformStream<Uint8Array, Uint8Array>();
    const writer = writable.getWriter();
    const subId = `sub-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
    const controller = new AbortController();

    if (!this.subscribersByShop.has(shopId)) {
      this.subscribersByShop.set(shopId, new Map());
    }
    const shopSubs = this.subscribersByShop.get(shopId)!;
    shopSubs.set(subId, { id: subId, writer, controller });

    // Send initial comment frame and retry directive
    const initialMsg = `retry: 3000\n: connected\n\n`;
    writer.write(this.encoder.encode(initialMsg)).catch(() => {
      this.removeSubscriber(shopId, subId);
    });

    const cleanup = () => {
      this.removeSubscriber(shopId, subId);
    };

    signal.addEventListener('abort', cleanup);
    controller.signal.addEventListener('abort', cleanup);

    return readable;
  }

  removeSubscriber(shopId: string, subId: string) {
    const shopSubs = this.subscribersByShop.get(shopId);
    if (!shopSubs) return;
    const sub = shopSubs.get(subId);
    if (sub) {
      try {
        sub.writer.close().catch(() => {});
      } catch {}
      shopSubs.delete(subId);
    }
    if (shopSubs.size === 0) {
      this.subscribersByShop.delete(shopId);
    }
  }

  publish(shopId: string, event: SSEEvent) {
    const shopSubs = this.subscribersByShop.get(shopId);
    if (!shopSubs || shopSubs.size === 0) return;

    // Both onmessage and addEventListener compatible format
    const payload = JSON.stringify(event);
    const sseMessage = `event: ${event.type}\ndata: ${payload}\n\n`;
    const encoded = this.encoder.encode(sseMessage);

    for (const [subId, sub] of shopSubs.entries()) {
      sub.writer.write(encoded).catch(() => {
        this.removeSubscriber(shopId, subId);
      });
    }
  }

  subscriberCount(shopId: string): number {
    return this.subscribersByShop.get(shopId)?.size ?? 0;
  }
}

// Global broker instance across warm isolates
export const broker = new SSEBroker();
