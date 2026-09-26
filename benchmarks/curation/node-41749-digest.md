# digest node-41749 : 31 anchored candidates (of 35 total)

## [4] doc/api/globals.md:342 author=benjamingr reply=false subj=line side=RIGHT cid=795081434
Any reason this can't be experimental without requiring a CLI flag? Even a warning would probably be better DX?

## [9] doc/api/globals.md:342 author=devsnek reply=true subj=line side=RIGHT cid=795081653
Given that the undici repo says this:

> This is experimental and is not yet fully compliant with the Fetch Standard. We plan to ship breaking changes to this feature until it is out of experimental.

I think flagging is a good call. Experimental with just a warning seems better for pushing adoption in non-critical projects to iron out bugs, but this seems to be a bit earlier than that.

## [10] doc/api/globals.md:342 author=aduh95 reply=true subj=line side=RIGHT cid=795081711
Landing new globals would be a semver-major change, using an experimental flag allows us to have it landed in release lines quicker.

## [5] doc/api/globals.md:371 author=benjamingr reply=false subj=line side=RIGHT cid=795081495
Any chance these could link to the relevant spec/mdn pages? (If they already do - my bad)

## [6] doc/api/globals.md:371 author=benjamingr reply=true subj=line side=RIGHT cid=795081525
(with an import rather than a global that is)

## [7] doc/api/globals.md:371 author=devsnek reply=true subj=line side=RIGHT cid=795081551
they should, that's the changes to type-parser.mjs

## [8] doc/api/globals.md:371 author=Mesteery reply=true subj=line side=RIGHT cid=795081556
I think type-parser.mjs (cf. diff) takes care of that.

## [2] doc/node.1:142 author=VoltrexKeyva reply=false subj=line side=RIGHT cid=795068913
```suggestion
.It Fl -experimental-fetch
```

## [0] lib/internal/bootstrap/pre_execution.js:19 author=aduh95 reply=false subj=line side=RIGHT cid=795062408
nit: ASCII order
```suggestion
  emitExperimentalWarning,
  exposeInterface,
```

## [1] lib/internal/bootstrap/pre_execution.js:38 author=targos reply=false subj=line side=RIGHT cid=795067183
It seems that this is not the right place to run this code if I want `fetch` to also be installed in workers.
Note that I cannot do it in `internal/bootstrap/browser.js` because it's not allowed to call `getOptionValue` from this file.

## [34] lib/internal/bootstrap/pre_execution.js:38 author=zcbenz reply=true subj=line side=RIGHT cid=795618629
I think it is fine leaving it here for now, we can surely move it to `browser.js` after the experimental flag is removed. Should we add a code comment to remind about this?

## [3] test/common/index.js:71 author=targos reply=false subj=line side=LEFT cid=795071028
Is there another way to know whether node was run with a file entry point? `require.main` doesn't exist in ESM and this change doesn't work for all tests.

## [12] test/common/index.js:71 author=targos reply=true subj=line side=LEFT cid=795146665
Thanks, but I don't think I can use this.
`test/common/index.js` is not the entry point (and it's not ESM either). import chain is:

- `node test/parallel/something.mjs` (ESM)
- `import '../common/index.mjs'` (ESM)
- `import './index.js'` (CJS)

Can I find out from `common/index.js` that the entry point was a file (ESM or CJS, I don't need to know in this case)?

Example of an entry point that is not a file:
- `node -e "require('./test/parallel/sometest.js')"`

## [28] test/common/index.js:71 author=targos reply=true subj=line side=LEFT cid=795162774
I replaced `require.main` with `fs.existsSync(process.argv[1])`

## [13] test/parallel/test-fetch.mjs:32 author=targos reply=false subj=line side=RIGHT cid=795146943
All these lines run very fast, but then on my computer the test hangs a few seconds before it exits. Could it have to do with keep-alive behavior? Can we override it?

## [14] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795148042
Try putting a breakpoint here and see if we enter the branch? https://github.com/nodejs/undici/blob/f4f1a862cc4e3c62f9025a9072b7b62b7dc6c471/lib/client.js#L839

Also - is it still connected on the server's side?

## [15] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795151065
> Also - is it still connected on the server's side?

How do I know?

Anyway, shouldn't `server.close()` close the connections?

## [16] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795151277
> Anyway, shouldn't server.close() close the connections?

https://nodejs.org/dist/latest-v17.x/docs/api/http.html#serverclosecallback

Apparently not.

## [17] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795151917
> Try putting a breakpoint here and see if we enter the branch?

Yes, we enter the branch. `client[kKeepAliveTimeoutValue]` becomes `4000`.

## [18] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795154683
Can confirm this reproduces in unidici/fetch with a simple tap test I wrote (this takes 4.4 seconds and removing the call reduces it to 400ms)

```js
"use strict";

const { test } = require("tap");

const { fetch } = require("../..");
const { createServer } = require('http')

test("validate fetch doesn't hang the process with keepalive", (t) => {
  t.plan(1)

  const obj = { asd: true }
  const server = createServer((req, res) => {
    res.end(JSON.stringify(obj))
  })
  t.teardown(server.close.bind(server))

  server.listen(0, async () => {
    const body = await fetch(`http://localhost:${server.address().port}`)
    t.strictSame(obj, await body.json())
  })
})
```

## [19] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795155381
Looks like the issue is coming from llhttp - when `wasm_on_headers_complete` is called it passes should_keep_alive as true (well 1).

Let me see if I can reproduce on raw llhttp

## [20] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795155558
Ok actually let me wait for either @ronag or @indutny to take a look https://github.com/nodejs/llhttp/issues/64 :)

## [21] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795157066
- `undici.fetch` uses the global dispatcher: https://github.com/nodejs/undici/blob/19b5789e2097c44ad1f1279cb8672062abf21030/index.js#L101, which is just an `Agent` created with default options.
- The default value for `keepAliveTimeout` is 4 seconds: https://github.com/nodejs/undici/blob/19b5789e2097c44ad1f1279cb8672062abf21030/docs/api/Client.md#new-clienturl-options

I think this is expected behavior from `undici`'s perspective. The problem is that the `fetch` API doesn't let me change it. I just want to make this test exit as soon as it is finished (without having to use `process.exit(0)` at the end).

## [22] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795157413
Workaround:

```diff
diff --git a/test/parallel/test-fetch.mjs b/test/parallel/test-fetch.mjs
index 1aff7e1fdc..de8ef36951 100644
--- a/test/parallel/test-fetch.mjs
+++ b/test/parallel/test-fetch.mjs
@@ -11,7 +11,10 @@ assert.strictEqual(typeof globalThis.Request, 'function');
 assert.strictEqual(typeof globalThis.Response, 'function');
 assert.strictEqual(typeof globalThis.Headers, 'function');
 
-const server = http.createServer((req, res) => res.end('Hello world'));
+const server = http.createServer((req, res) => {
+  res.setHeader('Keep-Alive', 'timeout=0, max=0');
+  res.end('Hello world');
+});
 server.listen(0);
 await events.once(server, 'listening');
 const port = server.address().port;
```

Is this fine?

## [23] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795159105
I think users would might find the process 'hanging' for ±4 seconds equally confusing?

Is there a way to eat the cake (use keepalive) and keep it too? (would `.unref`ing the socket when there are no active requests and `ref`ing it when there are work?)

## [26] test/parallel/test-fetch.mjs:32 author=benjamingr reply=true subj=line side=RIGHT cid=795160735
(To be clear - I am totally fine with the workaround and IMO this can be handled after the PR lands)

## [29] test/parallel/test-fetch.mjs:32 author=ronag reply=true subj=line side=RIGHT cid=795164874
I thought we handled this already. We do unref both timers and sockets. Maybe we missed one somewhere.

## [30] test/parallel/test-fetch.mjs:32 author=ronag reply=true subj=line side=RIGHT cid=795165355
Targos would you mind making an issue on undici and mention your workaround and we will have a look at it.

## [31] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795165782
I think the problem is because the server has an active ref on the socket, not the client.

## [32] test/parallel/test-fetch.mjs:32 author=targos reply=true subj=line side=RIGHT cid=795166024
Yeah, I confirmed that this (and removing the keep-alive header) also fixes the issue:

```js
server.on('connection', (socket) => socket.unref());
```

## [33] test/parallel/test-fetch.mjs:32 author=mcollina reply=true subj=line side=RIGHT cid=795166167
The problem for this is not in Undici. We handle it perfectly there.

The problem is Node.js core `http.Server` as the keepalive sockets are... kept alive, preventing Node.js from exiting. It is the same problem depicted at https://github.com/nodejs/node/issues/41578.

A solution for this is to set up a different default agent for fetch() that reduces the keepAlive significantly (100ms?). We are not aiming for maximum performance here so it might just be enough to avoid most problems for now.
