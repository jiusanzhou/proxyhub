export default {
  async fetch(request: Request, env: any): Promise<Response> {
    const url = new URL(request.url);
    if (url.pathname === '/') {
      return new Response(JSON.stringify({ ok: true, ts: Date.now() }), {
        headers: { 'content-type': 'application/json' },
      });
    }
    if (url.pathname === '/health') {
      const hasDB = !!env.DB;
      let dbOk = false;
      let dbErr: string | null = null;
      try {
        const r = await env.DB.prepare('SELECT 1 as ok').first();
        dbOk = r?.ok === 1;
      } catch (e: any) {
        dbErr = e?.message ?? String(e);
      }
      return new Response(JSON.stringify({ hasDB, dbOk, dbErr }), {
        headers: { 'content-type': 'application/json' },
      });
    }
    return new Response('not found', { status: 404 });
  },
};
