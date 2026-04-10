// A real HTTP server whose routing, binding, validation and middleware all run
// in Go, inside WebAssembly.
//
// Node owns the socket; Gin owns the request. WebAssembly cannot listen on a
// port -- Go's net package there is an in-process fake nothing can dial -- so
// the transport stays in Node and each request is handed to the Go handler.
//
//   node serve.mjs        then:  curl localhost:8787/api/health
import { createServer } from "node:http";
import { createClient } from "./node/dist/index.node.js";

const PORT = Number(process.env.PORT ?? 8787);

const api = await createClient();
const info = await api.start({ basePath: "/api" });

// Routes registered from JavaScript at runtime, alongside the Go ones.
await api.addRoute("GET", "/api/version", 200, JSON.stringify({ version: "1.0.0" }), "");

console.log(`gin engine started in ${info.mode} mode at ${info.basePath}`);
for (const r of await api.routes()) {
  console.log(`  ${r.method.padEnd(6)} ${r.path.padEnd(20)} (${r.source})`);
}

const server = createServer(async (req, res) => {
  const chunks = [];
  for await (const chunk of req) chunks.push(chunk);

  try {
    const response = await api.handle({
      method: req.method ?? "GET",
      path: req.url ?? "/",
      headers: Object.fromEntries(
        Object.entries(req.headers).map(([k, v]) => [k, Array.isArray(v) ? v.join(", ") : String(v)]),
      ),
      body: Buffer.concat(chunks).toString("utf8"),
    });
    res.writeHead(response.status, response.headers ?? {});
    res.end(response.body);
  } catch (err) {
    // A rejected promise here means the Go side failed, not the handler.
    res.writeHead(500, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: String(err?.message ?? err) }));
  }
});

server.listen(PORT, () => console.log(`\nlistening on http://localhost:${PORT}`));

const shutdown = async () => {
  server.close();
  await api.stop();
  await api.dispose();
  process.exit(0);
};
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);
