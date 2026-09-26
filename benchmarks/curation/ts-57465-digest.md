# digest ts-57465 : 25 anchored candidates (of 28 total)

## [25] src/compiler/checker.ts:10314 author=danvk reply=false subj=line side=LEFT cid=1526241965
Slight change here: previously this did not call `setEmitFlags(factory.createIdentifier(typePredicate.parameterName), EmitFlags.NoAsciiEscaping)` here but now it does via `nodeBuilder.typePredicateToTypePredicateNode`. I wasn't sure if this difference was intentional. Making these consistent doesn't break any tests, but I can model the difference if we want to keep it.

## [27] src/compiler/checker.ts:10314 author=weswigham reply=true subj=line side=LEFT cid=1526616514
AFAIK `NoAsciiEscaping` doesn't *do* anything for things that aren't string literals, so I'm honestly not sure why it's here.

## [9] src/compiler/checker.ts:15467 author=ahejlsberg reply=false subj=line side=RIGHT cid=1508184266
Change `===` check to `signature.resolvedReturnType.flags & TypeFlags.Boolean`. We generally want to avoid checking for specific _instances_ of types and instead check for specific _kinds_ of types.

## [19] src/compiler/checker.ts:37410 author=weswigham reply=false subj=line side=RIGHT cid=1523687507
We should check the signature's `getParameterCount` instead of the raw declaration here, if possible - as is, this PR infers a predicate `f is never` for
```ts
const a = (...f: []) => typeof f === "undefined";
```
which, while true, is kinda funky and... probably...? not useful.

More tests with rest args are probably pertinent, too - don't see many (any?) of those in the test added. For example, while the above makes a weird predicate, this makes no predicate:
```ts
const a = (...f: ["a" | "b"]) => f[0] === "a";
```
while the non-rest version does:
```ts
const a = (f: "a" | "b") => f === "a";
```

## [20] src/compiler/checker.ts:37410 author=danvk reply=true subj=line side=RIGHT cid=1523700900
Good point. We should probably just ignore rest parameters since they can't be used in type predicates:

```ts
const a = (...f: ["a" | "b"]): f is ['a'] => {
    // A type predicate cannot reference a rest parameter. (1229)
    return (f[0] === "a");
}
```

[playground](https://www.staging-typescript.org/play?ts=5.5.0-pr-57465-98#code/MYewdgzgLgBAhjAvDAFAOgwMwFwwNoBEcBMAPjAQEYEC6AlLpjAJYT4Dkc7NSAfDAG8AUDFEwA9OJgBBGFACeABwCmMRQCdlAE2bA4UVXrBgQsTZmWawwVQk3Q1cdXAC2yg+rSoAjACZfAJx0ImKaUACu6mComHgADDyISRTEdADcQgC+QkA)

Is a type predicate of `x is never` ever useful? It seems more likely that it indicates some other kind of mistake.

## [21] src/compiler/checker.ts:37410 author=danvk reply=true subj=line side=RIGHT cid=1524026734
@weswigham I added a check for `isRestParameter` and added your two examples to the test cases. I also added a test for a function that takes a rest parameter but narrows on another, non-rest parameter.

I switched the check to `getParameterCount`. Since this requires the signature, not the declaration, I've moved it out of `getTypePredicateFromBody` and into `getTypePredicateOfSignature`.

## [0] src/compiler/checker.ts:37426 author=danvk reply=false subj=line side=RIGHT cid=1497929564
I could use some advice from TS folks about what to do here. I see code to go from `string` to `__String` but not the other way around. I wonder whether `createTypePredicate` should take a `__String` rather than a `string`.

## [6] src/compiler/checker.ts:37426 author=ahejlsberg reply=true subj=line side=RIGHT cid=1507922928
Use `unescapeLeadingUnderscores(param.name)`.

## [11] src/compiler/checker.ts:37446 author=ahejlsberg reply=false subj=line side=RIGHT cid=1508185367
Again, see comment above regarding `===` with type instances.

## [7] src/compiler/checker.ts:37458 author=ahejlsberg reply=false subj=line side=RIGHT cid=1508177902
This isn't quite right. It really isn't meaningful to perform control flow analysis on an expression that wasn't assigned a flow node in the binder because without it you have no antecedent chain. The one notable exception may be the expression of an arrow function where you know there's no preceding code. I think the best approach here is to have the binder always initialize the `flowNode` property of `return` statements and then use that as the antecedent when analyzing expressions of (single) return statements. That would allow you to get rid of the single return statement exclusion logic you have below.

## [15] src/compiler/checker.ts:37469 author=JoostK reply=false subj=line side=RIGHT cid=1510329257
I don't know how engines optimize this today, but a couple years ago the spread operator was significantly slower than specifying all properties manually. Perhaps that would help here as well

```suggestion
            const falseCondition: FlowCondition = {
                flags: FlowFlags.FalseCondition,
                node: expr,
                antecedent: sharedAntecedent,
            };
```

## [16] src/compiler/checker.ts:37469 author=JoostK reply=true subj=line side=RIGHT cid=1510329608
Additionally, `createFlowCondition` creates the object literal as follows:

```ts
return initFlowNode({ flags, antecedent, node: expression });
```

importantly, `antecedent` precedes `node`. By deviating from this ordering here a second hidden class is introduced for condition flow nodes, which may cause monomorphic accesses to turn polymorphic.

## [17] src/compiler/checker.ts:37469 author=jakebailey reply=true subj=line side=RIGHT cid=1510330544
The flow nodes are one of the places we haven't optimized yet; there's loads of out of order props that would benefit from consistent factories (it's just not that way yet)

## [3] src/compiler/checker.ts:37470 author=danvk reply=false subj=line side=RIGHT cid=1497936006
This matches corresponding code in `getNarrowedTypeWorker`. It seems to behave slightly differently for `boolean` types, though. Do I need to expand `boolean` into `true | false`?

## [5] src/compiler/checker.ts:37470 author=danvk reply=true subj=line side=RIGHT cid=1501957422
This is no longer relevant, I found a simpler formulation of the condition that doesn't require this check.

## [12] src/compiler/checker.ts:37475 author=ahejlsberg reply=false subj=line side=RIGHT cid=1508186184
Just change to `!(falseSubtype.flags & TypeFlags.Never)`.

## [8] src/compiler/checker.ts:37480 author=ahejlsberg reply=false subj=line side=RIGHT cid=1508178595
You should be able to eliminate this given the changes I suggest above.

## [22] src/compiler/checker.ts:48585 author=weswigham reply=false subj=line side=RIGHT cid=1525179674
Even though we only infer `Identifier` non-assert type predicates right now, it'd be nice if this was robust enough to print the other predicate kinds without morphing them into identifier predicates.

## [24] src/compiler/checker.ts:48589 author=weswigham reply=false subj=line side=RIGHT cid=1525225501
🤦 I've just realized this is basically identical to code both in `typePredicateToString` and `signatureToSignatureDeclarationHelper` - rather than adding a 3rd instance, can we expose a `typePredicateToTypePredicateNode` on the `NodeBuilder` and use it in all 3?

## [26] src/compiler/checker.ts:48589 author=danvk reply=true subj=line side=RIGHT cid=1526243884
I've made this refactor in 50803a0d2c6c7620e2118a15e98d850a658b0d6b. ✂️ Nice to see the duplicated (triplicated!) code go away. There's one slight change that I've noted below. I don't think it's consequential but I wanted to check.

## [2] tests/baselines/reference/inferTypePredicates.types:664 author=danvk reply=false subj=line side=RIGHT cid=1497933806
Note the three `_`s on the type predicate. This is a bug caused by the `param.name.escapedText as string` type assertion. I'm not sure how to fix it.

## [13] tests/baselines/reference/inferTypePredicates.types:888 author=danvk reply=false subj=line side=RIGHT cid=1509104913
@ahejlsberg I just wanted to note that we infer a type predicate for this function with your change whereas we didn't before (that was the reason for the third call to `getFlowTypeOfReference`). This type guard isn't wrong, per se, just imprecise because if this function returns `false` then `x` is a `number`, not `number | Date`. I doubt this pattern is very common in the wild.

## [14] tests/baselines/reference/inferTypePredicates.types:888 author=ahejlsberg reply=true subj=line side=RIGHT cid=1509346539
I think the new behavior is preferable. We're not reflecting the fact that the function never returns for certain input types, but that shouldn't keep us from more accurately reflecting the behavior when the function actually returns. I mean, it's even more wrong to infer the function has no effect at all.

## [1] tests/baselines/reference/javascriptThisAssignmentInStaticBlock.errors.txt:1 author=danvk reply=false subj=file side=LEFT cid=1497931353
This file is deleted because we now (correctly) infer a type guard that's compatible with `Array.isArray`, which makes the error go away!

## [4] tests/cases/fourslash/thisPredicateFunctionQuickInfo.ts:68 author=danvk reply=false subj=line side=RIGHT cid=1497936841
This is correct, a type predicate now flows where it did not before.
