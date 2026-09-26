# digest tokio-4652 : 73 anchored candidates (of 104 total)

## [69] tokio-util/src/sync/cancellation_token.rs:60 author=Darksonn reply=false subj=line side=RIGHT cid=867355532
```suggestion
    /// A Future that is resolved once the corresponding [`CancellationToken`]
    /// is cancelled.
```

## [4] tokio-util/src/sync/cancellation_token.rs:63 author=Darksonn reply=false subj=line side=RIGHT cid=863507485
We do not need to heap allocate here.
```suggestion
    future: Option<tokio::sync::futures::Notified<'a>>,
```

## [8] tokio-util/src/sync/cancellation_token.rs:63 author=Finomnis reply=true subj=line side=RIGHT cid=863594418
I wasn't able to get it to compile without Pin<Box<>>. I'll try again later.

## [16] tokio-util/src/sync/cancellation_token.rs:63 author=Finomnis reply=true subj=line side=RIGHT cid=863625044
I believe to solve this we would have to implement [projections](https://doc.rust-lang.org/std/pin/#projections-and-structural-pinning).

## [17] tokio-util/src/sync/cancellation_token.rs:63 author=Finomnis reply=true subj=line side=RIGHT cid=864260700
I read more about projections and got to the conclusion that it's difficult. I realized we already use a projection crate, `pin-project-lite`. Sadly, this one doesn't work (to my understanding) with the `WaitForCancellationFuture`, because we wrap our nested future in an `Option`. Which brings us again to the same problem that we have a `Pin<&mut Option<Future>>`, but we need a `Pin<&mut Future>` to actually do the polling.
Will read about it some more, but I'm still wondering how problematic a `Pin<Box<>>` would really be.

## [18] tokio-util/src/sync/cancellation_token.rs:63 author=Finomnis reply=true subj=line side=RIGHT cid=864299012
Never mind. `.as_pin_mut()` does exactly that.

Updated.

## [73] tokio-util/src/sync/cancellation_token.rs:156 author=Darksonn reply=false subj=line side=RIGHT cid=867479797
```suggestion
    ///
    /// Be aware that cancellation is not an atomic operation. It is possible
    /// for another thread running in parallel with a call to `cancel` to first
    /// receive `true` from `is_cancelled` on one child node, and then receive
    /// `false` from `is_cancelled` on another child node. However, once the
    /// call to `cancel` returns, all child nodes have been fully cancelled.
    pub fn cancel(&self) {
```

## [74] tokio-util/src/sync/cancellation_token.rs:160 author=Darksonn reply=false subj=line side=RIGHT cid=867480046
```suggestion
    /// Returns `true` if the `CancellationToken` is cancelled.
```

## [75] tokio-util/src/sync/cancellation_token.rs:165 author=Darksonn reply=false subj=line side=RIGHT cid=867480717
```suggestion
    /// Returns a `Future` that gets fulfilled when cancellation is requested.
    ///
    /// The future will complete immediately if the token is already cancelled
    /// when this method is called.
    ///
    /// # Cancel safety
    ///
    /// This method is cancel safe.
```

## [5] tokio-util/src/sync/cancellation_token.rs:199 author=Darksonn reply=false subj=line side=RIGHT cid=863508216
When the `Notified` returns `Poll::Ready`, I think it would be best to call `get_future` again and only return `Poll::Ready` if you get back `None`.

## [7] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=863594102
I'm having a hard time with this one, I'm having troubles with the lifetimes. I'll try again later.

## [13] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=863610251
It seems that I need to implement [projections](https://doc.rust-lang.org/std/pin/#projections-and-structural-pinning), but I've never worked with that before. Is my assumption correct?

## [14] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=863615720
After reading into it a little, this seems very advanced and quite error prone. Are you sure we need that?
Is there any situation where the Waker could wake the future without being cancelled?

We could insert an `assert!(token.is_cancelled())` to make sure

## [15] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=863623063
I think the point of using `Pin<Box<>>` is related. To remove that, we also would need projections.

## [19] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=864299982
Still can't figure out the lifetimes.

Problem is that `Pin<&mut Future>` has the anonymous lifetime, but creating a new future with `.get_future()` creates a different lifetime that cannot be stored in the object.
I have no Idea if I'm simply doing something wrong, though.

If you have any idea, feel free to give me a hint. 

## [20] tokio-util/src/sync/cancellation_token.rs:199 author=Finomnis reply=true subj=line side=RIGHT cid=864322625
Ok finally found the relevant functions. And learned a lot about `Pin`.

Updated.

## [76] tokio-util/src/sync/cancellation_token.rs:215 author=Darksonn reply=false subj=line side=RIGHT cid=867534998
Since you found it non-obvious, we could add a comment like this one.
```suggestion
            // No wakeups can be lost here because there is always a call to
            // `is_cancelled` between the creation of the future and the call to
            // `poll`, and the code that sets the cancelled flag does so before
            // waking the `Notified`.
            if this.future.as_mut().poll(cx).is_pending() {
                return Poll::Pending;
            }
```

## [1] tokio-util/src/sync/cancellation_token.rs:337 author=Darksonn reply=false subj=line side=RIGHT cid=863504124
```suggestion
    fn with_locked_node_and_parent<F, Ret>(
        node: &Arc<TreeNode>,
        func: F,
    ) -> Ret
    where
        F: FnOnce(&mut Inner, Option<&mut Inner>) -> Ret,
    {
```

## [6] tokio-util/src/sync/cancellation_token.rs:337 author=Finomnis reply=true subj=line side=RIGHT cid=863575804
The `MutexGuard` is in there on purpose, so that `func` can choose to unlock either if required. (See `remove_child`)

## [2] tokio-util/src/sync/cancellation_token.rs:343 author=Darksonn reply=false subj=line side=RIGHT cid=863504833
If this check fails, you can reuse the parent `actual_parent` from the previous iteration.

## [21] tokio-util/src/sync/cancellation_token.rs:356 author=Darksonn reply=false subj=line side=RIGHT cid=864537762
I don't think this scope is useful. Just put
```Rust
potential_parent = actual_parent;
```
at the end.

## [29] tokio-util/src/sync/cancellation_token.rs:374 author=Darksonn reply=false subj=line side=RIGHT cid=866228275
I'm not a fan of the way this is phrased. You are saying "this doesn't work because X, but actually it works anyway because Y". I would prefer something like "this works because Y. It might seem like X is an issue, but reasons"

## [22] tokio-util/src/sync/cancellation_token.rs:464 author=Darksonn reply=false subj=line side=RIGHT cid=864740751
If this happens in parallel with a call to cancel on the parent node, after `node` is removed from its parent, but before `cancel` is called on `node`, then the cancel is lost for the children.

Possible solution: Don't destroy the tree when cancelling.

## [24] tokio-util/src/sync/cancellation_token.rs:464 author=Finomnis reply=true subj=line side=RIGHT cid=865033994
Just for understanding: 
- You call "Cancel" on the parent.
- The parent disconnects the node
- On other thread: the last handle gets dropped. Because the node no longer has a parent, "disconnect_children" gets called
- The "cancel" function finds a node with no children and decides that's it

If with "don't destroy the tree when cancelling" you mean, "First cancel everything, then destroy the tree", I agree.
We just gotta be careful about what happens when we get multiple cancels on different nodes simultaneously.

Related question: Do we actually have to destroy the tree when cancelling? Do we gain anything from it? Originally I thought it might reduce the number of nodes, but now that we implemented that we move children to parents when no handles exist any more, I don't think disconnecting the tree gives us anything any more.

We further have to be careful though, that when we iterate through the tree we don't run into the same problem.

The actual problem is that there is a gap in cancel() between locking the children for disconnect and locking the children for propagating the cancellation. I'll attempt to fix that.

## [25] tokio-util/src/sync/cancellation_token.rs:464 author=Darksonn reply=true subj=line side=RIGHT cid=865045913
No, I actually mean don't destroy the tree at all when cancelling. And yes, the issue is that cancellation is not atomic.

## [26] tokio-util/src/sync/cancellation_token.rs:464 author=Finomnis reply=true subj=line side=RIGHT cid=865067167
I realized while iterating over the tree for cancellation+dropping, we can only iterate over it depth first, because we have to keep the lock of the parent alive during the entire time of iterating through the children, and we cannot lock two children simultaneously. That means we have to descend one child at a time.

## [27] tokio-util/src/sync/cancellation_token.rs:464 author=Finomnis reply=true subj=line side=RIGHT cid=865067661
Also, I absolutely did not find a way to flatten the recursion, because recursion makes it extremely easy here to manage the locks.

## [34] tokio-util/src/sync/cancellation_token/implementation.rs:30 author=Darksonn reply=false subj=line side=RIGHT cid=867329366
I would simplify this to just say that we always lock in order of creation, which we can determine through invariant #2. You don't need to talk about siblings here.

## [35] tokio-util/src/sync/cancellation_token/implementation.rs:40 author=Darksonn reply=false subj=line side=RIGHT cid=867329381
Does it make more sense to name this file `tree_node.rs` instead?

## [48] tokio-util/src/sync/cancellation_token/implementation.rs:40 author=Finomnis reply=true subj=line side=RIGHT cid=867334399
Is this a question about an opinion or the request to change the name?

## [50] tokio-util/src/sync/cancellation_token/implementation.rs:40 author=Darksonn reply=true subj=line side=RIGHT cid=867335405
I have a weak preference for `tree_node.rs`.

## [36] tokio-util/src/sync/cancellation_token/implementation.rs:164 author=Darksonn reply=false subj=line side=RIGHT cid=867329811
The potential parent might not match and be `Some` here. We shouldn't keep the previous parent locked in that case.
```suggestion
            // If we locked the node and its parent is `None`, we are in a valid state
            // and can return.
            None => {
                drop(locked_parent);
                return func(locked_node, None);
            }
```

## [44] tokio-util/src/sync/cancellation_token/implementation.rs:164 author=Finomnis reply=true subj=line side=RIGHT cid=867333241
"Return" implies "drop" of all the variables in the context. That's how `DropGuard` works, essentially.

## [49] tokio-util/src/sync/cancellation_token/implementation.rs:164 author=Darksonn reply=true subj=line side=RIGHT cid=867335362
Yes, but without the explicit drop, the mutex guard is dropped _after_ calling `func`.

## [37] tokio-util/src/sync/cancellation_token/implementation.rs:293 author=Darksonn reply=false subj=line side=RIGHT cid=867331241
You should wake the wakers after releasing the lock. Otherwise the future might try to lock the mutex while we're still holding it, which is a waste of resources.

## [40] tokio-util/src/sync/cancellation_token/implementation.rs:297 author=Darksonn reply=false subj=line side=RIGHT cid=867331984
If the node is already cancelled, you can just return immediately.

## [57] tokio-util/src/sync/cancellation_token/implementation.rs:305 author=Darksonn reply=false subj=line side=RIGHT cid=867349454
Here you can call `continue` if `locked_child.is_cancelled`. Its children are already cancelled in that case.

## [62] tokio-util/src/sync/cancellation_token/implementation.rs:305 author=Finomnis reply=true subj=line side=RIGHT cid=867349669
You too fast with reviews :D:D just implemented that

## [43] tokio-util/src/sync/cancellation_token/implementation.rs:308 author=Darksonn reply=false subj=line side=RIGHT cid=867332481
If the node is already cancelled, you can just `continue` after these two statements.

## [42] tokio-util/src/sync/cancellation_token/implementation.rs:311 author=Darksonn reply=false subj=line side=RIGHT cid=867332429
Here, we _could_ avoid adopting the grandchildren that have no children. It would be more efficient.

## [47] tokio-util/src/sync/cancellation_token/implementation.rs:311 author=Finomnis reply=true subj=line side=RIGHT cid=867334149
Would be, is it worth the extra complexity?

## [52] tokio-util/src/sync/cancellation_token/implementation.rs:311 author=Darksonn reply=true subj=line side=RIGHT cid=867339671
Let's do everything else first, then we can think about it.

## [55] tokio-util/src/sync/cancellation_token/implementation.rs:313 author=Darksonn reply=false subj=line side=RIGHT cid=867349081
```suggestion
            if locked_grandchild.children.is_empty() || locked_grandchild.is_cancelled {
```

## [65] tokio-util/src/sync/cancellation_token/implementation.rs:313 author=Finomnis reply=true subj=line side=RIGHT cid=867349740
I implemented it a little different to avoid a second waker call

## [39] tokio-util/src/sync/cancellation_token/implementation.rs:325 author=Finomnis reply=false subj=line side=RIGHT cid=867331770
Typo, should be 'invalidate'. Rephrase the entire sentence, as it is confusing to read.

## [53] tokio-util/src/sync/cancellation_token/implementation.rs:325 author=Finomnis reply=true subj=line side=RIGHT cid=867348844
Section got removed completely during refactoring

## [56] tokio-util/src/sync/cancellation_token/implementation.rs:327 author=Darksonn reply=false subj=line side=RIGHT cid=867349308
This is the same, but I find it easier to read this way.
```suggestion
                locked_grandchild.parent = node.clone();
                locked_grandchild.parent_idx = locked_node.children.len();
                drop(locked_grandchild);
                locked_node.children.push(grandchild);
```

## [66] tokio-util/src/sync/cancellation_token/implementation.rs:327 author=Finomnis reply=true subj=line side=RIGHT cid=867349803
I would have preferred your version as well. But it's borrowed in the `locked_node`

## [38] tokio-util/src/sync/cancellation_token/implementation.rs:328 author=Darksonn reply=false subj=line side=RIGHT cid=867331648
You don't need a semicolon here.
```suggestion
        if let Some(mut parent) = parent {
            remove_child(&mut parent, node);
        }
```

## [41] tokio-util/src/sync/cancellation_token/implementation.rs:328 author=Darksonn reply=false subj=line side=RIGHT cid=867332247
There's nothing that really requires that we do this, and not doing it avoids locking the parent for the duration of the cancel call. 

## [45] tokio-util/src/sync/cancellation_token/implementation.rs:328 author=Finomnis reply=true subj=line side=RIGHT cid=867333612
It invalidates invariant #1. As soon as we detach `node` and then unlock it, `decrease_handle_refcount` might assume that `node` cannot be cancelled any more and detach all of its children.

The solution would be to not drop the lock of `node` in `remove_child`, but we cannot do that as we need to lock a sibling in the process of removing it.

## [46] tokio-util/src/sync/cancellation_token/implementation.rs:328 author=Finomnis reply=true subj=line side=RIGHT cid=867333812
I might be able to split this into two steps, where we first swap `node` to the end of `parent.children`, then lock it and remove it. Then it would be safe to unlock `parent`. But there cannot be a lock gap (where `node` is unlocked) between removing `node` from `parent` and iterating through `node.children`.

## [88] tokio-util/src/sync/cancellation_token/tree_node.rs:1 author=hawkw reply=false subj=line side=RIGHT cid=870763595
it would be nice if there was some top-level documentation explaining how this structure *works*, as well as the invariants that it upholds. the implementation has a bunch of inline comments, but there isn't a summary of the general design anywhere that i can find...

## [85] tokio-util/src/sync/cancellation_token/tree_node.rs:4 author=hawkw reply=false subj=line side=RIGHT cid=870759961
nitpick, not a big deal: normally, we use a section header like
```suggestion
//! # Safety
```
when documenting this kind of invariants

## [86] tokio-util/src/sync/cancellation_token/tree_node.rs:6 author=hawkw reply=false subj=line side=RIGHT cid=870760720
also a nitpick: i'm not sure about the use of the phrase "prove correctness" here --- this phrasing implies that there is a formal proof of the implementation's correctness, which I don't believe is the case?

## [103] tokio-util/src/sync/cancellation_token/tree_node.rs:10 author=Darksonn reply=false subj=line side=RIGHT cid=872677900
Grandchildren are also cancelled.
```suggestion
//! This cancellation request will cancel the node and all of its descendants.
```

## [87] tokio-util/src/sync/cancellation_token/tree_node.rs:52 author=hawkw reply=false subj=line side=RIGHT cid=870762534
nit, take it or leave it: how about just calling this
```suggestion
    pub(crate) fn notified(&self) -> tokio::sync::futures::Notified<'_> {
```

## [82] tokio-util/src/sync/cancellation_token/tree_node.rs:114 author=Darksonn reply=false subj=line side=RIGHT cid=867766444
```suggestion
    for child in std::mem::take(&mut node.children) {
```

## [89] tokio-util/src/sync/cancellation_token/tree_node.rs:168 author=hawkw reply=false subj=line side=RIGHT cid=870765321
this is a bit subtle: normally one would expect the `drop` here to be unnecessary, because returning from a functiion should drop all its locals...but evaluating `func` will run additional code before we return, so it's necessary to drop the parent first so that it doesn't remain locked while `func` is evaluated. I think it would be good if the comment here explained this...

## [90] tokio-util/src/sync/cancellation_token/tree_node.rs:178 author=hawkw reply=false subj=line side=RIGHT cid=870766037
i believe these explicit drops aren't _necessary_, as the locals should be dropped in this order on each iteration of the loop regardless. but, having them here might help to make the code clearer?

## [92] tokio-util/src/sync/cancellation_token/tree_node.rs:178 author=Finomnis reply=true subj=line side=RIGHT cid=870820911
Dropping `locked_parent` is necessary because `potential_parent` is borrowed in it, I think. The other one is to preserve drop order.

## [93] tokio-util/src/sync/cancellation_token/tree_node.rs:178 author=hawkw reply=true subj=line side=RIGHT cid=870821389
ah, i see --- i missed that. maybe worth adding a comment?

## [101] tokio-util/src/sync/cancellation_token/tree_node.rs:186 author=hawkw reply=false subj=line side=RIGHT cid=872616051
nit, maybe:
```suggestion
        // Drop locked_parent before reassigning to potential_parent, 
        // as potential_parent is borrowed in it
```

## [81] tokio-util/src/sync/cancellation_token/tree_node.rs:194 author=Darksonn reply=false subj=line side=RIGHT cid=867766280
```suggestion
    for child in std::mem::take(&mut node.children) {
```

## [80] tokio-util/src/sync/cancellation_token/tree_node.rs:229 author=Darksonn reply=false subj=line side=RIGHT cid=867765678
```suggestion
    }

    let len = parent.children.len();
    if 4*len <= parent.children.capacity() {
        parent.children.shrink_to(2*len);
    }
}
```


## [91] tokio-util/src/sync/cancellation_token/tree_node.rs:251 author=hawkw reply=false subj=line side=RIGHT cid=870767530
typo:
```suggestion
/// Decreases the reference count of handles.
```

## [70] tokio-util/src/sync/cancellation_token/tree_node.rs:285 author=Darksonn reply=false subj=line side=RIGHT cid=867478683
```suggestion
        // This can't deadlock because the mutex we are already
        // holding is the parent of child.
```

## [71] tokio-util/src/sync/cancellation_token/tree_node.rs:300 author=Darksonn reply=false subj=line side=RIGHT cid=867478923
```suggestion
            // This can't deadlock because the two mutexes we are already
            // holding is the parent and grandparent of grandchild.
            let mut locked_grandchild = grandchild.inner.lock().unwrap();
```

## [79] tokio-util/src/sync/cancellation_token/tree_node.rs:323 author=Darksonn reply=false subj=line side=RIGHT cid=867763599
```suggestion
                locked_grandchild.is_cancelled = true;
                locked_grandchild.children = Vec::new();
                drop(locked_grandchild);
```

## [84] tokio-util/src/sync/cancellation_token/tree_node.rs:323 author=Darksonn reply=true subj=line side=RIGHT cid=867827753
Hmm, it might not be possible to have an empty vector with non-zero capacity if you add the others, but I don't think it hurts to include it either.

## [72] tokio-util/src/sync/cancellation_token/tree_node.rs:336 author=Darksonn reply=false subj=line side=RIGHT cid=867479297
```suggestion
    // Cancel the node itself.
```

## [77] tokio-util/src/sync/cancellation_token/tree_node.rs:336 author=Darksonn reply=false subj=line side=RIGHT cid=867762967
```suggestion
        // Cancel the child
        locked_child.is_cancelled = true;
        locked_child.children = Vec::new();
        drop(locked_child);
```

## [78] tokio-util/src/sync/cancellation_token/tree_node.rs:345 author=Darksonn reply=false subj=line side=RIGHT cid=867763180
```suggestion
    // Cancel the node itself.
    locked_node.is_cancelled = true;
    locked_node.children = Vec::new();
    drop(locked_node);
```
