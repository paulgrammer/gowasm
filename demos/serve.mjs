// A static server for the demo pages, with the headers WebAssembly needs.
//
//   node demos/serve.mjs        then open http://localhost:8080
//
// Written by hand rather than pulling in a dependency, because the only thing
// that matters here is serving .wasm as application/wasm: without it,
// WebAssembly.compileStreaming refuses the response and the generated loader
// silently falls back to the slower path.
import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, normalize } from "node:path";

const ROOT = new URL(".", import.meta.url).pathname;
const PORT = Number(process.env.PORT ?? 8080);

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".wasm": "application/wasm",
  ".map": "application/json",
  ".svg": "image/svg+xml",
};

const server = createServer(async (req, res) => {
  // Everything is served from this directory; anything trying to climb out of
  // it is refused rather than resolved.
  const url = new URL(req.url ?? "/", "http://localhost");
  let path = normalize(decodeURIComponent(url.pathname));
  if (path.includes("..")) {
    res.writeHead(403).end("forbidden");
    return;
  }
  if (path.endsWith("/")) path += "index.html";

  const file = join(ROOT, path);
  try {
    const info = await stat(file);
    if (info.isDirectory()) {
      res.writeHead(302, { Location: path + "/" }).end();
      return;
    }
    res.writeHead(200, {
      "Content-Type": TYPES[extname(file)] ?? "application/octet-stream",
      "Content-Length": info.size,
      "Cache-Control": "no-store",
    });
    createReadStream(file).pipe(res);
  } catch {
    res.writeHead(404, { "Content-Type": "text/plain" });
    res.end(`not found: ${path}\n\nRun "make demo" to build the packages these pages need.`);
  }
});

server.listen(PORT, () => {
  console.log(`demos at http://localhost:${PORT}`);
});
