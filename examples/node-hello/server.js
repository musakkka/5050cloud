// SPDX-License-Identifier: MIT
// A tiny HTTP app used by the docs and the end-to-end suite.
const http = require("node:http");

const port = Number(process.env.PORT || 3000);
const started = new Date().toISOString();

const server = http.createServer((req, res) => {
  if (req.url === "/health") {
    res.writeHead(200, { "content-type": "text/plain" });
    res.end("ok\n");
    return;
  }
  res.writeHead(200, { "content-type": "application/json" });
  res.end(
    JSON.stringify({
      message: process.env.GREETING || "Hello from 5050cloud",
      started,
      path: req.url,
    }) + "\n",
  );
});

server.listen(port, "0.0.0.0", () => {
  console.log(`listening on 0.0.0.0:${port}`);
});

process.on("SIGTERM", () => server.close(() => process.exit(0)));
