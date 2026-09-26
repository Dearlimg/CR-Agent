# Real-PR 参考评论版本对齐审核工作表

对每条参考回答一个问题：评论抱怨的代码在“评论时 hunk”里存在，在“最终快照”里是否仍然如此？

status 取值：valid（仍成立的缺陷类）| suggestion（仍成立的非缺陷建议）| fixed_in_snapshot（要求的修改已在最终 diff 中）| unverifiable（diff 内无法验证）

## ts-57465 #0 (cid=1508184266) src/compiler/checker.ts:15486 [suggestion/correctness]

- status: 
- audit_note: 

**评论**：

> Change `===` check to `signature.resolvedReturnType.flags & TypeFlags.Boolean`. We generally want to avoid checking for specific _instances_ of types and instead check for specific _kinds_ of types.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `===`：final_added=false, comment_hunk=true
- `signature.resolvedReturnType.flags & TypeFlags.Boolean`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -15458,9 +15458,19 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
                         jsdocPredicate = getTypePredicateOfSignature(jsdocSignature);
                     }
                 }
-                signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
-                    createTypePredicateFromTypePredicateNode(type, signature) :
-                    jsdocPredicate || noTypePredicate;
+                if (type || jsdocPredicate) {
+                    signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
+                        createTypePredicateFromTypePredicateNode(type, signature) :
+                        jsdocPredicate || noTypePredicate;
+                }
+                else if (signature.declaration && isFunctionLikeDeclaration(signature.declaration) && (!signature.resolvedReturnType || signature.resolvedReturnType === booleanType)) {
+                    const { declaration } = signature;
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
-                   signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
-                       createTypePredicateFromTypePredicateNode(type, signature) :
-                       jsdocPredicate || noTypePredicate;
+15481                 if (type || jsdocPredicate) {
+15482                     signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
+15483                         createTypePredicateFromTypePredicateNode(type, signature) :
+15484                         jsdocPredicate || noTypePredicate;
+15485                 }
+15486                 else if (signature.declaration && isFunctionLikeDeclaration(signature.declaration) && (!signature.resolvedReturnType || signature.resolvedReturnType.flags & TypeFlags.Boolean) && getParameterCount(signature) > 0) {
+15487                     const { declaration } = signature;
+15488                     signature.resolvedTypePredicate = noTypePredicate; // avoid infinite loop
+15489                     signature.resolvedTypePredicate = getTypePredicateFromBody(declaration) || noTypePredicate;
+15490                 }
+15491                 else {
+15492                     signature.resolvedTypePredicate = noTypePredicate;
+15493                 }
 15494             }

```

---

## ts-57465 #1 (cid=1523687507) src/compiler/checker.ts:15486 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> We should check the signature's `getParameterCount` instead of the raw declaration here, if possible - as is, this PR infers a predicate `f is never` for `const a = (...f: []) => typeof f === "undefined";` which, while true, is kinda funky and not useful. More tests with rest args are probably pertinent, too.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `getParameterCount`：final_added=false, comment_hunk=false
- `f is never`：final_added=false, comment_hunk=false
- `const a = (...f: []) => typeof f === "undefined";`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -37389,6 +37399,72 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
         }
     }
 
+    function getTypePredicateFromBody(func: FunctionLikeDeclaration): TypePredicate | undefined {
+        switch (func.kind) {
+            case SyntaxKind.Constructor:
+            case SyntaxKind.GetAccessor:
+            case SyntaxKind.SetAccessor:
+                return undefined;
+        }
+        const functionFlags = getFunctionFlags(func);
+        if (functionFlags !== FunctionFlags.Normal || func.parameters.length === 0) return undefined;
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
-                   signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
-                       createTypePredicateFromTypePredicateNode(type, signature) :
-                       jsdocPredicate || noTypePredicate;
+15481                 if (type || jsdocPredicate) {
+15482                     signature.resolvedTypePredicate = type && isTypePredicateNode(type) ?
+15483                         createTypePredicateFromTypePredicateNode(type, signature) :
+15484                         jsdocPredicate || noTypePredicate;
+15485                 }
+15486                 else if (signature.declaration && isFunctionLikeDeclaration(signature.declaration) && (!signature.resolvedReturnType || signature.resolvedReturnType.flags & TypeFlags.Boolean) && getParameterCount(signature) > 0) {
+15487                     const { declaration } = signature;
+15488                     signature.resolvedTypePredicate = noTypePredicate; // avoid infinite loop
+15489                     signature.resolvedTypePredicate = getTypePredicateFromBody(declaration) || noTypePredicate;
+15490                 }
+15491                 else {
+15492                     signature.resolvedTypePredicate = noTypePredicate;
+15493                 }
 15494             }

```

---

## ts-57465 #2 (cid=1507922928) src/compiler/checker.ts:37492 [suggestion/correctness]

- status: 
- audit_note: 

**评论**：

> Use `unescapeLeadingUnderscores(param.name)`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `unescapeLeadingUnderscores(param.name)`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -37389,6 +37399,81 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
         }
     }
 
+    function getTypePredicateFromBody(func: FunctionLikeDeclaration, _sig: Signature): TypePredicate | undefined {
+        const functionFlags = getFunctionFlags(func);
+        if (functionFlags !== FunctionFlags.Normal) return undefined;
+
+        // Only attempt to infer a type predicate if there's exactly one return.
+        let singleReturn: Expression | undefined;
+        if (func.body && func.body.kind !== SyntaxKind.Block) {
+            singleReturn = func.body; // arrow function
+        }
+        else {
+            if (functionHasImplicitReturn(func)) return undefined;
+
+            const bailedEarly = forEachReturnStatement(func.body as Block, returnStatement => {
+                if (singleReturn || !returnStatement.expression) return true;
+                singleReturn = returnStatement.expression;
+            });
+            if (bailedEarly || !singleReturn) return undefined;
+        }
+
+        const predicate = checkIfExpressionRefinesAnyParameter(singleReturn);
+        if (predicate) {
+            const [i, type] = predicate;
+            const param = func.parameters[i];
+            if (isIdentifier(param.name)) {
+                // TODO: is there an alternative to the "as string" here? (It's __String)
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
+37484         return forEach(func.parameters, (param, i) => {
+37485             const initType = getTypeOfSymbol(param.symbol);
+37486             if (!initType || initType.flags & TypeFlags.Boolean || !isIdentifier(param.name) || isSymbolAssigned(param.symbol) || isRestParameter(param)) {
+37487                 // Refining "x: boolean" to "x is true" or "x is false" isn't useful.
+37488                 return;
+37489             }
+37490             const trueType = checkIfExpressionRefinesParameter(func, expr, param, initType);
+37491             if (trueType) {
+37492                 return createTypePredicate(TypePredicateKind.Identifier, unescapeLeadingUnderscores(param.name.escapedText), i, trueType);
+37493             }
+37494         });
+37495     }
+37496 
+37497     function checkIfExpressionRefinesParameter(func: FunctionLikeDeclaration, expr: Expression, param: ParameterDeclaration, initType: Type): Type | undefined {
+37498         const antecedent = (expr as Expression & { flowNode?: FlowNode; }).flowNode ||
+37499             expr.parent.kind === SyntaxKind.ReturnStatement && (expr.parent as ReturnStatement).flowNode ||
+37500             { flags: FlowFlags.Start };

```

---

## ts-57465 #3 (cid=1508177902) src/compiler/checker.ts:37458 [design/design]

- status: 
- audit_note: 

**评论**：

> This isn't quite right. It really isn't meaningful to perform control flow analysis on an expression that wasn't assigned a flow node in the binder because without it you have no antecedent chain. The one notable exception may be the expression of an arrow function where you know there's no preceding code. I think the best approach here is to have the binder always initialize the `flowNode` property of `return` statements and then use that as the antecedent when analyzing expressions of (single) return statements. That would allow you to get rid of the single return statement exclusion logic you have below.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `flowNode`：final_added=false, comment_hunk=false
- `return`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -37389,6 +37399,95 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
         }
     }
 
+    function getTypePredicateFromBody(func: FunctionLikeDeclaration): TypePredicate | undefined {
+        switch (func.kind) {
+            case SyntaxKind.Constructor:
+            case SyntaxKind.GetAccessor:
+            case SyntaxKind.SetAccessor:
+                return undefined;
+        }
+        const functionFlags = getFunctionFlags(func);
+        if (functionFlags !== FunctionFlags.Normal || func.parameters.length === 0) return undefined;
+
+        // Only attempt to infer a type predicate if there's exactly one return.
+        let singleReturn: Expression | undefined;
+        let singleReturnStatement: ReturnStatement | undefined;
+        if (func.body && func.body.kind !== SyntaxKind.Block) {
+            singleReturn = func.body; // arrow function
+        }
+        else {
+            if (functionHasImplicitReturn(func)) return undefined;
+
+            const bailedEarly = forEachReturnStatement(func.body as Block, returnStatement => {
+                if (singleReturn || !returnStatement.expression) return true;
+                singleReturnStatement = returnStatement;
+                singleReturn = returnStatement.expression;
+            });
+            if (bailedEarly || !singleReturn) return undefined;
+        }
+
+        const predicate = checkIfExpressionRefinesAnyParameter(singleReturn);
+        if (predicate) {
+            const [i, type] = predicate;
+            const param = func.parameters[i];
+            if (isIdentifier(param.name)) {
+                return createTypePredicate(TypePredicateKind.Identifier, unescapeLeadingUnderscores(param.name.escapedText), i, type);
+     …(截断)
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
 37451         }
 37452     }
 37453 
+37454     function getTypePredicateFromBody(func: FunctionLikeDeclaration): TypePredicate | undefined {
+37455         switch (func.kind) {
+37456             case SyntaxKind.Constructor:
+37457             case SyntaxKind.GetAccessor:
+37458             case SyntaxKind.SetAccessor:
+37459                 return undefined;
+37460         }
+37461         const functionFlags = getFunctionFlags(func);
+37462         if (functionFlags !== FunctionFlags.Normal) return undefined;
+37463 
+37464         // Only attempt to infer a type predicate if there's exactly one return.
+37465         let singleReturn: Expression | undefined;
+37466         if (func.body && func.body.kind !== SyntaxKind.Block) {

```

---

## ts-57465 #4 (cid=1508186184) src/compiler/checker.ts:37475 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> Just change to `!(falseSubtype.flags & TypeFlags.Never)`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `!(falseSubtype.flags & TypeFlags.Never)`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -37389,6 +37399,95 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
         }
     }
 
+    function getTypePredicateFromBody(func: FunctionLikeDeclaration): TypePredicate | undefined {
+        switch (func.kind) {
+            case SyntaxKind.Constructor:
+            case SyntaxKind.GetAccessor:
+            case SyntaxKind.SetAccessor:
+                return undefined;
+        }
+        const functionFlags = getFunctionFlags(func);
+        if (functionFlags !== FunctionFlags.Normal || func.parameters.length === 0) return undefined;
+
+        // Only attempt to infer a type predicate if there's exactly one return.
+        let singleReturn: Expression | undefined;
+        let singleReturnStatement: ReturnStatement | undefined;
+        if (func.body && func.body.kind !== SyntaxKind.Block) {
+            singleReturn = func.body; // arrow function
+        }
+        else {
+            if (functionHasImplicitReturn(func)) return undefined;
+
+            const bailedEarly = forEachReturnStatement(func.body as Block, returnStatement => {
+                if (singleReturn || !returnStatement.expression) return true;
+                singleReturnStatement = returnStatement;
+                singleReturn = returnStatement.expression;
+            });
+            if (bailedEarly || !singleReturn) return undefined;
+        }
+
+        const predicate = checkIfExpressionRefinesAnyParameter(singleReturn);
+        if (predicate) {
+            const [i, type] = predicate;
+            const param = func.parameters[i];
+            if (isIdentifier(param.name)) {
+                return createTypePredicate(TypePredicateKind.Identifier, unescapeLeadingUnderscores(param.name.escapedText), i, type);
+     …(截断)
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
+37467             singleReturn = func.body; // arrow function
+37468         }
+37469         else {
+37470             const bailedEarly = forEachReturnStatement(func.body as Block, returnStatement => {
+37471                 if (singleReturn || !returnStatement.expression) return true;
+37472                 singleReturn = returnStatement.expression;
+37473             });
+37474             if (bailedEarly || !singleReturn || functionHasImplicitReturn(func)) return undefined;
+37475         }
+37476         return checkIfExpressionRefinesAnyParameter(func, singleReturn);
+37477     }
+37478 
+37479     function checkIfExpressionRefinesAnyParameter(func: FunctionLikeDeclaration, expr: Expression): TypePredicate | undefined {
+37480         expr = skipParentheses(expr, /*excludeJSDocTypeAssertions*/ true);
+37481         const returnType = checkExpressionCached(expr);
+37482         if (!(returnType.flags & TypeFlags.Boolean)) return undefined;
+37483 

```

---

## ts-57465 #5 (cid=1525225501) src/compiler/checker.ts:48584 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> I've just realized this is basically identical to code both in `typePredicateToString` and `signatureToSignatureDeclarationHelper` - rather than adding a 3rd instance, can we expose a `typePredicateToTypePredicateNode` on the `NodeBuilder` and use it in all 3?

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `typePredicateToString`：final_added=false, comment_hunk=false
- `signatureToSignatureDeclarationHelper`：final_added=false, comment_hunk=false
- `typePredicateToTypePredicateNode`：final_added=false, comment_hunk=false
- `NodeBuilder`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -48500,6 +48576,18 @@ export function createTypeChecker(host: TypeCheckerHost): TypeChecker {
             return factory.createToken(SyntaxKind.AnyKeyword) as KeywordTypeNode;
         }
         const signature = getSignatureFromDeclaration(signatureDeclaration);
+        const typePredicate = getTypePredicateOfSignature(signature);
+        if (typePredicate) {
+            // Inferred type predicates
+            const assertsModifier = typePredicate.kind === TypePredicateKind.AssertsThis || typePredicate.kind === TypePredicateKind.AssertsIdentifier ?
+                factory.createToken(SyntaxKind.AssertsKeyword) :
+                undefined;
+            const parameterName = typePredicate.kind === TypePredicateKind.Identifier || typePredicate.kind === TypePredicateKind.AssertsIdentifier ?
+                setEmitFlags(factory.createIdentifier(typePredicate.parameterName), EmitFlags.NoAsciiEscaping) :
+                factory.createThisTypeNode();
+            const typeNode = typePredicate.type && nodeBuilder.typeToTypeNode(typePredicate.type, enclosingDeclaration, flags | NodeBuilderFlags.MultilineObjectLiterals, tracker);
+            return factory.createTypePredicateNode(assertsModifier, parameterName, typeNode);
```

**最终快照锚点区**：

```diff
--- src/compiler/checker.ts @@
 48578             return factory.createToken(SyntaxKind.AnyKeyword) as KeywordTypeNode;
 48579         }
 48580         const signature = getSignatureFromDeclaration(signatureDeclaration);
+48581         const typePredicate = getTypePredicateOfSignature(signature);
+48582         if (typePredicate) {
+48583             // Inferred type predicates
+48584             return nodeBuilder.typePredicateToTypePredicateNode(typePredicate, enclosingDeclaration, flags | NodeBuilderFlags.MultilineObjectLiterals, tracker);
+48585         }
 48586         return nodeBuilder.typeToTypeNode(getReturnTypeOfSignature(signature), enclosingDeclaration, flags | NodeBuilderFlags.MultilineObjectLiterals, tracker);
 48587     }
 48588 

```

---

## node-41749 #0 (cid=795067183) lib/internal/bootstrap/pre_execution.js:38 [design/design]

- status: 
- audit_note: 

**评论**：

> It seems that this is not the right place to run this code if I want `fetch` to also be installed in workers. Note that I cannot do it in `internal/bootstrap/browser.js` because it's not allowed to call `getOptionValue` from this file.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `fetch`：final_added=false, comment_hunk=false
- `internal/bootstrap/browser.js`：final_added=false, comment_hunk=false
- `getOptionValue`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -30,6 +35,7 @@ function prepareMainThreadExecution(expandArgv1 = false) {
   setupPerfHooks();
   setupInspectorHooks();
   setupWarningHandler();
+  setupFetch();
```

**最终快照锚点区**：

```diff
--- lib/internal/bootstrap/pre_execution.js @@
 35   setupPerfHooks();
 36   setupInspectorHooks();
 37   setupWarningHandler();
+38   setupFetch();
 39 
 40   // Resolve the coverage directory to an absolute path, and
 41   // overwrite process.env so that the original path gets passed

```

---

## node-41749 #1 (cid=795146943) test/parallel/test-fetch.mjs:32 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> All these lines run very fast, but then on my computer the test hangs a few seconds before it exits. Could it have to do with keep-alive behavior? Can we override it?

**评论时 diff hunk**：

```diff
@@ -0,0 +1,27 @@
+// Flags: --experimental-fetch --no-warnings
+
+import '../common/index.mjs';
+
+import assert from 'assert';
+import events from 'events';
+import http from 'http';
+
+assert.strictEqual(typeof globalThis.fetch, 'function');
+assert.strictEqual(typeof globalThis.Request, 'function');
+assert.strictEqual(typeof globalThis.Response, 'function');
+assert.strictEqual(typeof globalThis.Headers, 'function');
+
+const server = http.createServer((req, res) => res.end('Hello world'));
+server.listen(0);
+await events.once(server, 'listening');
+const port = server.address().port;
+
+const response = await fetch(`http://localhost:${port}`);
+
+assert(response instanceof Response);
+assert.strictEqual(response.status, 200);
+assert.strictEqual(response.statusText, 'OK');
+const body = await response.text();
+assert.strictEqual(body, 'Hello world');
+
+server.close();
```

**最终快照锚点区**：

```diff
--- test/parallel/test-fetch.mjs @@
+24 const response = await fetch(`http://localhost:${port}`);
+25 
+26 assert(response instanceof Response);
+27 assert.strictEqual(response.status, 200);
+28 assert.strictEqual(response.statusText, 'OK');
+29 const body = await response.text();
+30 assert.strictEqual(body, 'Hello world');
+31 
+32 server.close();

```

---

## node-41749 #2 (cid=795081434) doc/api/globals.md:342 [design/design]

- status: 
- audit_note: 

**评论**：

> Any reason this can't be experimental without requiring a CLI flag? Even a warning would probably be better DX?

**评论时 diff hunk**：

```diff
@@ -333,6 +333,17 @@ A browser-compatible implementation of the `EventTarget` class. See
 
 This variable may appear to be global but is not. See [`exports`][].
 
+## `fetch`
+
+<!-- YAML
+added: REPLACEME
+-->
+
+> Stability: 1 - Experimental. Enable this API with the [`--experimental-fetch`][]
```

**最终快照锚点区**：

```diff
--- doc/api/globals.md @@
 334 This variable may appear to be global but is not. See [`exports`][].
 335 
+336 ## `fetch`
+337 
+338 <!-- YAML
+339 added: REPLACEME
+340 -->
+341 
+342 > Stability: 1 - Experimental. Enable this API with the [`--experimental-fetch`][]
+343 > CLI flag.
+344 
+345 A browser-compatible implementation of the [`fetch()`][] function.
+346 
 347 ## `global`
 348 
 349 <!-- YAML

```

---

## gin-2632 #0 (cid=694639407) context_test.go:1433 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> If we trust proxy `40.40.40.40`, but not trust `30.30.30.30` (proxy it is or not), then ClientIP should be `30.30.30.30` as `20.20.20.20` was set by somebody untrusted. Please do not forget that `X-Forwarded-For` is appended, so it should be processed right-to-left: right-most IP address is the IP address of the most recent proxy and the left-most IP address is the IP address of the originating client.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `40.40.40.40`：final_added=false, comment_hunk=true
- `30.30.30.30`：final_added=false, comment_hunk=true
- `20.20.20.20`：final_added=false, comment_hunk=true
- `X-Forwarded-For`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -1416,6 +1415,74 @@ func TestContextClientIP(t *testing.T) {
 	// no port
 	c.Request.RemoteAddr = "50.50.50.50"
 	assert.Empty(t, c.ClientIP())
+
+	// Tests exercising the TrustedProxies functionality
+	resetContextForClientIPTests(c)
+
+	// No trusted proxies
+	c.engine.TrustedProxies = []string{}
+	c.engine.RemoteIPHeaders = []string{"X-Forwarded-For"}
+	assert.Equal(t, "40.40.40.40", c.ClientIP())
+
+	// Last proxy is trusted, but the RemoteAddr is not
+	c.engine.TrustedProxies = []string{"30.30.30.30"}
+	assert.Equal(t, "40.40.40.40", c.ClientIP())
+
+	// Only trust RemoteAddr
+	c.engine.TrustedProxies = []string{"40.40.40.40"}
+	assert.Equal(t, "20.20.20.20", c.ClientIP())
```

**最终快照锚点区**：

```diff
--- context_test.go @@
+1425 	assert.Equal(t, "40.40.40.40", c.ClientIP())
+1426 
+1427 	// Last proxy is trusted, but the RemoteAddr is not
+1428 	c.engine.TrustedProxies = []string{"30.30.30.30"}
+1429 	assert.Equal(t, "40.40.40.40", c.ClientIP())
+1430 
+1431 	// Only trust RemoteAddr
+1432 	c.engine.TrustedProxies = []string{"40.40.40.40"}
+1433 	assert.Equal(t, "20.20.20.20", c.ClientIP())
+1434 
+1435 	// All steps are trusted
+1436 	c.engine.TrustedProxies = []string{"40.40.40.40", "30.30.30.30", "20.20.20.20"}
+1437 	assert.Equal(t, "20.20.20.20", c.ClientIP())
+1438 
+1439 	// Use CIDR
+1440 	c.engine.TrustedProxies = []string{"40.40.25.25/16", "30.30.30.30"}
+1441 	assert.Equal(t, "20.20.20.20", c.ClientIP())

```

---

## gin-2632 #1 (cid=694643333) context.go:756 [design/design]

- status: 
- audit_note: 

**评论**：

> Do you expect that HTTP proxy running on `c.RemoteIP()` resets `X-Forwarded-For`? Because if it appends, then we can't inherit trustiness of `c.RemoteIP()` to all other proxies.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `c.RemoteIP()`：final_added=false, comment_hunk=false
- `X-Forwarded-For`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -746,11 +744,46 @@ func (c *Context) ClientIP() string {
 		}
 	}
 
-	if ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr)); err == nil {
-		return ip
+	if c.shouldCheckIPHeaders() {
+		for _, cidr := range c.engine.trustedCIDRs {
+			if cidr.Contains(remoteIP) {
+				for _, headerName := range c.engine.RemoteIPHeaders {
+					ip, valid := validateHeader(c.requestHeader(headerName))
+					if valid {
+						return ip
+					}
+				}
+			}
```

**最终快照锚点区**：

```diff
--- context.go @@
 751 		}
 752 	}
+753 	return remoteIP.String()
+754 }
 755 
-   	if c.engine.AppEngine {
-   		if addr := c.requestHeader("X-Appengine-Remote-Addr"); addr != "" {
-   			return addr
+756 // RemoteIP parses the IP from Request.RemoteAddr, normalizes and returns the IP (without the port).
+757 // It also checks if the remoteIP is a trusted proxy or not.
+758 // In order to perform this validation, it will see if the IP is contained within at least one of the CIDR blocks
+759 // defined in Engine.TrustedProxies
+760 func (c *Context) RemoteIP() (net.IP, bool) {
+761 	ip, _, err := net.SplitHostPort(strings.TrimSpace(c.Request.RemoteAddr))
+762 	if err != nil {
+763 		return nil, false
+764 	}

```

---

## gin-2767 #0 (cid=659322066) tree.go:413 [suggestion/readability]

- status: 
- audit_note: 

**评论**：

> these names are a little too short and cryptic for my taste. mind making them longer? (i.e. `recordIndex`)

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `recordIndex`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -405,11 +406,30 @@ type nodeValue struct {
 // made if a handle exists with an extra (without the) trailing slash for the
 // given path.
 func (n *node) getValue(path string, params *Params, unescape bool) (value nodeValue) {
-	var skipped *skip
+	var (
+		skipped *skip
+		nroot   = &(*n)                                // not found `level 1 router` use nroot
+		ldi     = len(strings.Split(path, "/")[1]) + 1 // level 1 router '/' index
+		ri      int                                    //record index
```

**最终快照锚点区**：

```diff
--- tree.go @@
+405 	// level 2 router:123
+406 	// level 3 router:def
+407 	var (
+408 		skippedPath string
+409 		latestNode  = n // not found `level 2 router` use latestNode
+410 
+411 		// match '/' count
+412 		// matchNum < 1: `level 2 router` not found,the current node needs to be equal to latestNode
+413 		// matchNum >= 1: `level (2 or 3 or 4 or ...) router`: Normal handling
+414 		matchNum int // each match will accumulate
+415 	)
+416 	//if path == "/", no need to look for tree node
+417 	if len(path) == 1 {
+418 		matchNum = 1
+419 	}
 420 
 421 walk: // Outer loop for walking the tree

```

---

## gin-2767 #1 (cid=664093314) tree.go:570 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> // level 2 router not found and latestNode.wildChild is true

**评论时 diff hunk**：

```diff
@@ -535,6 +567,10 @@ walk: // Outer loop for walking the tree
 		}
 
 		if path == prefix {
+			// level 2 router not found and latestNode.wildChild is ture
```

**最终快照锚点区**：

```diff
--- tree.go @@
 567 		}
 568 
 569 		if path == prefix {
+570 			// level 2 router not found and latestNode.wildChild is true
+571 			if matchNum < 1 && latestNode.wildChild {
+572 				n = latestNode.children[len(latestNode.children)-1]
+573 			}
 574 			// We should have reached the node containing the handle.
 575 			// Check if this node has a handle registered.
 576 			if value.handlers = n.handlers; value.handlers != nil {

```

---

## gin-2767 #2 (cid=663143713) tree_test.go:162 [test/testing]

- status: 
- audit_note: 

**评论**：

> can you add more routes with multiple params? I also want to confirm something like this: `/something/:paramname/thirdthing` and `/something/secondthing/test`. If someone does `GET /something/secondthing/thirdthing` that should have a 404, because `GET /something/secondthing` is explicitly created, it gets its own tree and won't ever fall back to `/something/:paramname`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `/something/:paramname/thirdthing`：final_added=false, comment_hunk=false
- `/something/secondthing/test`：final_added=false, comment_hunk=false
- `GET /something/secondthing/thirdthing`：final_added=false, comment_hunk=false
- `GET /something/secondthing`：final_added=false, comment_hunk=false
- `/something/:paramname`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -154,6 +154,12 @@ func TestTreeWildcard(t *testing.T) {
 		"/info/:user/public",
 		"/info/:user/project/:project",
 		"/info/:user/project/golang",
+		"/aa/*xx",
+		"/ab/*xx",
+		"/:cc",
+		"/:cc/cc",
+		"/get/test/abc/",
+		"/get/:param/abc/",
```

**最终快照锚点区**：

```diff
--- tree_test.go @@
 154 		"/info/:user/public",
 155 		"/info/:user/project/:project",
 156 		"/info/:user/project/golang",
+157 		"/aa/*xx",
+158 		"/ab/*xx",
+159 		"/:cc",
+160 		"/:cc/cc",
+161 		"/:cc/:dd/ee",
+162 		"/:cc/:dd/:ee/ff",
+163 		"/:cc/:dd/:ee/:ff/gg",
+164 		"/:cc/:dd/:ee/:ff/:gg/hh",
+165 		"/get/test/abc/",
+166 		"/get/:param/abc/",
+167 		"/something/:paramname/thirdthing",
+168 		"/something/secondthing/test",
 169 	}
 170 	for _, route := range routes {

```

---

## pandas-34473 #0 (cid=441965192) pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> This goes back to L1636. CPython manages a global error and we never explicitly clear it on that line. You could call `PyErr_Clear()` but it actually would be better if you used `PyLong_AsLongLongAndOverflow` and checked for a non-zero overflow.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `PyErr_Clear()`：final_added=false, comment_hunk=false
- `PyLong_AsLongLongAndOverflow`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -1080,6 +1087,39 @@ void encode(JSOBJ obj, JSONObjectEncoder *enc, const char *name,
         }
 
         case JT_UTF8: {
+            printf("case: JT_UTF8\n");
+            value = enc->getStringValue(obj, &tc, &szlen);
+            Buffer_Reserve(enc, RESERVE_STRING(szlen));
+            if (enc->errorMsg) {
+                enc->endTypeContext(obj, &tc);
+                return;
+            }
+            Buffer_AppendCharUnchecked(enc, '\"');
+
+            if (enc->forceASCII) {
+                if (!Buffer_EscapeStringValidated(obj, enc, value,
+                                                  value + szlen)) {
+                    enc->endTypeContext(obj, &tc);
+                    enc->level--;
+                    return;
+                }
+            } else {
+                if (!Buffer_EscapeStringUnvalidated(enc, value,
+                                                    value + szlen)) {
+                    enc->endTypeContext(obj, &tc);
+                    enc->level--;
+                    return;
+                }
+            }
+
+            Buffer_AppendCharUnchecked(enc, '\"');
+            break;
+        }
+
+        case JT_BIGNUM: {
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/lib/ultrajsonenc.c @@
+1111         case JT_BIGNUM: {
+1112             value = enc->getBigNumStringValue(obj, &tc, &szlen);
+1113 
+1114             Buffer_Reserve(enc, RESERVE_STRING(szlen));
+1115             if (enc->errorMsg) {
+1116                 enc->endTypeContext(obj, &tc);
+1117                 return;
+1118             }
+1119 
+1120             if (enc->forceASCII) {
+1121                 if (!Buffer_EscapeStringValidated(obj, enc, value,
+1122                                                   value + szlen)) {
+1123                     enc->endTypeContext(obj, &tc);
+1124                     enc->level--;
+1125                     return;
+1126                 }
+1127             } else {

```

---

## pandas-34473 #1 (cid=436401921) pandas/_libs/src/ujson/python/objToJSON.c:2136 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> Make sure you Py_DECREF these objects when they are no longer needed or else this will leak memory.

**评论时 diff hunk**：

```diff
@@ -2126,6 +2126,18 @@ double Object_getDoubleValue(JSOBJ Py_UNUSED(obj), JSONTypeContext *tc) {
     return GET_TC(tc)->doubleValue;
 }
 
+const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+                                    size_t *_outLen) {
+    PyObject* repr = PyObject_Repr(obj);
+    PyObject* str = PyUnicode_AsEncodedString(repr, "utf-8", "~E~");
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/python/objToJSON.c @@
 2130     return GET_TC(tc)->doubleValue;
 2131 }
 2132 
+2133 const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+2134                                     size_t *_outLen) {
+2135     PyObject* repr = PyObject_Str(obj);
+2136     const char *str = PyUnicode_AsUTF8AndSize(repr, (Py_ssize_t *) _outLen);
+2137     char* bytes = PyObject_Malloc(*_outLen + 1);
+2138     memcpy(bytes, str, *_outLen + 1);
+2139     GET_TC(tc)->cStr = bytes;
+2140 
+2141     Py_DECREF(repr);
+2142     
+2143     return GET_TC(tc)->cStr;
+2144 }

```

---

## pandas-34473 #2 (cid=437870883) pandas/_libs/src/ujson/python/objToJSON.c:2135 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> If this object gets released would also free bytes, so need to memcpy first. Also just use Py_DECREF for these as simple enough to see lifecycle.

**评论时 diff hunk**：

```diff
@@ -2126,6 +2126,20 @@ double Object_getDoubleValue(JSOBJ Py_UNUSED(obj), JSONTypeContext *tc) {
     return GET_TC(tc)->doubleValue;
 }
 
+const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+                                    size_t *_outLen) {
+    PyObject* repr = PyObject_Str(obj);
+    PyObject* str = PyUnicode_AsEncodedString(repr, "utf-8", "~E~");
+    char *bytes = PyBytes_AS_STRING(str);
+
+    Py_XDECREF(repr);
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/python/objToJSON.c @@
 2130     return GET_TC(tc)->doubleValue;
 2131 }
 2132 
+2133 const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+2134                                     size_t *_outLen) {
+2135     PyObject* repr = PyObject_Str(obj);
+2136     const char *str = PyUnicode_AsUTF8AndSize(repr, (Py_ssize_t *) _outLen);
+2137     char* bytes = PyObject_Malloc(*_outLen + 1);
+2138     memcpy(bytes, str, *_outLen + 1);
+2139     GET_TC(tc)->cStr = bytes;
+2140 
+2141     Py_DECREF(repr);
+2142     
+2143     return GET_TC(tc)->cStr;

```

---

## pandas-34473 #3 (cid=436401611) pandas/_libs/src/ujson/python/objToJSON.c:2133 [suggestion/correctness]

- status: 
- audit_note: 

**评论**：

> You will want PyUnicode_AsUTF8AndSize here. You can pass outLen as the second argument.

**评论时 diff hunk**：

```diff
@@ -2126,6 +2126,18 @@ double Object_getDoubleValue(JSOBJ Py_UNUSED(obj), JSONTypeContext *tc) {
     return GET_TC(tc)->doubleValue;
 }
 
+const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+                                    size_t *_outLen) {
+    PyObject* repr = PyObject_Repr(obj);
+    PyObject* str = PyUnicode_AsEncodedString(repr, "utf-8", "~E~");
+    char *bytes = PyBytes_AS_STRING(str);
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/python/objToJSON.c @@
 2130     return GET_TC(tc)->doubleValue;
 2131 }
 2132 
+2133 const char *Object_getBigNumStringValue(JSOBJ obj, JSONTypeContext *tc, 
+2134                                     size_t *_outLen) {
+2135     PyObject* repr = PyObject_Str(obj);
+2136     const char *str = PyUnicode_AsUTF8AndSize(repr, (Py_ssize_t *) _outLen);
+2137     char* bytes = PyObject_Malloc(*_outLen + 1);
+2138     memcpy(bytes, str, *_outLen + 1);
+2139     GET_TC(tc)->cStr = bytes;
+2140 
+2141     Py_DECREF(repr);

```

---

## pandas-34473 #4 (cid=442585616) pandas/_libs/src/ujson/python/objToJSON.c:1639 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> Bytes is assigned to cStr but you won't free it in this branch. We need an extra branch that checks for the JT_BIGNUM type and frees cStr in that case.

**评论时 diff hunk**：

```diff
@@ -2107,6 +2114,7 @@ void Object_endTypeContext(JSOBJ Py_UNUSED(obj), JSONTypeContext *tc) {
         GET_TC(tc)->columnLabels = NULL;
 
         PyObject_Free(GET_TC(tc)->cStr);
+        free(bytes);
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/python/objToJSON.c @@
+1634         int err;
+1635         err = (GET_TC(tc)->longValue == -1) && PyErr_Occurred();
 1636 
-           exc = PyErr_Occurred();
-   
-           if (exc && PyErr_ExceptionMatches(PyExc_OverflowError)) {
+1637         if (overflow){
+1638             PRINTMARK();
+1639             tc->type = JT_BIGNUM;
+1640         }
+1641         else if (err) {
 1642             PRINTMARK();
 1643             goto INVALID;
 1644         }
-   
+1645         
 1646         return;

```

---

## pandas-34473 #5 (cid=442356287) pandas/_libs/src/ujson/python/objToJSON.c:1632 [suggestion/readability]

- status: 
- audit_note: 

**评论**：

> No need to prepend an underscore here; may be confusing to authors who expect that to mean something. (`int overflow = 0;`)

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `int overflow = 0;`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -1629,14 +1629,20 @@ void Object_beginTypeContext(JSOBJ _obj, JSONTypeContext *tc) {
     if (PyLong_Check(obj)) {
         PRINTMARK();
         tc->type = JT_LONG;
-        GET_TC(tc)->longValue = PyLong_AsLongLong(obj);
-
+        int _overflow = 0;
```

**最终快照锚点区**：

```diff
--- pandas/_libs/src/ujson/python/objToJSON.c @@
 1629     if (PyLong_Check(obj)) {
 1630         PRINTMARK();
 1631         tc->type = JT_LONG;
-           GET_TC(tc)->longValue = PyLong_AsLongLong(obj);
+1632         int overflow = 0;
+1633         GET_TC(tc)->longValue = PyLong_AsLongLongAndOverflow(obj, &overflow);
+1634         int err;
+1635         err = (GET_TC(tc)->longValue == -1) && PyErr_Occurred();
 1636 
-           exc = PyErr_Occurred();
-   
-           if (exc && PyErr_ExceptionMatches(PyExc_OverflowError)) {
+1637         if (overflow){

```

---

## tokio-4652 #0 (cid=867329811) tokio-util/src/sync/cancellation_token/tree_node.rs:185 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> The potential parent might not match and be `Some` here. We shouldn't keep the previous parent locked in that case.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `Some`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -0,0 +1,341 @@
+//! GENERAL NOTES
+//! -------------
+//!
+//! ** Invariants **
+//!
+//! Those invariants shall be true at any time, and are required to prove correctness
+//! of the CancellationToken.
+//!
+//! 1. A node that has no parents and no handles can no longer be cancelled.
+//!     This is important during both cancellation and refcounting.
+//!
+//! 2. If node B *is* or *was* a child of node A, then node B was created *after* node A.
+//!     This is important for deadlock safety, as it is used for lock order.
+//!     Node B can only become the child of node A in two ways:
+//!         - being created with `child_node()`, in which case it is trivially true that
+//!           node A already existed when node B was created
+//!         - being moved A->C->B to A->B because node C was removed in `decrease_handle_refcount()`.
+//!           In this case the invariant still holds, as B was younger than C, and C was younger
+//!           than A, therefore B is also younger than A.
+//!
+//! 3. If two nodes are both unlocked and node A is the parent of node B, then node B is a child of node A.
+//!     It is important to always restore that invariant before dropping the lock of a node.
+//!
+//! ** Deadlock safety **
+//!
+//! Principles that provide deadlock safety:
+//!     1. We always lock in the order of creation time. We can prove this through invariant #2.
+//!        Specifically, through invariant #2, we know that we always have to lock a parent before its child.
+//!     2. We never lock two siblings simultaneously, because we cannot establish an order.
+//!        There is one exception in `with_locked_node_and_parent()`, which is described in the function itself.
+//!
+use crate::loom::sync::{Arc, Mutex, MutexGuard};
+
+/// A node of the cancella …(截断)
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+177             None => {
+178                 // Was the wrong parent, so unlock it before calling `func`
+179                 drop(locked_parent);
+180                 return func(locked_node, None);
+181             }
+182         };
+183 
+184         // Loop until we managed to lock both the node and its parent
+185         if Arc::ptr_eq(&actual_parent, &potential_parent) {
+186             return func(locked_node, Some(locked_parent));
+187         }
+188 
+189         // Drop locked_parent before reassigning to potential_parent,
+190         // as potential_parent is borrowed in it
+191         drop(locked_node);
+192         drop(locked_parent);
+193 

```

---

## tokio-4652 #1 (cid=867331241) tokio-util/src/sync/cancellation_token/tree_node.rs:348 [defect/performance]

- status: 
- audit_note: 

**评论**：

> You should wake the wakers after releasing the lock. Otherwise the future might try to lock the mutex while we're still holding it, which is a waste of resources.

**评论时 diff hunk**：

```diff
@@ -0,0 +1,341 @@
+//! GENERAL NOTES
+//! -------------
+//!
+//! ** Invariants **
+//!
+//! Those invariants shall be true at any time, and are required to prove correctness
+//! of the CancellationToken.
+//!
+//! 1. A node that has no parents and no handles can no longer be cancelled.
+//!     This is important during both cancellation and refcounting.
+//!
+//! 2. If node B *is* or *was* a child of node A, then node B was created *after* node A.
+//!     This is important for deadlock safety, as it is used for lock order.
+//!     Node B can only become the child of node A in two ways:
+//!         - being created with `child_node()`, in which case it is trivially true that
+//!           node A already existed when node B was created
+//!         - being moved A->C->B to A->B because node C was removed in `decrease_handle_refcount()`.
+//!           In this case the invariant still holds, as B was younger than C, and C was younger
+//!           than A, therefore B is also younger than A.
+//!
+//! 3. If two nodes are both unlocked and node A is the parent of node B, then node B is a child of node A.
+//!     It is important to always restore that invariant before dropping the lock of a node.
+//!
+//! ** Deadlock safety **
+//!
+//! Principles that provide deadlock safety:
+//!     1. We always lock in the order of creation time. We can prove this through invariant #2.
+//!        Specifically, through invariant #2, we know that we always have to lock a parent before its child.
+//!     2. We never lock two siblings simultaneously, because we cannot establish an order.
+//!        There is one exception in `with_locked_node_and_parent()`, which is described in the function itself.
+//!
+use crate::loom::sync::{Arc, Mutex, MutexGuard};
+
+/// A node of the cancella …(截断)
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+340 
+341             // For performance reasons, only adopt grandchildren that have children.
+342             // Otherwise, just cancel them right away, no need for another iteration.
+343             if locked_grandchild.children.is_empty() {
+344                 // Cancel the grandchild
+345                 locked_grandchild.is_cancelled = true;
+346                 locked_grandchild.children = Vec::new();
+347                 drop(locked_grandchild);
+348                 grandchild.waker.notify_waiters();
+349             } else {
+350                 // Otherwise, adopt grandchild
+351                 locked_grandchild.parent = Some(node.clone());
+352                 locked_grandchild.parent_idx = locked_node.children.len();
+353                 drop(locked_grandchild);
+354                 locked_node.children.push(grandchild);
+355             }
+356         }

```

---

## tokio-4652 #2 (cid=867331984) tokio-util/src/sync/cancellation_token/tree_node.rs:306 [suggestion/performance]

- status: 
- audit_note: 

**评论**：

> If the node is already cancelled, you can just return immediately.

**评论时 diff hunk**：

```diff
@@ -0,0 +1,341 @@
+//! GENERAL NOTES
+//! -------------
+//!
+//! ** Invariants **
+//!
+//! Those invariants shall be true at any time, and are required to prove correctness
+//! of the CancellationToken.
+//!
+//! 1. A node that has no parents and no handles can no longer be cancelled.
+//!     This is important during both cancellation and refcounting.
+//!
+//! 2. If node B *is* or *was* a child of node A, then node B was created *after* node A.
+//!     This is important for deadlock safety, as it is used for lock order.
+//!     Node B can only become the child of node A in two ways:
+//!         - being created with `child_node()`, in which case it is trivially true that
+//!           node A already existed when node B was created
+//!         - being moved A->C->B to A->B because node C was removed in `decrease_handle_refcount()`.
+//!           In this case the invariant still holds, as B was younger than C, and C was younger
+//!           than A, therefore B is also younger than A.
+//!
+//! 3. If two nodes are both unlocked and node A is the parent of node B, then node B is a child of node A.
+//!     It is important to always restore that invariant before dropping the lock of a node.
+//!
+//! ** Deadlock safety **
+//!
+//! Principles that provide deadlock safety:
+//!     1. We always lock in the order of creation time. We can prove this through invariant #2.
+//!        Specifically, through invariant #2, we know that we always have to lock a parent before its child.
+//!     2. We never lock two siblings simultaneously, because we cannot establish an order.
+//!        There is one exception in `with_locked_node_and_parent()`, which is described in the function itself.
+//!
+use crate::loom::sync::{Arc, Mutex, MutexGuard};
+
+/// A node of the cancella …(截断)
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+298         });
+299     }
+300 }
+301 
+302 /// Cancels a node and its children.
+303 pub(crate) fn cancel(node: &Arc<TreeNode>) {
+304     let mut locked_node = node.inner.lock().unwrap();
+305 
+306     if locked_node.is_cancelled {
+307         return;
+308     }
+309 
+310     // One by one, adopt grandchildren and then cancel and detach the child
+311     while let Some(child) = locked_node.children.pop() {
+312         // This can't deadlock because the mutex we are already
+313         // holding is the parent of child.
+314         let mut locked_child = child.inner.lock().unwrap();

```

---

## tokio-4652 #3 (cid=867349454) tokio-util/src/sync/cancellation_token/tree_node.rs:322 [suggestion/performance]

- status: 
- audit_note: 

**评论**：

> Here you can call `continue` if `locked_child.is_cancelled`. Its children are already cancelled in that case.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `continue`：final_added=false, comment_hunk=false
- `locked_child.is_cancelled`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -0,0 +1,355 @@
+//! GENERAL NOTES
+//! -------------
+//!
+//! ** Invariants **
+//!
+//! Those invariants shall be true at any time, and are required to prove correctness
+//! of the CancellationToken.
+//!
+//! 1. A node that has no parents and no handles can no longer be cancelled.
+//!     This is important during both cancellation and refcounting.
+//!
+//! 2. If node B *is* or *was* a child of node A, then node B was created *after* node A.
+//!     This is important for deadlock safety, as it is used for lock order.
+//!     Node B can only become the child of node A in two ways:
+//!         - being created with `child_node()`, in which case it is trivially true that
+//!           node A already existed when node B was created
+//!         - being moved A->C->B to A->B because node C was removed in `decrease_handle_refcount()`.
+//!           In this case the invariant still holds, as B was younger than C, and C was younger
+//!           than A, therefore B is also younger than A.
+//!
+//! 3. If two nodes are both unlocked and node A is the parent of node B, then node B is a child of node A.
+//!     It is important to always restore that invariant before dropping the lock of a node.
+//!
+//! ** Deadlock safety **
+//!
+//! Principles that provide deadlock safety:
+//!     1. We always lock in the order of creation time. We can prove this through invariant #2.
+//!        Specifically, through invariant #2, we know that we always have to lock a parent before its child.
+//!     2. We never lock two siblings simultaneously, because we cannot establish an order.
+//!        There is one exception in `with_locked_node_and_parent()`, which is described in the function itself.
+//!
+use crate::loom::sync::{Arc, Mutex, MutexGuard};
+
+/// A node of the cancella …(截断)
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+314         let mut locked_child = child.inner.lock().unwrap();
+315 
+316         // Detach the child from node
+317         // No need to modify node.children, as the child already got removed with `.pop`
+318         locked_child.parent = None;
+319         locked_child.parent_idx = 0;
+320 
+321         // If child is already cancelled, detaching is enough
+322         if locked_child.is_cancelled {
+323             continue;
+324         }
+325 
+326         // Cancel or adopt grandchildren
+327         while let Some(grandchild) = locked_child.children.pop() {
+328             // This can't deadlock because the two mutexes we are already
+329             // holding is the parent and grandparent of grandchild.
+330             let mut locked_grandchild = grandchild.inner.lock().unwrap();

```

---

## tokio-4652 #4 (cid=867332429) tokio-util/src/sync/cancellation_token/tree_node.rs:343 [suggestion/performance]

- status: 
- audit_note: 

**评论**：

> Here, we _could_ avoid adopting the grandchildren that have no children. It would be more efficient.

**评论时 diff hunk**：

```diff
@@ -0,0 +1,341 @@
+//! GENERAL NOTES
+//! -------------
+//!
+//! ** Invariants **
+//!
+//! Those invariants shall be true at any time, and are required to prove correctness
+//! of the CancellationToken.
+//!
+//! 1. A node that has no parents and no handles can no longer be cancelled.
+//!     This is important during both cancellation and refcounting.
+//!
+//! 2. If node B *is* or *was* a child of node A, then node B was created *after* node A.
+//!     This is important for deadlock safety, as it is used for lock order.
+//!     Node B can only become the child of node A in two ways:
+//!         - being created with `child_node()`, in which case it is trivially true that
+//!           node A already existed when node B was created
+//!         - being moved A->C->B to A->B because node C was removed in `decrease_handle_refcount()`.
+//!           In this case the invariant still holds, as B was younger than C, and C was younger
+//!           than A, therefore B is also younger than A.
+//!
+//! 3. If two nodes are both unlocked and node A is the parent of node B, then node B is a child of node A.
+//!     It is important to always restore that invariant before dropping the lock of a node.
+//!
+//! ** Deadlock safety **
+//!
+//! Principles that provide deadlock safety:
+//!     1. We always lock in the order of creation time. We can prove this through invariant #2.
+//!        Specifically, through invariant #2, we know that we always have to lock a parent before its child.
+//!     2. We never lock two siblings simultaneously, because we cannot establish an order.
+//!        There is one exception in `with_locked_node_and_parent()`, which is described in the function itself.
+//!
+use crate::loom::sync::{Arc, Mutex, MutexGuard};
+
+/// A node of the cancella …(截断)
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+335 
+336             // If grandchild is already cancelled, detaching is enough
+337             if locked_grandchild.is_cancelled {
+338                 continue;
+339             }
+340 
+341             // For performance reasons, only adopt grandchildren that have children.
+342             // Otherwise, just cancel them right away, no need for another iteration.
+343             if locked_grandchild.children.is_empty() {
+344                 // Cancel the grandchild
+345                 locked_grandchild.is_cancelled = true;
+346                 locked_grandchild.children = Vec::new();
+347                 drop(locked_grandchild);
+348                 grandchild.waker.notify_waiters();
+349             } else {
+350                 // Otherwise, adopt grandchild
+351                 locked_grandchild.parent = Some(node.clone());

```

---

## tokio-4652 #5 (cid=870763595) tokio-util/src/sync/cancellation_token/tree_node.rs:1 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> it would be nice if there was some top-level documentation explaining how this structure *works*, as well as the invariants that it upholds. the implementation has a bunch of inline comments, but there isn't a summary of the general design anywhere that i can find...

**评论时 diff hunk**：

```diff
@@ -0,0 +1,359 @@
+//! GENERAL NOTES
```

**最终快照锚点区**：

```diff
--- tokio-util/src/sync/cancellation_token/tree_node.rs @@
+1 //! This mod provides the logic for the inner tree structure of the CancellationToken.
+2 //!
+3 //! CancellationTokens are only light handles with references to TreeNode.
+4 //! All the logic is actually implemented in the TreeNode.
+5 //!
+6 //! A TreeNode is part of the cancellation tree and may have one parent and an arbitrary number of
+7 //! children.
+8 //!
+9 //! A TreeNode can receive the request to perform a cancellation through a CancellationToken.

```

---

## redis-9323 #0 (cid=684999246) src/server.c:5437 [design/design]

- status: 
- audit_note: 

**评论**：

> i think it may be a cleaner approach to turn on both `server.loading` and `server.async_loading` in our scenario. this way, all the existing code that checks the `loading` flag will not need a change, and we'll only need to add a few exceptions like `server.loading && !server.async_loading`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `server.loading`：final_added=false, comment_hunk=true
- `server.async_loading`：final_added=false, comment_hunk=true
- `loading`：final_added=false, comment_hunk=true
- `server.loading && !server.async_loading`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -713,7 +713,7 @@ void clusterAcceptHandler(aeEventLoop *el, int fd, void *privdata, int mask) {
 
     /* If the server is starting up, don't accept cluster connections:
      * UPDATE messages may interact with the database content. */
-    if (server.masterhost == NULL && server.loading) return;
+    if (server.masterhost == NULL && (server.loading || server.async_loading)) return;
```

**最终快照锚点区**：

```diff
--- src/server.c @@
 5434 
 5435     /* Loading DB? Return an error if the command has not the
 5436      * CMD_LOADING flag. */
-       if (server.loading && is_denyloading_command) {
+5437     if (server.loading && !server.async_loading && is_denyloading_command) {
 5438         rejectCommand(c, shared.loadingerr);
 5439         return C_OK;
 5440     }

```

---

## redis-9323 #1 (cid=685941509) src/rdb.c:2502 [design/design]

- status: 
- audit_note: 

**评论**：

> Here's a list of scenarios: 1. a replica gets disconnected and gets a full-sync when it reconnects. 2. a replica was connected to one master and got redirected to a new master (of the same replication chain) and got a full sync for some reason. 3. redis was restarted from rdb file (after a graceful shutdown), and has replicaof config, attempted a partial sync with the replid it got from the rdb file, but gets a full sync. 4. the replica was a master till now and got some data from that era, then gets a REPLICAOF command and does a full-sync. 5. the replica started empty and is attempting its first sync ever. 6. the replica was replicating from a certain master, and is now full-syncing from another master that's completely unrelated to the previous one. i think the first 3 scenarios want to allow clients to read the data, but on the last 3 we don't! i think the rule here is that if the current dataset in the db represents a valid point in time of the master we're full-syncing from, then we wanna allow reads, but if it doesn't represent a certain point in time of that master, then we don't want to serve reads.

**评论时 diff hunk**：

```diff
@@ -713,7 +713,7 @@ void clusterAcceptHandler(aeEventLoop *el, int fd, void *privdata, int mask) {
 
     /* If the server is starting up, don't accept cluster connections:
      * UPDATE messages may interact with the database content. */
-    if (server.masterhost == NULL && server.loading) return;
+    if (server.masterhost == NULL && (server.loading || server.async_loading)) return;
```

**最终快照锚点区**：

```diff
--- src/rdb.c @@
 2496 
 2497 /* Mark that we are loading in the global state and setup the fields
 2498  * needed to provide loading stats. */
-   void startLoading(size_t size, int rdbflags) {
+2499 void startLoading(size_t size, int rdbflags, int async) {
 2500     /* Load the DB */
 2501     server.loading = 1;
+2502     if (async == 1) server.async_loading = 1;
 2503     server.loading_start_time = time(NULL);
 2504     server.loading_loaded_bytes = 0;
 2505     server.loading_total_bytes = size;

```

---

## redis-9323 #2 (cid=691928309) src/db.c:497 [design/compatibility]

- status: 
- audit_note: 

**评论**：

> This change is gonna break modules that use the `RedisModuleEvent_ReplBackup` mechanism anyway, so i suggest the following: 1. We deprecate `RedisModuleEvent_ReplBackup` (starting redis 7.0 we never fire that event). 2. We create a new API named `RedisModuleEvent_ReplAsync`, holding 3 sub-events; STARTED, COMPLETED, ABORTED. 3. We add another module flag for `RedisModule_SetModuleOptions` (REDISMODULE_OPTIONS_HANDLE_REPL_ASYNC). 4. in replication.c, if there are modules loaded which registered a data type and didn't declare they're supporting this, we fall back to the alternative replication method.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `RedisModuleEvent_ReplBackup`：final_added=false, comment_hunk=false
- `RedisModuleEvent_ReplAsync`：final_added=false, comment_hunk=false
- `RedisModule_SetModuleOptions`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -462,90 +455,46 @@ long long emptyDb(int dbnum, int flags, void(callback)(dict*)) {
     return removed;
 }
 
-/* Store a backup of the database for later use, and put an empty one
- * instead of it. */
-dbBackup *backupDb(void) {
-    dbBackup *backup = zmalloc(sizeof(dbBackup));
+/* Initialize temporary db on replica for use during diskless replication */
+tempDb *initTempDb(void) {
+    tempDb *tempDb = zmalloc(sizeof(tempDb));
 
-    /* Backup main DBs. */
-    backup->dbarray = zmalloc(sizeof(redisDb)*server.dbnum);
+    tempDb->dbarray = zmalloc(sizeof(redisDb)*server.dbnum);
     for (int i=0; i<server.dbnum; i++) {
-        backup->dbarray[i] = server.db[i];
-        server.db[i].dict = dictCreate(&dbDictType);
-        server.db[i].expires = dictCreate(&dbExpiresDictType);
+        tempDb->dbarray[i].dict = dictCreate(&dbDictType);
+        tempDb->dbarray[i].expires = dictCreate(&dbExpiresDictType);
     }
 
-    /* Backup cluster slots to keys map if enable cluster. */
+    /* Init cluster slots to keys map if enable cluster. */
     if (server.cluster_enabled) {
-        backup->slots_to_keys = server.cluster->slots_to_keys;
-        memcpy(backup->slots_keys_count, server.cluster->slots_keys_count,
-            sizeof(server.cluster->slots_keys_count));
-        server.cluster->slots_to_keys = raxNew();
-        memset(server.cluster->slots_keys_count, 0,
-            sizeof(server.cluster->slots_keys_count));
+        tempDb->slots_to_keys = raxNew();
+        memset(tempDb->slots_keys_count, 0, sizeof(tempDb->slots_keys_count));
     }
 
-    moduleFireServerEvent(REDISMODULE_EVENT_REPL_BACKUP,
-                          REDISMODULE_SUBEVENT_REPL_BACKUP_CREATE,
-                          NULL);
-
-    return backup;
+    return tempDb;
 }
 
-/* Discard …(截断)
```

**最终快照锚点区**：

```diff
--- src/db.c @@
-   void restoreDbBackup(dbBackup *backup) {
-       /* Restore main DBs. */
-       for (int i=0; i<server.dbnum; i++) {
-           serverAssert(dictSize(server.db[i].dict) == 0);
-           serverAssert(dictSize(server.db[i].expires) == 0);
-           dictRelease(server.db[i].dict);
-           dictRelease(server.db[i].expires);
-           server.db[i] = backup->dbarray[i];
+497     if (server.cluster_enabled) {
+498         /* Release temp slot to key map. */
+499         slotToKeyDestroy(tempDb);
 500     }
 501 
-       /* Restore slots to keys map backup if enable cluster. */
-       if (server.cluster_enabled) slotToKeyRestoreBackup(&backup->slots_to_keys);
-   
-       /* Release backup. */

```

---

## redis-9323 #3 (cid=685446527) src/db.c:1328 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> this function is not as generic as it seems (by its name), the implementation and behavior are quite specific for placing the newly loaded temp db at the main active one, and the old active one in the temp. so i think we need a better name for it, and we certainly need a beefier doc comment.

**评论时 diff hunk**：

```diff
@@ -1363,6 +1312,43 @@ int dbSwapDatabases(int id1, int id2) {
     return C_OK;
 }
 
+void dbSwapAllDatabases(redisDb *dbarray1, redisDb *dbarray2) {
```

**最终快照锚点区**：

```diff
--- src/db.c @@
 1320 }
 1321 
+1322 /* Logically, this discards (flushes) the old main database, and apply the newly loaded
+1323  * database (temp) as the main (active) database, the actual freeing of old database
+1324  * (which will now be placed in the temp one) is done later. */
+1325 void swapMainDbWithTempDb(redisDb *tempDb) {
+1326     if (server.cluster_enabled) {
+1327         /* Swap slots_to_keys from tempdb just loaded with main db slots_to_keys. */
+1328         clusterSlotToKeyMapping *aux = server.db->slots_to_keys;
+1329         server.db->slots_to_keys = tempDb->slots_to_keys;
+1330         tempDb->slots_to_keys = aux;
+1331     }
+1332 
+1333     for (int i=0; i<server.dbnum; i++) {
+1334         redisDb aux = server.db[i];
+1335         redisDb *activedb = &server.db[i], *newdb = &tempDb[i];
+1336 

```

---

## redis-9323 #4 (cid=725662202) src/db.c:499 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> The slot-to-key array needs to be free'd somewhere before the tempDb is free'd. Flush just memsets it to zero. Maybe we can replace slotToKeyFlush with two functions: slotToKeyInit (allocates it) and slotToKeyDestroy (frees it)?

**评论时 diff hunk**：

```diff
@@ -463,41 +463,40 @@ long long emptyDb(int dbnum, int flags, void(callback)(dict*)) {
 }
 
 /* Initialize temporary db on replica for use during diskless replication. */
-tempDb *initTempDb(void) {
-    tempDb *tempDb = zmalloc(sizeof(tempDb));
-
-    tempDb->dbarray = zcalloc(sizeof(redisDb)*server.dbnum);
+redisDb *initTempDb(void) {
+    redisDb *tempDb = zcalloc(sizeof(redisDb)*server.dbnum);
     for (int i=0; i<server.dbnum; i++) {
-        tempDb->dbarray[i].dict = dictCreate(&dbDictType);
-        tempDb->dbarray[i].expires = dictCreate(&dbExpiresDictType);
+        tempDb[i].dict = dictCreate(&dbDictType);
+        tempDb[i].expires = dictCreate(&dbExpiresDictType);
+        tempDb[i].slots_to_keys = NULL;
     }
 
     if (server.cluster_enabled) {
-        /* Prepare temp slots_to_keys to be written during async diskless replication. */
-        slotToKeyTempDbFlush();
+        /* Prepare temp slots to keys map to be written during async diskless replication. */
+        tempDb->slots_to_keys = zmalloc(sizeof(*tempDb->slots_to_keys));
+        slotToKeyFlush(tempDb);
     }
 
     return tempDb;
 }
 
 /* Discard tempDb, this can be slow (similar to FLUSHALL), but it's always async */
- void discardTempDb(tempDb *tempDb, void(callback)(dict*)) {
+ void discardTempDb(redisDb *tempDb, void(callback)(dict*)) {
     int async = 1;
 
     /* Release temp DBs */
-    emptyDbStructure(tempDb->dbarray, -1, async, callback);
+    emptyDbStructure(tempDb, -1, async, callback);
     for (int i=0; i<server.dbnum; i++) {
-        dictRelease(tempDb->dbarray[i].dict);
-        dictRelease(tempDb->dbarray[i].expires);
+        dictRelease(tempDb[i].dict);
+        dictRelease(tempDb[i].expires);
     }
 
-    zfree(tempDb->dbarray);
-    zfree(tempDb);
-
     if (server.clu …(截断)
```

**最终快照锚点区**：

```diff
--- src/db.c @@
-       for (int i=0; i<server.dbnum; i++) {
-           serverAssert(dictSize(server.db[i].dict) == 0);
-           serverAssert(dictSize(server.db[i].expires) == 0);
-           dictRelease(server.db[i].dict);
-           dictRelease(server.db[i].expires);
-           server.db[i] = backup->dbarray[i];
+497     if (server.cluster_enabled) {
+498         /* Release temp slot to key map. */
+499         slotToKeyDestroy(tempDb);
 500     }
 501 
-       /* Restore slots to keys map backup if enable cluster. */
-       if (server.cluster_enabled) slotToKeyRestoreBackup(&backup->slots_to_keys);
-   
-       /* Release backup. */
-       zfree(backup->dbarray);
-       zfree(backup);

```

---

## redis-9323 #5 (cid=725663704) src/db.c:1322 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> This code doesn't swap them. It just copies one to the other. Don't we want to swap them like we do for dict, expires, avg_ttl and expires_cursor below? Also, we don't need memcpy here. We can just swap the pointers.

**评论时 diff hunk**：

```diff
@@ -1316,16 +1315,16 @@ int dbSwapDatabases(int id1, int id2) {
 /* Logically, this discards (flushes) the old main database, and apply the newly loaded
  * database (temp) as the main (active) database, the actual freeing of old database
  * (which will now be placed in the temp one) is done later. */
-void swapMainDbWithTempDb(tempDb *tempDb) {
+void swapMainDbWithTempDb(redisDb *tempDb) {
     if (server.cluster_enabled) {
         /* Copy slots_to_keys from tempdb just loaded to main slots_to_keys. */
-        memcpy(server.cluster->slots_to_keys, server.cluster->slots_to_keys_tempdb,
-           sizeof(server.cluster->slots_to_keys_tempdb));
+        memcpy(*server.db->slots_to_keys, *tempDb->slots_to_keys,
+           sizeof(*tempDb->slots_to_keys));
```

**最终快照锚点区**：

```diff
--- src/db.c @@
 1319     return C_OK;
 1320 }
 1321 
+1322 /* Logically, this discards (flushes) the old main database, and apply the newly loaded
+1323  * database (temp) as the main (active) database, the actual freeing of old database
+1324  * (which will now be placed in the temp one) is done later. */
+1325 void swapMainDbWithTempDb(redisDb *tempDb) {
+1326     if (server.cluster_enabled) {
+1327         /* Swap slots_to_keys from tempdb just loaded with main db slots_to_keys. */
+1328         clusterSlotToKeyMapping *aux = server.db->slots_to_keys;
+1329         server.db->slots_to_keys = tempDb->slots_to_keys;
+1330         tempDb->slots_to_keys = aux;

```

---

## tokio-6001 #0 (cid=1368527199) tokio/src/runtime/task/list.rs:119 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> Having correctness rely on such things makes it very easy to introduce bugs when changing things. (The check for `closed` must happen while the shard mutex is locked; otherwise a task spawned during shutdown can be pushed to `OwnedTasks` but not removed by `close_and_shutdown_all`.)

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `closed`：final_added=false, comment_hunk=true
- `OwnedTasks`：final_added=false, comment_hunk=true
- `close_and_shutdown_all`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -111,25 +110,30 @@ impl<S: 'static> OwnedTasks<S> {
             // to the field.
             task.header().set_owner_id(self.id);
         }
-
-        let mut lock = self.inner.lock();
-        if lock.closed {
-            drop(lock);
-            drop(notified);
+        // check close flag
+        if self.closed.load(Ordering::Acquire) {
             task.shutdown();
-            None
-        } else {
-            lock.list.push_front(task);
-            Some(notified)
+            return None;
         }
+        self.list.push(task);
+
+        // double check close flag for ensuring all tasks will shutdown after OwnedTasks has been closed,
+        // it should be completed quickly.
+        if self.closed.load(Ordering::Acquire) {
+            for i in 0..self.get_shard_size() {
+                if let Some(task) = self.list.pop_back(i) {
+                    task.shutdown();
+                }
+            }
+        }
```

**最终快照锚点区**：

```diff
--- tokio/src/runtime/task/list.rs @@
 115 
-           let mut lock = self.inner.lock();
-           if lock.closed {
-               drop(lock);
-               drop(notified);
+116         let shard = self.list.lock_shard(&task);
+117         // Check the closed flag in the lock for ensuring all that tasks
+118         // will shut down after the OwnedTasks has been closed.
+119         if self.closed.load(Ordering::Acquire) {
+120             drop(shard);
 121             task.shutdown();
-               None
-           } else {
-               lock.list.push_front(task);
-               Some(notified)
+122             return None;
 123         }

```

---

## tokio-6001 #1 (cid=1332175404) tokio/src/runtime/task/list.rs:44 [suggestion/readability]

- status: 
- audit_note: 

**评论**：

> I don't really understand what the word "grain" means in this context. Can we add a doc comment explaining this, rename the field to something that better describes what it is, or both?

**评论时 diff hunk**：

```diff
@@ -24,47 +26,30 @@ use std::num::NonZeroU64;
 // bug in Tokio, so we accept that certain bugs would not be caught if the two
 // mixed up runtimes happen to have the same id.
 
-cfg_has_atomic_u64! {
-    use std::sync::atomic::{AtomicU64, Ordering};
+static NEXT_OWNED_TASKS_ID: std::sync::atomic::AtomicU32 = std::sync::atomic::AtomicU32::new(1);
 
-    static NEXT_OWNED_TASKS_ID: AtomicU64 = AtomicU64::new(1);
-
-    fn get_next_id() -> NonZeroU64 {
-        loop {
-            let id = NEXT_OWNED_TASKS_ID.fetch_add(1, Ordering::Relaxed);
-            if let Some(id) = NonZeroU64::new(id) {
-                return id;
-            }
-        }
-    }
-}
-
-cfg_not_has_atomic_u64! {
-    use std::sync::atomic::{AtomicU32, Ordering};
-
-    static NEXT_OWNED_TASKS_ID: AtomicU32 = AtomicU32::new(1);
-
-    fn get_next_id() -> NonZeroU64 {
-        loop {
-            let id = NEXT_OWNED_TASKS_ID.fetch_add(1, Ordering::Relaxed);
-            if let Some(id) = NonZeroU64::new(u64::from(id)) {
-                return id;
-            }
+fn get_next_id() -> NonZeroU32 {
+    loop {
+        let id = NEXT_OWNED_TASKS_ID.fetch_add(1, Ordering::Relaxed);
+        if let Some(id) = NonZeroU32::new(id) {
+            return id;
         }
     }
 }
 
 pub(crate) struct OwnedTasks<S: 'static> {
-    inner: Mutex<CountedOwnedTasksInner<S>>,
-    pub(crate) id: NonZeroU64,
-}
-struct CountedOwnedTasksInner<S: 'static> {
-    list: CountedLinkedList<Task<S>, <Task<S> as Link>::Target>,
-    closed: bool,
+    lists: Box<[Mutex<ListInner<S>>]>,
+    pub(crate) id: NonZeroU32,
+    closed: AtomicBool,
+    pub(crate) grain: u32,
```

**最终快照锚点区**：

```diff
--- tokio/src/runtime/task/list.rs @@
 41 }
 42 
 43 cfg_not_has_atomic_u64! {
-       use std::sync::atomic::{AtomicU32, Ordering};
+44     use std::sync::atomic::AtomicU32;
 45 
 46     static NEXT_OWNED_TASKS_ID: AtomicU32 = AtomicU32::new(1);
 47 

```

---

## tokio-6001 #2 (cid=1332181300) tokio/src/runtime/task/list.rs:78 [design/design]

- status: 
- audit_note: 

**评论**：

> it looks like `OwnedTasks::new` is only ever called with the values 1 and 16. What do you think about making this constructor private, and having the `pub(crate)` constructors be `new_current_thread` and `new_multi_thread` or something? that way, the value (which feels like an internal implementation detail) isn't leaked to the places where the `OwnedTasks` is constructed, and we can change the value in one place, rather than having to update _every_ call to `OwnedTasks::new`?

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `OwnedTasks::new`：final_added=false, comment_hunk=false
- `pub(crate)`：final_added=false, comment_hunk=true
- `new_current_thread`：final_added=false, comment_hunk=false
- `new_multi_thread`：final_added=false, comment_hunk=false
- `OwnedTasks`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -73,13 +58,23 @@ struct OwnedTasksInner<S: 'static> {
 }
 
 impl<S: 'static> OwnedTasks<S> {
-    pub(crate) fn new() -> Self {
+    /// grain must be an integer power of 2
+    pub(crate) fn new(grain: u32) -> Self {
+        assert_eq!(
+            grain & (grain - 1),
+            0,
+            "the grain of OwnedTasks must be an integer power of 2"
+        );
```

**最终快照锚点区**：

```diff
--- tokio/src/runtime/task/list.rs @@
+71 
 72 struct OwnedTasksInner<S: 'static> {
 73     list: LinkedList<Task<S>, <Task<S> as Link>::Target>,
 74     closed: bool,
 75 }
 76 
 77 impl<S: 'static> OwnedTasks<S> {
-       pub(crate) fn new() -> Self {
+78     pub(crate) fn new(num_cores: usize) -> Self {
+79         let shard_size = Self::gen_shared_list_size(num_cores);
 80         Self {
-               inner: Mutex::new(CountedOwnedTasksInner {
-                   list: CountedLinkedList::new(),
-                   closed: false,
-               }),
+81             list: List::new(shard_size),
+82             closed: AtomicBool::new(false),

```

---

## tokio-6001 #3 (cid=1382579513) tokio/src/util/sharded_list.rs:32 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> ```suggestion
>     unsafe fn get_shard_id(target: NonNull<Self::Target>) -> usize;
> ```

**评论时 diff hunk**：

```diff
@@ -0,0 +1,160 @@
+#![cfg_attr(not(feature = "full"), allow(dead_code))]
+
+use std::{ptr::NonNull, sync::atomic::Ordering};
+
+use crate::loom::sync::{Mutex, MutexGuard};
+use std::sync::atomic::AtomicUsize;
+
+use super::linked_list::{Link, LinkedList};
+
+/// An intrusive  linked list supporting high concurrent updates.
+///
+/// It currently relies on `LinkedList`, so it is the caller's
+/// responsibility to ensure the list is empty before dropping it.
+///
+/// Note: Due to its inner sharded design, the order of node cannot be guaranteed.
+pub(crate) struct ShardedList<L, T> {
+    lists: Box<[Mutex<LinkedList<L, T>>]>,
+    count: AtomicUsize,
+    shard_mask: usize,
+}
+
+/// Defines the id of a node in a linked list, different ids cause inner list nodes
+/// to be scattered in different mutexs.
+///
+/// # Safety
+///
+/// Implementations must guarantee that `Target` types are pinned in memory. In
+/// other words, when a node is inserted, the value will not be moved as long as
+/// it is stored in the list.
+pub(crate) unsafe trait ShardedListItem: Link {
+    // The returned id is used to pick which list this item should go into.
+    unsafe fn get_shared_id(target: NonNull<Self::Target>) -> usize;
```

**最终快照锚点区**：

```diff
--- tokio/src/util/sharded_list.rs @@
+24 ///
+25 /// Implementations must guarantee that the id of an item does not change from
+26 /// call to call.
+27 pub(crate) unsafe trait ShardedListItem: Link {
+28     /// # Safety
+29     /// The provided pointer must point at a valid list item.
+30     unsafe fn get_shard_id(target: NonNull<Self::Target>) -> usize;
+31 }
+32 
+33 impl<L, T> ShardedList<L, T> {
+34     /// Creates a new and empty sharded linked list with the specified size.
+35     pub(crate) fn new(sharded_size: usize) -> Self {
+36         assert!(sharded_size.is_power_of_two());
+37 
+38         let shard_mask = sharded_size - 1;
+39         let mut lists = Vec::with_capacity(sharded_size);
+40         for _ in 0..sharded_size {

```

---

## redis-14017 #0 (cid=2106124779) src/config.c:3171 [design/design]

- status: 
- audit_note: 

**评论**：

> i personally think this is far too low level to be made a user config (and p.s. i don't see it in redis.conf). maybe we can completely drop it, and if not, let's make it a HIDDEN one.

**评论时 diff hunk**：

```diff
@@ -3168,6 +3168,7 @@ standardConfig static_configs[] = {
     createIntConfig("databases", NULL, IMMUTABLE_CONFIG, 1, INT_MAX, server.dbnum, 16, INTEGER_CONFIG, NULL, NULL),
     createIntConfig("port", NULL, MODIFIABLE_CONFIG, 0, 65535, server.port, 6379, INTEGER_CONFIG, NULL, updatePort), /* TCP port. */
     createIntConfig("io-threads", NULL, DEBUG_CONFIG | IMMUTABLE_CONFIG, 1, 128, server.io_threads_num, 1, INTEGER_CONFIG, NULL, NULL), /* Single threaded by default */
+    createIntConfig("prefetch-batch-max-size", NULL, MODIFIABLE_CONFIG, 0, 128, server.prefetch_batch_max_size, 16, INTEGER_CONFIG, NULL, NULL),
```

**最终快照锚点区**：

```diff
--- src/config.c @@
 3168     createIntConfig("databases", NULL, IMMUTABLE_CONFIG, 1, INT_MAX, server.dbnum, 16, INTEGER_CONFIG, NULL, NULL),
 3169     createIntConfig("port", NULL, MODIFIABLE_CONFIG, 0, 65535, server.port, 6379, INTEGER_CONFIG, NULL, updatePort), /* TCP port. */
 3170     createIntConfig("io-threads", NULL, DEBUG_CONFIG | IMMUTABLE_CONFIG, 1, 128, server.io_threads_num, 1, INTEGER_CONFIG, NULL, NULL), /* Single threaded by default */
+3171     createIntConfig("prefetch-batch-max-size", NULL, MODIFIABLE_CONFIG | HIDDEN_CONFIG, 0, 128, server.prefetch_batch_max_size, 16, INTEGER_CONFIG, NULL, NULL),
 3172     createIntConfig("auto-aof-rewrite-percentage", NULL, MODIFIABLE_CONFIG, 0, INT_MAX, server.aof_rewrite_perc, 100, INTEGER_CONFIG, NULL, NULL),
 3173     createIntConfig("cluster-replica-validity-factor", "cluster-slave-validity-factor", MODIFIABLE_CONFIG, 0, INT_MAX, server.cluster_slave_validity_factor, 10, INTEGER_CONFIG, NULL, NULL), /* Slave max data age factor. */
 3174     createIntConfig("list-max-listpack-size", "list-max-ziplist-size", MODIFIABLE_CONFIG, INT_MIN, INT_MAX, server.list_max_listpack_size, -2, INTEGER_CONFIG, NULL, NULL),

```

---

## redis-14017 #1 (cid=2106125576) src/db.c:374 [design/design]

- status: 
- audit_note: 

**评论**：

> if we do that, maybe we can cache the result so it can serve others usages (ACL, Cluster, ROF). besides, maybe instead of just computing the slot of the first key, it can already check for cross slot, and then the main thread won't have to. we did that in lookahead.

**评论时 diff hunk**：

```diff
@@ -324,6 +324,24 @@ int getKeySlot(sds key) {
     return calculateKeySlot(key);
 }
 
+/* Return the slot of the key in the command. IO threads use this function
+ * to calculate slot to reduce main-thread load */
+int getSlotFromCommand(struct redisCommand *cmd, robj **argv, int argc) {
+    int slot = -1;
+    if (!cmd || !server.cluster_enabled) return slot;
+
+    /* Get the keys from the command */
+    getKeysResult result = GETKEYS_RESULT_INIT;
+    int numkeys = getKeysFromCommand(cmd, argv, argc, &result);
```

**最终快照锚点区**：

```diff
--- src/db.c @@
+366 /* Return the slot of the key in the command. IO threads use this function
+367  * to calculate slot to reduce main-thread load */
+368 int getSlotFromCommand(struct redisCommand *cmd, robj **argv, int argc) {
+369     int slot = -1;
+370     if (!cmd || !server.cluster_enabled) return slot;
+371 
+372     /* Get the keys from the command */
+373     getKeysResult result = GETKEYS_RESULT_INIT;
+374     int numkeys = getKeysFromCommand(cmd, argv, argc, &result);
+375     if (numkeys > 0) {
+376         /* Get the slot of the first key */
+377         robj *first = argv[result.keys[0].pos];
+378         slot = keyHashSlot(first->ptr, (int)sdslen(first->ptr));
+379     }
+380     getKeysFreeResult(&result);
+381     return slot;
+382 }

```

---

## redis-14017 #2 (cid=2112771801) src/memory_prefetch.c:338 [suggestion/correctness]

- status: 
- audit_note: 

**评论**：

> Function comment mentions that goal is to bring data to L1. Then, here it says I/O thread already did the look up but on a multi core machine (most machines nowadays), iothread and main thread will be on different cores and not sharing L1. I wonder if this comment is accurate or am I missing something. Maybe this was done for a very specific CPU.

**评论时 diff hunk**：

```diff
@@ -0,0 +1,401 @@
+/*
+ * This file utilizes prefetching keys and data for multiple commands in a batch,
+ * to improve performance by amortizing memory access costs across multiple operations.
+ *
+ * Copyright (c) 2025-Present, Redis Ltd. and contributors.
+ * All rights reserved.
+ *
+ * Copyright (c) 2024-present, Valkey contributors.
+ * All rights reserved.
+ *
+ * Licensed under your choice of (a) the Redis Source Available License 2.0
+ * (RSALv2); or (b) the Server Side Public License v1 (SSPLv1); or (c) the
+ * GNU Affero General Public License v3 (AGPLv3).
+ *
+ * Portions of this file are available under BSD3 terms; see REDISCONTRIBUTIONS for more information.
+ */
+
+#include "memory_prefetch.h"
+#include "server.h"
+#include "dict.h"
+
+typedef enum { HT_IDX_FIRST = 0, HT_IDX_SECOND = 1, HT_IDX_INVALID = -1 } HashTableIndex;
+
+typedef enum {
+    PREFETCH_BUCKET,     /* Initial state, determines which hash table to use and prefetch the table's bucket */
+    PREFETCH_ENTRY,      /* prefetch entries associated with the given key's hash */
+    PREFETCH_KVOBJ,      /* prefetch the kv object of the entry found in the previous step */
+    PREFETCH_VALDATA,    /* prefetch the value data of the kv object found in the previous step */
+    PREFETCH_DONE        /* Indicates that prefetching for this key is complete */
+} PrefetchState;
+
+
+/************************************ State machine diagram for the prefetch operation. ********************************
+                                                           │
+                                                         start
+                                                           │
+                                                  ┌────────▼─────────┐
+                                       ┌─────── …(截断)
```

**最终快照锚点区**：

```diff
--- src/memory_prefetch.c @@
+330     if (!batch) return;
+331 
+332     /* Prefetch argv's for all clients */
+333     for (size_t i = 0; i < batch->client_count; i++) {
+334         client *c = batch->clients[i];
+335         if (!c || c->argc <= 1) continue;
+336         /* Skip prefetching first argv (cmd name) it was already looked up by
+337          * the I/O thread, and the main thread will not touch argv[0]. */
+338         for (int j = 1; j < c->argc; j++) {
+339             redis_prefetch_read(c->argv[j]);
+340         }
+341     }
+342 
+343     /* Prefetch the argv->ptr if required */
+344     for (size_t i = 0; i < batch->client_count; i++) {
+345         client *c = batch->clients[i];
+346         if (!c || c->argc <= 1) continue;

```

---

## redis-14017 #3 (cid=2112804186) src/memory_prefetch.c:394 [suggestion/performance]

- status: 
- audit_note: 

**评论**：

> If I'm not mistaken, we can do `batch->keys_dicts[batch->key_count] = kvstoreGetDict(c->db->keys, (c->slot > 0 ? c->slot : 0));` and delete `batch->slots`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `batch->slots`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -0,0 +1,401 @@
+/*
+ * This file utilizes prefetching keys and data for multiple commands in a batch,
+ * to improve performance by amortizing memory access costs across multiple operations.
+ *
+ * Copyright (c) 2025-Present, Redis Ltd. and contributors.
+ * All rights reserved.
+ *
+ * Copyright (c) 2024-present, Valkey contributors.
+ * All rights reserved.
+ *
+ * Licensed under your choice of (a) the Redis Source Available License 2.0
+ * (RSALv2); or (b) the Server Side Public License v1 (SSPLv1); or (c) the
+ * GNU Affero General Public License v3 (AGPLv3).
+ *
+ * Portions of this file are available under BSD3 terms; see REDISCONTRIBUTIONS for more information.
+ */
+
+#include "memory_prefetch.h"
+#include "server.h"
+#include "dict.h"
+
+typedef enum { HT_IDX_FIRST = 0, HT_IDX_SECOND = 1, HT_IDX_INVALID = -1 } HashTableIndex;
+
+typedef enum {
+    PREFETCH_BUCKET,     /* Initial state, determines which hash table to use and prefetch the table's bucket */
+    PREFETCH_ENTRY,      /* prefetch entries associated with the given key's hash */
+    PREFETCH_KVOBJ,      /* prefetch the kv object of the entry found in the previous step */
+    PREFETCH_VALDATA,    /* prefetch the value data of the kv object found in the previous step */
+    PREFETCH_DONE        /* Indicates that prefetching for this key is complete */
+} PrefetchState;
+
+
+/************************************ State machine diagram for the prefetch operation. ********************************
+                                                           │
+                                                         start
+                                                           │
+                                                  ┌────────▼─────────┐
+                                       ┌─────── …(截断)
```

**最终快照锚点区**：

```diff
--- src/memory_prefetch.c @@
+386         /* Get command's keys positions */
+387         getKeysResult result = GETKEYS_RESULT_INIT;
+388         int num_keys = getKeysFromCommand(c->iolookedcmd, c->argv, c->argc, &result);
+389         for (int i = 0; i < num_keys && batch->key_count < batch->max_prefetch_size; i++) {
+390             batch->keys[batch->key_count] = c->argv[result.keys[i].pos];
+391             batch->keys_dicts[batch->key_count] =
+392                 kvstoreGetDict(c->db->keys, c->slot > 0 ? c->slot : 0);
+393             batch->key_count++;
+394         }
+395         getKeysFreeResult(&result);
+396     }
+397 
+398     return C_OK;
+399 }

```

---

## redis-14017 #4 (cid=2097420269) src/memory_prefetch.c:231 [suggestion/performance]

- status: 
- audit_note: 

**评论**：

> Small kv objects of type string their values is embedded in the kv (i.e. `OBJ_ENCODING_EMBSTR`). In that case, maybe we can optimize and skip the step of `PREFETCH_VALDATA`.

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `OBJ_ENCODING_EMBSTR`：final_added=false, comment_hunk=false
- `PREFETCH_VALDATA`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -0,0 +1,401 @@
+/*
+ * This file utilizes prefetching keys and data for multiple commands in a batch,
+ * to improve performance by amortizing memory access costs across multiple operations.
+ *
+ * Copyright (c) 2025-Present, Redis Ltd. and contributors.
+ * All rights reserved.
+ *
+ * Copyright (c) 2024-present, Valkey contributors.
+ * All rights reserved.
+ *
+ * Licensed under your choice of (a) the Redis Source Available License 2.0
+ * (RSALv2); or (b) the Server Side Public License v1 (SSPLv1); or (c) the
+ * GNU Affero General Public License v3 (AGPLv3).
+ *
+ * Portions of this file are available under BSD3 terms; see REDISCONTRIBUTIONS for more information.
+ */
+
+#include "memory_prefetch.h"
+#include "server.h"
+#include "dict.h"
+
+typedef enum { HT_IDX_FIRST = 0, HT_IDX_SECOND = 1, HT_IDX_INVALID = -1 } HashTableIndex;
+
+typedef enum {
+    PREFETCH_BUCKET,     /* Initial state, determines which hash table to use and prefetch the table's bucket */
+    PREFETCH_ENTRY,      /* prefetch entries associated with the given key's hash */
+    PREFETCH_KVOBJ,      /* prefetch the kv object of the entry found in the previous step */
+    PREFETCH_VALDATA,    /* prefetch the value data of the kv object found in the previous step */
+    PREFETCH_DONE        /* Indicates that prefetching for this key is complete */
+} PrefetchState;
+
+
+/************************************ State machine diagram for the prefetch operation. ********************************
+                                                           │
+                                                         start
+                                                           │
+                                                  ┌────────▼─────────┐
+                                       ┌─────── …(截断)
```

**最终快照锚点区**：

```diff
--- src/memory_prefetch.c @@
+223     info->current_kv = kv;
+224     info->state = PREFETCH_VALDATA;
+225     /* If the entry is a pointer of kv object, we don't need to prefetch it */
+226     if (!is_kv) prefetchAndMoveToNextKey(kv);
+227 }
+228 
+229 /* Prefetch the value data of the kv object found in dict entry. */
+230 static void prefetchValueData(KeyPrefetchInfo *info) {
+231     size_t i = batch->cur_idx;
+232     kvobj *kv = info->current_kv;
+233 
+234     /* 1. If this is the last element, we assume a hit and don't compare the keys
+235      * 2. This kv object is the target of the lookup. */
+236     if ((!dictGetNext(info->current_entry) && !dictIsRehashing(batch->current_dicts[i])) ||
+237         dictCompareKeys(batch->current_dicts[i], batch->keys[i], kv))
+238     {
+239         if (batch->get_value_data_func) {

```

---

## pandas-27237 #0 (cid=407346544) pandas/core/indexes/datetimelike.py:191 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> We will actually need to check here if `idx` is still a DatetimeIndex? Because otherwise, I assume the below code won't work. For example, when doing `dtidx.sort_values(key=lambda x: x.month)`

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `idx`：final_added=false, comment_hunk=true
- `dtidx.sort_values(key=lambda x: x.month)`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -168,12 +169,14 @@ def __contains__(self, key: Any) -> bool:
             is_scalar(res) or isinstance(res, slice) or (is_list_like(res) and len(res))
         )
 
-    def sort_values(self, return_indexer=False, ascending=True):
+    def sort_values(self, return_indexer=False, ascending=True, key=None):
         """
         Return sorted copy of Index.
         """
+        idx = ensure_key_mapped(self, key)
```

**最终快照锚点区**：

```diff
--- pandas/core/indexes/datetimelike.py @@
 184             is_scalar(res) or isinstance(res, slice) or (is_list_like(res) and len(res))
 185         )
 186 
-       def sort_values(self, return_indexer=False, ascending=True):
+187     def sort_values(self, return_indexer=False, ascending=True, key=None):
 188         """
 189         Return sorted copy of Index.
 190         """
+191         idx = ensure_key_mapped(self, key)
+192 
+193         _as = idx.argsort()
+194         if not ascending:
+195             _as = _as[::-1]
+196         sorted_index = self.take(_as)
+197 
 198         if return_indexer:
-               _as = self.argsort()

```

---

## pandas-27237 #1 (cid=372248894) pandas/core/frame.py:5234 [design/design]

- status: 
- audit_note: 

**评论**：

> Are we OK with this? (I am not really sure myself) It seems a bit inconsistent with DataFrame which gets done column by column. But of course it's also not a fully valid comparison.

**评论时 diff hunk**：

```diff
@@ -5013,6 +5037,15 @@ def sort_index(
 
             .. versionadded:: 1.0.0
 
+        key : callable, optional
+            If not None, apply the key function to the index values
+            before sorting. This is similar to the `key` argument in the
+            builtin :meth:`sorted` function, with the notable difference that
+            this `key` function should be *vectorized*. It should expect an
+            ``Index`` and return an ``Index`` of the same shape.
```

**最终快照锚点区**：

```diff
--- pandas/core/frame.py @@
 5231 
 5232         axis = self._get_axis_number(axis)
 5233         labels = self._get_axis(axis)
+5234         labels = ensure_key_mapped(labels, key, levels=level)
 5235 
 5236         # make sure that the axis is lexsorted to start
 5237         # if not we need to reconstruct to get the correct indexer
 5238         labels = labels._sort_levels_monotonic()
 5239         if level is not None:
-   
 5240             new_axis, indexer = labels.sortlevel(
 5241                 level, ascending=ascending, sort_remaining=sort_remaining

```

---

## pandas-27237 #2 (cid=366145493) pandas/core/frame.py:5172 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> should create something in pandas._typing for this, maybe SortByKey

**评论时 diff hunk**：

```diff
@@ -4889,6 +4899,7 @@ def sort_values(
         kind="quicksort",
         na_position="last",
         ignore_index=False,
+        key: Optional[Callable[["DataFrame"], Union["DataFrame", AnyArrayLike]]] = None,
```

**最终快照锚点区**：

```diff
--- pandas/core/frame.py @@
 5169 
 5170             .. versionadded:: 1.0.0
 5171 
+5172         key : callable, optional
+5173             If not None, apply the key function to the index values
+5174             before sorting. This is similar to the `key` argument in the
+5175             builtin :meth:`sorted` function, with the notable difference that
+5176             this `key` function should be *vectorized*. It should expect an
+5177             ``Index`` and return an ``Index`` of the same shape. For MultiIndex
+5178             inputs, the key is applied *per level*.
+5179 
+5180             .. versionadded:: 1.1.0

```

---

## pandas-27237 #3 (cid=407257378) pandas/core/sorting.py:287 [design/design]

- status: 
- audit_note: 

**评论**：

> is this not wrapped at a higher level? if not, why not?

**评论时 diff hunk**：

```diff
@@ -241,21 +262,33 @@ def lexsort_indexer(keys, orders=None, na_position: str = "last"):
 
 
 def nargsort(
-    items, kind: str = "quicksort", ascending: bool = True, na_position: str = "last"
+    items,
+    kind: str = "quicksort",
+    ascending: bool = True,
+    na_position: str = "last",
+    key: Optional[Callable] = None,
 ):
     """
     Intended to be a drop-in replacement for np.argsort which handles NaNs.
 
-    Adds ascending and na_position parameters.
+    Adds ascending, na_position, and key parameters.
 
-    (GH #6399, #5231)
+    (GH #6399, #5231, #27237)
 
     Parameters
     ----------
     kind : str, default 'quicksort'
     ascending : bool, default True
     na_position : {'first', 'last'}, default 'last'
+    key : Optional[Callable], default None
     """
+
+    if key is not None:
+        items = ensure_key_mapped(items, key)
```

**最终快照锚点区**：

```diff
--- pandas/core/sorting.py @@
 279     ----------
 280     kind : str, default 'quicksort'
 281     ascending : bool, default True
 282     na_position : {'first', 'last'}, default 'last'
+283     key : Optional[Callable], default None
 284     """
+285 
+286     if key is not None:
+287         items = ensure_key_mapped(items, key)
+288         return nargsort(
+289             items, kind=kind, ascending=ascending, na_position=na_position, key=None
+290         )
+291 
 292     items = extract_array(items)
 293     mask = np.asarray(isna(items))
 294 

```

---

## pandas-27237 #4 (cid=359884679) pandas/core/sorting.py:329 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> `ABCIndexClass` works, but not `ABCIndex`. That fails on `RangeIndex`. Is that OK?

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `ABCIndexClass`：final_added=false, comment_hunk=false
- `ABCIndex`：final_added=false, comment_hunk=false
- `RangeIndex`：final_added=false, comment_hunk=false

**评论时 diff hunk**：

```diff
@@ -285,6 +316,24 @@ def nargsort(
     return indexer
 
 
+def ensure_key_mapped(values, key):
+    from pandas import Index
+
+    if not key:
+        return values
+
+    if isinstance(values, Index):
+        return values.map(key, na_action="ignore")
+    elif isinstance(values, np.ndarray):
+
+        def map_f(values, f):
```

**最终快照锚点区**：

```diff
--- pandas/core/sorting.py @@
 321 
+322 def ensure_key_mapped_multiindex(index, key: Callable, level=None):
+323     """
+324     Returns a new MultiIndex in which key has been applied
+325     to all levels specified in level (or all levels if level
+326     is None). Used for key sorting for MultiIndex.
+327 
+328     Parameters
+329     ----------
+330     index : MultiIndex
+331         Index to which to apply the key function on the
+332         specified levels.
+333     key : Callable
+334         Function that takes an Index and returns an Index of
+335         the same shape. This key is applied to each level
+336         separately. The name of the level can be used to
+337         distinguish different levels for application.

```

---

## pandas-22862 #0 (cid=226629141) pandas/core/arrays/period.py:166 [defect/correctness]

- status: 
- audit_note: 

**评论**：

> I think we *always* need to copy here, e.g. if you pass in an ndarray with copy=False this makes the internals mutable which is not great.

**评论时 diff hunk**：

```diff
@@ -85,94 +108,113 @@ def wrapper(self, other):
     return compat.set_function_name(wrapper, opname, cls)
 
 
-class PeriodArrayMixin(DatetimeLikeArrayMixin):
-    @property
-    def _box_func(self):
-        return lambda x: Period._from_ordinal(ordinal=x, freq=self.freq)
+class PeriodArray(dtl.DatetimeLikeArrayMixin, ExtensionArray):
+    """
+    Pandas ExtensionArray for storing Period data.
 
-    @cache_readonly
-    def dtype(self):
-        return PeriodDtype.construct_from_string(self.freq)
+    Users should use :func:`period_array` to create new instances.
 
-    @property
-    def _ndarray_values(self):
-        # Ordinals
-        return self._data
+    Notes
+    -----
+    There are two components to a PeriodArray
 
-    @property
-    def asi8(self):
-        return self._ndarray_values.view('i8')
+    - ordinals : integer ndarray
+    - freq : pd.tseries.offsets.Tick
 
-    @property
-    def freq(self):
-        """Return the frequency object if it is set, otherwise None"""
-        return self._freq
-
-    @freq.setter
-    def freq(self, value):
-        msg = ('Setting {cls}.freq has been deprecated and will be '
-               'removed in a future version; use {cls}.asfreq instead. '
-               'The {cls}.freq setter is not guaranteed to work.')
-        warnings.warn(msg.format(cls=type(self).__name__),
-                      FutureWarning, stacklevel=2)
-        self._freq = value
+    The values are physically stored as a 1-D ndarray of integers. These are
+    called "ordinals" and represent some kind of offset from a base.
 
-    # --------------------------------------------------------------------
-    # Constructors
+    The `freq` indicates the span covered by each element of the array.
+    All elements in the PeriodArray have the s …(截断)
```

**最终快照锚点区**：

```diff
--- pandas/core/arrays/period.py @@
+158 
+159     # Names others delegate to us
+160     _other_ops = []
+161     _bool_ops = ['is_leap_year']
+162     _object_ops = ['start_time', 'end_time', 'freq']
+163     _field_ops = ['year', 'month', 'day', 'hour', 'minute', 'second',
+164                   'weekofyear', 'weekday', 'week', 'dayofweek',
+165                   'dayofyear', 'quarter', 'qyear',
+166                   'days_in_month', 'daysinmonth']
+167     _datetimelike_ops = _field_ops + _object_ops + _bool_ops
+168     _datetimelike_methods = ['strftime', 'to_timestamp', 'asfreq']
 169 
 170     # --------------------------------------------------------------------
 171     # Constructors
+172     def __init__(self, values, freq=None, copy=False):
+173         if freq is not None:
+174             freq = Period._maybe_convert_freq(freq)

```

---

## pandas-22862 #1 (cid=226800296) pandas/core/arrays/period.py:75 [suggestion/correctness]

- status: 
- audit_note: 

**评论**：

> Yes, return NotImplemented. Also presumably ABCDataFrame. (Comparison operators should return NotImplemented instead of raising for unsupported operands.)

**评论时 diff hunk**：

```diff
@@ -52,13 +71,17 @@ def _period_array_cmp(cls, op):
 
     def wrapper(self, other):
         op = getattr(self._ndarray_values, opname)
+        if isinstance(other, (ABCSeries, ABCIndexClass)):
+            # TODO: return NotImplemented?
```

**最终快照锚点区**：

```diff
--- pandas/core/arrays/period.py @@
 70     nat_result = True if opname == '__ne__' else False
 71 
 72     def wrapper(self, other):
-           op = getattr(self._ndarray_values, opname)
+73         op = getattr(self.asi8, opname)
+74         # We want to eventually defer to the Series or PeriodIndex (which will
+75         # return here with an unboxed PeriodArray). But before we do that,
+76         # we do a bit of validation on type (Period) and freq, so that our
+77         # error messages are sensible
+78         not_implemented = isinstance(other, (ABCSeries, ABCIndexClass))
+79         if not_implemented:
+80             other = other._values
+81 
 82         if isinstance(other, Period):
 83             if other.freq != self.freq:

```

---

## pandas-22862 #2 (cid=224703904) pandas/core/arrays/period.py:147 [suggestion/maintainability]

- status: 
- audit_note: 

**评论**：

> shouldn't this be simpler and instead just call ``period_array``?

**反引号符号提示**（最终 diff 新增行 / 评论时 hunk 中是否出现）：

- `period_array`：final_added=false, comment_hunk=true

**评论时 diff hunk**：

```diff
@@ -83,88 +103,216 @@ def wrapper(self, other):
     return compat.set_function_name(wrapper, opname, cls)
 
 
-class PeriodArrayMixin(DatetimeLikeArrayMixin):
-    @property
-    def _box_func(self):
-        return lambda x: Period._from_ordinal(ordinal=x, freq=self.freq)
+class PeriodArray(DatetimeLikeArrayMixin, ExtensionArray):
+    """
+    Pandas ExtensionArray for storing Period data.
 
-    @cache_readonly
-    def dtype(self):
-        return PeriodDtype.construct_from_string(self.freq)
+    Users should use the :func:`period_array` function to create
+    new instances of PeriodArray.
 
-    @property
-    def _ndarray_values(self):
-        # Ordinals
-        return self._data
+    Notes
+    -----
+    There are two components to a PeriodArray
 
-    @property
-    def asi8(self):
-        return self._ndarray_values.view('i8')
+    - ordinals : integer ndarray
+    - freq : pd.tseries.offsets.Tick
 
-    @property
-    def freq(self):
-        """Return the frequency object if it is set, otherwise None"""
-        return self._freq
+    The values are physically stored as a 1-D ndarray of integers. These are
+    called "ordinals" and represent some kind of offset from a base.
+
+    The `freq` indicates the span covered by each element of the array.
+    All elements in the PeriodArray have the same `freq`.
 
-    @freq.setter
-    def freq(self, value):
-        msg = ('Setting {cls}.freq has been deprecated and will be '
-               'removed in a future version; use {cls}.asfreq instead. '
-               'The {cls}.freq setter is not guaranteed to work.')
-        warnings.warn(msg.format(cls=type(self).__name__),
-                      FutureWarning, stacklevel=2)
-        self._freq = value
+    See Also
+    --------
+    period_array : Create  …(截断)
```

**最终快照锚点区**：

```diff
--- pandas/core/arrays/period.py @@
+139     -----
+140     There are two components to a PeriodArray
+141 
+142     - ordinals : integer ndarray
+143     - freq : pd.tseries.offsets.Offset
+144 
+145     The values are physically stored as a 1-D ndarray of integers. These are
+146     called "ordinals" and represent some kind of offset from a base.
+147 
+148     The `freq` indicates the span covered by each element of the array.
+149     All elements in the PeriodArray have the same `freq`.
+150 
+151     See Also
+152     --------
+153     period_array : Create a new PeriodArray
+154     pandas.PeriodIndex : Immutable Index for period data
+155     """

```

---

