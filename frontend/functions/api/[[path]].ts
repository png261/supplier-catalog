// Cloudflare Pages Function: Reverse proxy for /api/* to Cloudflare Worker
export const onRequest: PagesFunction = async (context) => {
  const url = new URL(context.request.url);
  const targetUrl = new URL(`https://supplier-catalog-api.pages.dev${url.pathname}${url.search}`);
  
  // Forward original request with method, headers, and body
  const requestInit: RequestInit = {
    method: context.request.method,
    headers: context.request.headers,
    redirect: 'follow',
  };

  if (context.request.method !== 'GET' && context.request.method !== 'HEAD') {
    requestInit.body = context.request.body;
  }

  const response = await fetch(targetUrl.toString(), requestInit);
  return response;
};
