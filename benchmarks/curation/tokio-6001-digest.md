# digest tokio-6001 : 100 anchored candidates (of 110 total)

## [47] tokio/src/runtime/builder.rs:448 author=Darksonn reply=false subj=line side=RIGHT cid=1341479206
It's not clear to me that we want a configuration option for this. At least, it should be unstable.

## [49] tokio/src/runtime/builder.rs:448 author=wathenjiang reply=true subj=line side=RIGHT cid=1341574764
This is as I said above:
> The new added method `spawn_concurrency_level()` in Builder to set the number of segment lock in OwnedTasks struct is to allow us to customize this value. Its default value is 4 times the number of workers thread. An alternative approach is to set it as a constant, like 64, regardless of the number of CPU cores on different platforms. However, considering the usage of Tokio on micro-embedded devices, making the default value dependent on the number of worker threads can effectively minimize the time required for runtime creation on micro-embedded devices.

I thinke make it unstable is a good decision.


## [52] tokio/src/runtime/builder.rs:448 author=wathenjiang reply=true subj=line side=RIGHT cid=1342130529
I have made it unstable, and it seems that unstable features can not be tested in `benches` crate, so I remove `benches/spawn_current.rs` temporarily.

The benchmark test of parameter `spawn_concurrency_level` under different values is in github hidden items now: https://github.com/tokio-rs/tokio/pull/6001#issuecomment-1731611873

## [93] tokio/src/runtime/builder.rs:1340 author=Darksonn reply=false subj=line side=RIGHT cid=1403592556
If we are removing it as an option from the builder, then all changes related to `spawn_concurrency_level` in this file and other files should be removed. Instead, I would just move this into `runtime/task/list.rs` as a const.

## [17] tokio/src/runtime/scheduler/multi_thread/worker.rs:290 author=hawkw reply=false subj=line side=RIGHT cid=1332194928
how was 16 chosen as the value for this? i see that you ran the benchmarks on a Xeon Gold 6133, which has 16 CPU cores...would it be more correct to determine this value based on `num_cpus` to ensure that it's always the number of CPU cores, regardless of which CPU is in use?

## [18] tokio/src/runtime/scheduler/multi_thread/worker.rs:290 author=wathenjiang reply=true subj=line side=RIGHT cid=1332298055
I run on a `Intel(R) Xeon(R) Gold 6133 CPU @ 2.50GHz.` , which has 8 CPU cores. The reason I choose 16 is for performance testing, and this size can be discussed further.

Actually I decided to make this size twice the number of CPU cores, because the number of worker threads in tokio is the number of CPU core. Meanwhile, I want it to be a integer power of 2.

So we could just set it to a big enough vlaue for most platform, like 64, and let people to configure this. But it might cause CPU cache problem.

Or, set it to an integer power of two that is at least twice the number of CPU cores.


## [33] tokio/src/runtime/task/core.rs:161 author=Darksonn reply=false subj=line side=RIGHT cid=1339833247
Why not just recompute this every time you access the list? I think that would simplify the code.

Is the problem with being able to access the task id inside `list.rs`?

## [35] tokio/src/runtime/task/core.rs:161 author=wathenjiang reply=true subj=line side=RIGHT cid=1339994933
It is because if we use task_id to calculate the index every time, we have to store index in Header struct. But Core struct also relies on task_id to generate panic and some logic in TaskIdGuard, which means we either store a duplicate copy of task_id in Core(just like header), or pass the task_id as a parameter when calling Core-related methods.

The reason for this is not to avoid calculating the index every time, but so that the Header struct does not need to know the task_id. 

Header knows the index and OwnedTasks, Core konws the scheduler and task_id. What do you think of it?

## [37] tokio/src/runtime/task/core.rs:161 author=wathenjiang reply=true subj=line side=RIGHT cid=1340015787
Depend on the current design, when we got a task in `list.rs`, we can access its Header, but have no ideas about Core, the Core is used by the scheduler.

## [39] tokio/src/runtime/task/core.rs:161 author=Darksonn reply=true subj=line side=RIGHT cid=1340136358
Would this work for getting the id?

https://github.com/tokio-rs/tokio/blob/95368e5dcd1865cf60e76b1d83f716fcd40dfd58/tokio/src/runtime/task/core.rs#L457-L465

## [41] tokio/src/runtime/task/core.rs:161 author=wathenjiang reply=true subj=line side=RIGHT cid=1340166206
It definitely works! So I will use this method to caculate the index.

## [46] tokio/src/runtime/task/core.rs:161 author=wathenjiang reply=true subj=line side=RIGHT cid=1340818680
I have use `Header::get_id` method to calculate the index, and it works well.

## [0] tokio/src/runtime/task/core.rs:178 author=Darksonn reply=false subj=line side=RIGHT cid=1331204810
We already store the task id. It's in the core struct.

## [3] tokio/src/runtime/task/core.rs:178 author=wathenjiang reply=true subj=line side=RIGHT cid=1331368956
It is because the `id` field of task becomes hot, so I put it here. Maybe we should delete the ID in core struct. The reason I didn't delete it in core before is because I found in the core should know task's ID too in some cases, let me find if we have a way to do that.

## [4] tokio/src/runtime/task/core.rs:178 author=Darksonn reply=true subj=line side=RIGHT cid=1331479744
Does the `header_lte_cache_line` test still pass when you put it here?

## [6] tokio/src/runtime/task/core.rs:178 author=wathenjiang reply=true subj=line side=RIGHT cid=1331562630
> header_lte_cache_line

Yes. I reduce the size of `owner_id` to u32, I believe this size for owner_id is big enough. And I noticed that the `#[repr(C)]` is used, so  I ensured that the layout of the current struct fields is optimal for memory size.

## [22] tokio/src/runtime/task/core.rs:178 author=wathenjiang reply=true subj=line side=RIGHT cid=1334618950
We could consider removing the task_id from the Core struct and, when a task is selected into Harness for execution, pass the task_id in the header to the Core method by parameter.

I'm not sure if this is the best approach.


## [32] tokio/src/runtime/task/core.rs:178 author=wathenjiang reply=true subj=line side=RIGHT cid=1338835806
Why don't we expand using the existing `owner_id` field in `Header` struct instead of moving the task_id from `Core` to `Header`?

What we should do is that owner_id is still 64 bits(in current version I change it to u32), but now its lower 32 bits are the index of lists, and the upper 32 bits are the unique ID of ownedTasks. Of course, we can use another u32 field to store the index, and also use u32 to store owner_id instead of u64. I belive the latter is more reasonable, what do you think?

When we need to remove the task from OwnedTasks, we use the lower 32 bits of the `owner_id` to find the list store it, we don't do caculating for index every time, it already caculated.

It can decouple index implementation details(which depends on task_id) from `Header` struct. Header owns the index of list have the tasks, instead of having the data to calculate index.

@Darksonn Do you think my above thought is more reasonable than the current PR design?

## [34] tokio/src/runtime/task/id.rs:78 author=Darksonn reply=false subj=line side=RIGHT cid=1339840810
I think what you can do is use loom's lazy_static wrapper, which you can read about at https://docs.rs/loom/latest/loom/macro.lazy_static.html

This avoids a problem where tasks created in different threads get the same task id, since thread locals have a separate counter per thread.

By using the lazy_static wrapper, you should be able to put a `loom::sync::atomic::AtomicU64` in a global.

## [36] tokio/src/runtime/task/id.rs:78 author=wathenjiang reply=true subj=line side=RIGHT cid=1340009281
I have no tests on it yet, but as far as I know, the lazy_static has a little performance loss, do you think it is suitable for generating global ID? 

## [38] tokio/src/runtime/task/id.rs:78 author=Darksonn reply=true subj=line side=RIGHT cid=1340133284
You should only use lazy_static during loom tests, so this does not impact performance when not using loom.

## [42] tokio/src/runtime/task/id.rs:78 author=wathenjiang reply=true subj=line side=RIGHT cid=1340262424
But when I try this way,
```
  #[cfg(loom)]
        {
            crate::loom::lazy_static! {
                static ref NEXT_ID: StaticAtomicU64 = StaticAtomicU64::new(1);
            }            
            Self(NEXT_ID.fetch_add(1, Relaxed)) 
        }
```

 this following error occurs:

```
error[E0433]: failed to resolve: could not find `lazy_static` in `loom`
  --> tokio/src/runtime/task/id.rs:84:26
   |
84 |             crate::loom::lazy_static! {
   |                          ^^^^^^^^^^^ could not find `lazy_static` in `loom`
```

## [43] tokio/src/runtime/task/id.rs:78 author=wathenjiang reply=true subj=line side=RIGHT cid=1340268396
Other items in loom import correctly, except the `lazy_static!`, Do you have a way to fix this?

## [44] tokio/src/runtime/task/id.rs:78 author=wathenjiang reply=true subj=line side=RIGHT cid=1340283044
Oh may be we should use `[cfg(not(all(test, loom)))]` and `#[cfg(all(test, loom))]` instead.
`

## [45] tokio/src/runtime/task/id.rs:78 author=wathenjiang reply=true subj=line side=RIGHT cid=1340802778
I  have use `lazy_static` in loom instead, and it works well.

## [14] tokio/src/runtime/task/list.rs:44 author=hawkw reply=false subj=line side=RIGHT cid=1332175404
I don't really understand what the word "grain" means in this context. Can we add a doc comment explaining this, rename the field to something that better describes what it is, or both?

## [21] tokio/src/runtime/task/list.rs:44 author=wathenjiang reply=true subj=line side=RIGHT cid=1334492806
Yes, I've renamed it `segment_size` which descibes the segment lock design better.

## [15] tokio/src/runtime/task/list.rs:67 author=hawkw reply=false subj=line side=RIGHT cid=1332181300
it looks like `OwnedTasks::new` is only ever called with the values 1 and 16. What do you think about making this constructor private, and having the `pub(crate)` constructors be `new_current_thread` and `new_multi_thread` or something? that way, the value (which feels like an internal implementation detail) isn't leaked to the places where the `OwnedTasks` is constructed, and we can change the value in one place, rather than having to update _every_ call to `OwnedTasks::new`?

if we did that, we could probably also remove the assertion, or change it to a `debug_assert!`, since the caller can't pass in a value that isn't a power of two.
```suggestion
    pub(crate) fn new_current_thread() -> Self {
        Self::new(1)
    }
    
    pub(crate) fn new_multi_thread() -> Self {
        Self::new(16)
    }

    /// grain must be an integer power of 2
    fn new(grain: u32) -> Self {
        debug_assert_eq!(
            grain & (grain - 1),
            0,
            "the grain of OwnedTasks must be an integer power of 2"
        );
```

## [20] tokio/src/runtime/task/list.rs:67 author=wathenjiang reply=true subj=line side=RIGHT cid=1334490542
I would make it configurable in `Builder` of the Runtime.

## [94] tokio/src/runtime/task/list.rs:116 author=Darksonn reply=false subj=line side=RIGHT cid=1403595056
```suggestion
        // Check the closed flag in the lock for ensuring all that tasks
        // will shut down after the OwnedTasks has been closed.
```

## [16] tokio/src/runtime/task/list.rs:127 author=hawkw reply=false subj=line side=RIGHT cid=1332185125
this code for indexing the set of task lists by a task ID is somewhat complex, and it's repeated in a few places. what do you think about potentially adding a function that takes a task ID and returns a `MutexGuard` so that we don't have to repeat this?

## [19] tokio/src/runtime/task/list.rs:127 author=wathenjiang reply=true subj=line side=RIGHT cid=1334489767
Yes, this is duplicate code, I have move the way of get list by task id into the method `segment_inner`.

## [1] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=false subj=line side=RIGHT cid=1331209441
This doesn't look right to me. The `OwnedTasks` could be closed between the call to `self.closed.load(Acquire)` and the call to `self.lists[...].lock()`. If that happens, then we push something to the list, but it's never removed because the close operation has already completed.

## [2] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=true subj=line side=RIGHT cid=1331210822
You must check the `closed` flag _after_ calling `lock()` to fix this.

## [5] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1331558700
Concurrency safety control here is complex, but I believe it is safe now.

When we call `tokio::spawn` to create task anywhere, whether in `Runtime.block_on()` or `Runtime::spawn()` or inside the tasks which are being scheduled by worker threads, after this task is put into the ownedtasks, there will be **at least one** thread (if the kind of runtime is concurrent_thread, then the main thread; if multi_thread, then the worker thread) to be responsible for cleaning, ie. call the `close_and_shutdown_all`.

It is the ownership of Runtime to make it works.
 
If I am not mistaken about the above understanding, even if we delete the closed flag, this is concurrency safe.


## [7] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=true subj=line side=RIGHT cid=1331582370
No, I don't agree that this is correct. If I spawn something from outside the runtime during shutdown, this problem can result in the task being pushed to the `OwnedTasks`, but not removed by the `close_and_shutdown_all` call.

## [8] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1331614341
Is there only method `shutdown_background`, `shutdown_timeout` and `Drop::drop` will shutdown the Runtime? If it is true, because they consume the ownership of Runtime, other threads can not shutdown it meanwhile, when adding new tasks.

If the runtime is being shutdown, can we add tasks at the same time?

## [9] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1331736579
Does the `Runtime.enter()` will cause such concurrency problem?

## [10] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=true subj=line side=RIGHT cid=1331755141
You can spawn tasks during shutdown with `tokio::runtime::Handle`.

However, it doesn't really matter. Even if the API prevents you from triggering this, we should still fix it. Having correctness rely on such things makes it very easy to introduce bugs when changing things.

## [11] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1331782239
In this cases, If a thread entered the runtime, then it become a legal worker thread.

This thread shoud be responsible for cleaning tasks, when the`EnterGuard` is dropped, let it check if the runtime is closed, if it is true, then cleaning tasks.

Does it make sense?


## [12] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1331813998
> You can spawn tasks during shutdown with `tokio::runtime::Handle`.
> 
> However, it doesn't really matter. Even if the API prevents you from triggering this, we should still fix it. Having correctness rely on such things makes it very easy to introduce bugs when changing things.

I just saw this. What I said above may be a solution, but it may be more appropriate to directly use locks to solve the problem. I think your solution is more reasonable, let it easy.

## [13] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=true subj=line side=RIGHT cid=1331982429
I see you moved the check into the lock, so I'll resolve this.

## [56] tokio/src/runtime/task/list.rs:128 author=Darksonn reply=false subj=line side=RIGHT cid=1368527199
You can avoid this by defining a method like this:
```
impl<L: ShardedListItem> ShardedList<L, L::Target> {
    pub fn lock_shard(&self, val: &L::Handle) -> ShardGuard<'_, L, L::Target> {
        let id = unsafe { L::get_shared_id(L::as_raw(&val)) };
        ShardGuard {
            lock: self.shard_inner(id),
            count: &self.count,
            id,
        }
    }
}

struct ShardGuard<'a, L, T> {
    lock: MutexGuard<'a, LinkedList<L, T>>
    count: &'a AtomicUsize,
    id: usize,
}

impl<L: ShardedListItem> ShardGuard<L, L::Target> {
    /// Push a value to this shard.
    pub fn push_shard(self, val: L::Handle) {
        let id = unsafe { L::get_shared_id(L::as_raw(&val)) };
        assert_eq!(id, self.id);
        self.lock.push_front(val);
        self.count.fetch_add(1, Ordering::Relaxed);
    }
}
```
This will let you check `self.closed` while the mutex is locked.

## [57] tokio/src/runtime/task/list.rs:128 author=wathenjiang reply=true subj=line side=RIGHT cid=1368709741
Good idea, I have adopted this suggestion.

## [58] tokio/src/runtime/task/list.rs:145 author=Darksonn reply=false subj=line side=RIGHT cid=1382577720
This sentence is inaccurate if `start == 0`. 
```suggestion
    /// once the `shard_size` is reached, continuing until `start - 1`, it works like a ring.
```

## [96] tokio/src/runtime/task/list.rs:145 author=Darksonn reply=false subj=line side=RIGHT cid=1403597799
```suggestion
    /// The parameter start determines which shard this method will start at.
    /// Using different values for each worker thread reduces contention.
```


## [24] tokio/src/runtime/task/list.rs:170 author=hawkw reply=false subj=line side=RIGHT cid=1334664861
style nit: i think we can simplify this to
```suggestion
            while let Some(task) = self.segment_inner(i).pop_back() {
                self.count.fetch_sub(1, Ordering::Relaxed);
                task.shutdown();
            }
```
since the `lock` is dropped at the end of the expression `self.segment_inner(i).pop_back()`. of course, if you think it improves readability to make the duration of the mutex lock more explicit, we could also write
```suggestion
            loop {
                let mut lock = self.segment_inner(i);
                match lock.pop_back() {
                    Some(task) => {
                        drop(lock);
                        self.count.fetch_sub(1, Ordering::Relaxed);
                        task.shutdown();
                    }
                    None => break,
                };
            }
```
but IMO, the `while` loop is a bit nicer. it's up to you, though.

## [26] tokio/src/runtime/task/list.rs:170 author=wathenjiang reply=true subj=line side=RIGHT cid=1335215151
When using `while let Some(task) = self.segment_inner(i).pop_back() {`, we encounter the undesirable effect of holding the mutex for a longer time. The MutexGuard won't be released until the end of an iteration of the while loop.

The `task.shutdown` operation should be performed when we do not hold the lock.


## [27] tokio/src/runtime/task/list.rs:170 author=wathenjiang reply=true subj=line side=RIGHT cid=1335216264
I would like to take the second suggestion, but I made the mistake of taking the first suggestion so I chose to submit the code myself instead of adding suggestion to batch.

## [28] tokio/src/runtime/task/list.rs:170 author=hawkw reply=true subj=line side=RIGHT cid=1335234321
> When using while let Some(task) = self.segment_inner(i).pop_back() {, we encounter the undesirable effect of holding the mutex for a longer time. The MutexGuard won't be released until the end of an iteration of the while loop.

hmm, i thought that the MutexGuard would be dropped at the end of the `self.segment_inner(i).pop_back()` expression, so the mutex is locked for the same period of time as in the current code. but, if that's not actually the case, i agree that the current code is better, since the critical section is only as long as it takes to pop the task.

would introducing an explicit scope in the `while` loop also have that effect? like this:
```rust
while let Some(task) = { self.segment_inner(i).pop_back() } {
    // ...
}
```
all locals to the scope (like the guard) should be dropped as soon as the scope is evaluated, right?

## [29] tokio/src/runtime/task/list.rs:170 author=hawkw reply=true subj=line side=RIGHT cid=1335234415
in any case, i don't have a problem with the current code as-is, i'm just curious about whether the while loop would have similar behavior or not. 

## [30] tokio/src/runtime/task/list.rs:170 author=wathenjiang reply=true subj=line side=RIGHT cid=1335373371
The lifetime of `MutexGuard` is as follows:
```rust
while let Some(task) = self.segment_inner(i).pop_back() { // get MutexGuard here
      self.count.fetch_sub(1, Ordering::Relaxed);
      let _lock = self.segment_inner(i); // dead lock here
      task.shutdown();
}// drop MutexGuard here
```
The dead lock line reflects the lifetime of `MutexGuard`.

I belive the auto drop feature of Rust is very useful for avoid unexpected resources not being released, but in order to achieve the precise or customize lock control, the better way is that we drop lock manully like other languages which has no auto drop.

## [31] tokio/src/runtime/task/list.rs:170 author=wathenjiang reply=true subj=line side=RIGHT cid=1335379756
The following code does not change the lifetime of `MutexGuard` at all.
```
while let Some(task) = { self.segment_inner(i).pop_back() } {
    // ...
}
```
- we create `MutexGuard` in ` { self.segment_inner(i).pop_back() } ` code block
- and then move it from the block
- the `MutexGuard` still will not be dropped until th end of an iteration of this while loop.

This behavior of `MutexGuard in while let` also can be found in https://stackoverflow.com/questions/58968488/why-is-this-mutexguard-not-dropped




## [101] tokio/src/runtime/task/list.rs:188 author=Darksonn reply=false subj=line side=RIGHT cid=1404459239
```suggestion
    }

    /// Generates the size of the shared list based on the number of worker threads.
```

## [50] tokio/src/runtime/task/list.rs:196 author=wathenjiang reply=true subj=line side=RIGHT cid=1341584212
I use `count: AtomicUsize` to represent the number of tasks, and wo do operation by `Ordering::Relaxed` everywhere for the best performance, but it has no `happens-before` relationship.

The shutdown flag is write by `Ordering::Release` and read by `Ordering::Acquire`, it can ensure a `happens-before` relationship.

I use this flag here to make sure the following assert can always pass:

```
debug_assert!(self.shared.owned.is_shutdown()); // do has a `happens-before` relationship.
debug_assert!(self.shared.owned.is_empty()); // has no `happens-before` relationship.
```


## [51] tokio/src/runtime/task/list.rs:196 author=wathenjiang reply=true subj=line side=RIGHT cid=1342127858
Because in `multi_thread` mod, the lock is always obtained first, and then the closed flag of OwnedTasks is read. So I would like to delete the shutdown flag.

## [59] tokio/src/runtime/task/list.rs:196 author=Darksonn reply=false subj=line side=RIGHT cid=1382578002
```suggestion
            self.list.for_each(&mut f);
```

## [23] tokio/src/runtime/task/list.rs:200 author=hawkw reply=false subj=line side=RIGHT cid=1334659483
style nit, take it or leave it: i think we can simplify this code to just
```suggestion
        let task = self.segment_inner(task.task_id() as usize).remove(task.header_ptr())?;
        self.count.fetch_sub(1, Ordering::Relaxed);
        Some(task)
```

## [25] tokio/src/runtime/task/list.rs:200 author=wathenjiang reply=true subj=line side=RIGHT cid=1335211440
Yes! We use `?` here to resolve `None` better, but I would like to drop mutex manully here.

## [103] tokio/src/runtime/task/list.rs:200 author=Darksonn reply=false subj=line side=RIGHT cid=1404461565
This maximum seems really large to me. Thoughts?

## [104] tokio/src/runtime/task/list.rs:200 author=wathenjiang reply=true subj=line side=RIGHT cid=1405288685
I just used the settings of the previous `spawn_concurrency_level` function. The previous function could support user configuration, so this value was set to a large value.

If you want a smaller value, I'm ok with that.

As far as I know, some current server CPUs can reach close to 200 CPU cores (or hyperthreadings), such as the [AMD EPYC 9654](https://www.amd.com/en/products/cpu/amd-epyc-9654) which has 192 threads. Maybe we can set the value to 256(4 times this value is `1<<10`)?

## [105] tokio/src/runtime/task/list.rs:200 author=wathenjiang reply=true subj=line side=RIGHT cid=1412073332
I'd like to retract my previous point. I believe this should be a memory-bound scenario, so we only need to ensure a certain number of mutexes. In fact, the number of mutexes should not depend on the number of worker threads, but actually depends on the possible level of concurrency.

Below, I describe the concurrency level that may actually occur, rather than what occurs in the benchmark.

In real applications that use Tokio, multiple threads may perform `tokio::spawn` concurrently. However, having too many threads concurrently performing `tokio::spawn` at the same time can be considered a poor design choice in the higher-level application architecture. It is more likely that multiple threads will perform removing tasks concurrently at the same time. Nonetheless, removing a task from the list only takes up a minimal amount of runtime during the entire task life cycle, so the concurrency level of removing tasks is usually not a significant concern.

Therefore, I agree with your idea, and it is reasonable to set the upper limit to a small value. Setting this to 64 or 128 might make sense.

## [106] tokio/src/runtime/task/list.rs:200 author=Darksonn reply=true subj=line side=RIGHT cid=1412211570
Alright. What do you think about multiplying by 4?

## [107] tokio/src/runtime/task/list.rs:200 author=wathenjiang reply=true subj=line side=RIGHT cid=1412716768
The total number of random memory operations is consistent, providing multiple locks. Each thread obtains one of the locks through round robin, and can only perform random memory access after obtaining the lock. I got the following test results:
mutexes/threads | 1 | 2 | 4 | 8 | 12 | 16 | 24 | 32 | 48 | 64 | 128
-- | -- | -- | -- | -- | -- | -- | -- | -- | -- | -- | --
0 | 5.980172444 | 2.899437975 | 1.447906311 | 0.828731566 | 0.689618066 | 0.612355429 | 0.589401394 | 0.587380871 | 0.525477567 | 0.578456362 | 0.552132325
1 | 7.970250774 | 17.29034894 | 22.60164692 | 25.97284605 | 28.12352579 | 33.31359697 | 31.18786342 | 31.61139126 | 29.23225856 | 30.94094675 | 31.59191497
2 | 7.883931727 | 15.97845738 | 16.11107368 | 18.73377898 | 20.34614133 | 23.02624802 | 22.69439808 | 23.15802647 | 21.80570219 | 22.48815498 | 22.98585238
4 | 7.975676415 | 10.25364766 | 11.88074538 | 15.40198137 | 15.51024255 | 16.35328034 | 15.46874828 | 15.7982897 | 15.48703267 | 15.67227903 | 15.35829948
8 | 8.058803258 | 8.138193999 | 7.619081588 | 7.936418179 | 7.654288652 | 7.901945312 | 7.642439744 | 7.861542054 | 7.730389506 | 7.821229611 | 7.748344488
16 | 9.797308994 | 6.213334839 | 4.455407945 | 4.496371955 | 4.291254249 | 4.130849346 | 4.347601475 | 4.294096757 | 3.990391527 | 4.028562691 | 4.059085994
32 | 8.742854719 | 4.847656612 | 3.301780829 | 2.578327826 | 2.480488617 | 2.331294827 | 2.388718271 | 2.306257478 | 2.421350161 | 2.278177495 | 2.26569423
64 | 8.042672888 | 4.963568223 | 3.012473492 | 2.08243512 | 1.828237002 | 1.653421053 | 1.550811454 | 1.536452054 | 1.519761769 | 1.618966043 | 1.48010674
128 | 8.62801309 | 4.978525185 | 2.637936755 | 1.777546296 | 1.549096849 | 1.359814529 | 1.43875245 | 1.385468038 | 1.238832309 | 1.249940559 | 1.248131329
256 | 8.584906215 | 4.591742459 | 2.441556366 | 1.504790937 | 1.335449235 | 1.169191715 | 1.115906268 | 1.230570609 | 1.075581823 | 1.048285585 | 1.02977064
512 | 8.171549127 | 4.182283461 | 2.37535305 | 1.54202412 | 1.1690348 | 1.054650104 | 1.015366906 | 1.153238581 | 0.993319168 | 0.998864737 | 0.981392837
1024 | 8.533398132 | 4.175120792 | 2.209645233 | 1.412410651 | 1.055442085 | 0.938202817 | 1.122801927 | 0.940661156 | 0.888767412 | 0.914867532 | 0.92237305

</byte-sheet-html-origin><!--EndFragment-->

This is mainly because when the number of worker threads is relatively small, setting the number of locks to 4 times has a significant performance improvement compared to 2 times.

![image](https://github.com/tokio-rs/tokio/assets/48505670/dd3204c7-1ad6-4179-b2ab-077162b502ec)












## [108] tokio/src/runtime/task/list.rs:200 author=wathenjiang reply=true subj=line side=RIGHT cid=1412718134
For complete testing, please refer to https://gist.github.com/wathenjiang/30b689a7ef20b4ea667a2e8f358c321d

I think this performance test can provide a reference for how many mutexes are needed for OwnedTasks.

## [102] tokio/src/runtime/task/list.rs:205 author=Darksonn reply=false subj=line side=RIGHT cid=1404461036
```suggestion
        usize::min(MAX_SHARED_LIST_SIZE, num_cores.next_power_of_two()*4)
```

## [100] tokio/src/runtime/task/list.rs:206 author=Darksonn reply=false subj=line side=RIGHT cid=1404458868
It seems like this has several typos of "shared" instead of "sharded". Or is there some reason you use the word "shared"?

## [53] tokio/src/runtime/task/list.rs:223 author=Darksonn reply=false subj=line side=RIGHT cid=1354996045
You should only subtract here if `task.is_some()`.

## [54] tokio/src/runtime/task/list.rs:223 author=wathenjiang reply=true subj=line side=RIGHT cid=1355954891
I have used the `?` operator to quickly return. So if the task is `None`, we will not subtract here. But to reduce the scope of the lock, we should first release this lock, and then determine wether it is Some.

## [97] tokio/src/runtime/task/mod.rs:512 author=Darksonn reply=false subj=line side=RIGHT cid=1403598051
```suggestion
/// the shard id still won't change from call to call.)
```

## [80] tokio/src/runtime/task/mod.rs:515 author=wathenjiang reply=true subj=line side=RIGHT cid=1383336955
When I was writing this code, it occurred to me that `task_id` is actually different from `shard_id`. The size of `task_id` is 64 bits in all platform, but the `shard_id` depends on the specific platform: 
- 32 bits on 32-bit machine.
- 64 bits on 32-bit machine.

Casting 64 bits to 32 bits may be not safe, but we use this id for shard, so it is safe here.

I think it's okay to delete this comment.


## [84] tokio/src/runtime/task/mod.rs:515 author=Darksonn reply=false subj=line side=RIGHT cid=1384626537
```suggestion
/// # Safety
///
/// The id of a task is never changed after creation of the task, so the return value of
/// `get_shard_id` will not change. (The cast may throw away the upper 32 bits of the task id, but
/// the shard still won't change from call to call.)
unsafe impl<S> sharded_list::ShardedListItem for Task<S> {
    unsafe fn get_shard_id(target: NonNull<Self::Target>) -> usize {
        // SAFETY: The caller guarantees that `target` points at a valid task.
        let task_id = unsafe { Header::get_id(target) };
        task_id.0 as usize
```


## [85] tokio/src/runtime/task/mod.rs:515 author=Darksonn reply=true subj=line side=RIGHT cid=1384626884
See my other comment for the right way to add these safety comments.

## [81] tokio/src/util/linked_list.rs:299 author=wathenjiang reply=true subj=line side=RIGHT cid=1383351339
Yes, it should accept ownership of this `F` closure  instead!

## [99] tokio/src/util/linked_list.rs:299 author=Darksonn reply=true subj=line side=RIGHT cid=1403602070
Please undo this change and continue to use `mut f: F`. The code in `sharded_list.rs` will still work because mutable references are also `FnMut`.

## [90] tokio/src/util/sharded_list.rs:2 author=wathenjiang reply=true subj=line side=RIGHT cid=1384816879
Do you means `#![cfg_attr(not(feature = "full"), allow(dead_code))]`, if do that, we will got dede_code error in some feature.

## [91] tokio/src/util/sharded_list.rs:2 author=Darksonn reply=true subj=line side=RIGHT cid=1384822104
Please adjust the `cfg` in `src/util/mod.rs` instead to fix that dead code warning.

## [92] tokio/src/util/sharded_list.rs:2 author=wathenjiang reply=true subj=line side=RIGHT cid=1386226389
I use `cfg_rt!` in `src/util/mod.rs`, and move `#![cfg_attr(not(feature = "full"), allow(dead_code))]`.

## [62] tokio/src/util/sharded_list.rs:3 author=Darksonn reply=false subj=line side=RIGHT cid=1382578392
We only use `{}` for things in the same module.
```suggestion
use std::ptr::NonNull;
use std::sync::atomic::Ordering;
```

## [64] tokio/src/util/sharded_list.rs:10 author=Darksonn reply=false subj=line side=RIGHT cid=1382578583
```suggestion
/// An intrusive linked list supporting highly concurrent updates.
```

## [65] tokio/src/util/sharded_list.rs:15 author=Darksonn reply=false subj=line side=RIGHT cid=1382578597
```suggestion
/// Note: Due to its inner sharded design, the order of nodes cannot be guaranteed.
```

## [66] tokio/src/util/sharded_list.rs:23 author=Darksonn reply=false subj=line side=RIGHT cid=1382578737
```suggestion
/// Determines which linked list an item should be stored in.
```

## [67] tokio/src/util/sharded_list.rs:29 author=Darksonn reply=false subj=line side=RIGHT cid=1382578990
You don't need this, because anything that implements this trait also implements `Link`, so they promised to follow these rules when implementing `Link`. It's not necessary to require the same thing twice.

However, you do require one other thing:
```suggestion
/// Implementations must guarantee that the id of an item does not change from
/// call to call.
```


## [68] tokio/src/util/sharded_list.rs:32 author=Darksonn reply=false subj=line side=RIGHT cid=1382579429
```suggestion
    /// # Safety
    /// The provided pointer must point at a valid list item.
    unsafe fn get_shared_id(target: NonNull<Self::Target>) -> usize;
```

## [69] tokio/src/util/sharded_list.rs:32 author=Darksonn reply=false subj=line side=RIGHT cid=1382579513
```suggestion
    unsafe fn get_shard_id(target: NonNull<Self::Target>) -> usize;
```

## [70] tokio/src/util/sharded_list.rs:36 author=Darksonn reply=false subj=line side=RIGHT cid=1382579583
```suggestion
    /// Creates a new and empty sharded linked list with the specified size.
```

## [71] tokio/src/util/sharded_list.rs:42 author=Darksonn reply=false subj=line side=RIGHT cid=1382580724
I'm not a big fan of this. It means that the size is not always accurate, and the loop will run infinitely if `sharded_size > 2^63`.
```suggestion
        assert!(sharded_size.is_power_of_two());
```

## [86] tokio/src/util/sharded_list.rs:42 author=wathenjiang reply=true subj=line side=RIGHT cid=1384701005
I also think it's better to leave the size of `sharded_size` to  the caller.

But considering that the number of worker threads is not necessarily exactly an integer power of 2, this loop must still be executed.

However, spawn_concurrency_level is 4 times the number of worker threads. I considered setting its maximum value to `65536`, which I believe is a large enough number.

## [89] tokio/src/util/sharded_list.rs:42 author=wathenjiang reply=true subj=line side=RIGHT cid=1384791103
I moved this logic to 
```
impl Builder {
    fn get_spawn_concurrency_level(&self) -> usize {
        const MAX_SPAWN_CONCURRENCY_LEVEL: usize = 1 << 16;

        match self.spawn_concurrency_level {
            Some(i) => i,
            None => {
                use crate::loom::sys::num_cpus;
                let core_threads = self.worker_threads.unwrap_or_else(num_cpus);

                let mut size = 1;
                while size / 4 < core_threads && size < MAX_SPAWN_CONCURRENCY_LEVEL {
                    size <<= 1;
                }
                size.min(MAX_SPAWN_CONCURRENCY_LEVEL)
            }
        }
    }
}
```

## [72] tokio/src/util/sharded_list.rs:71 author=Darksonn reply=false subj=line side=RIGHT cid=1382580796
If this function isn't used, then just delete it.

## [98] tokio/src/util/sharded_list.rs:81 author=Darksonn reply=false subj=line side=RIGHT cid=1403600993
```suggestion
        // SAFETY: Since the shard id cannot change, it's not possible for this node
        // to be in any other list of the same sharded list.
        let node = unsafe { lock.remove(node) };
```

## [109] tokio/src/util/sharded_list.rs:112 author=Darksonn reply=false subj=line side=RIGHT cid=1418737749
```suggestion
    /// Used to help us to decide the parameter `shard_id` of the `pop_back` method.
```

## [73] tokio/src/util/sharded_list.rs:117 author=Darksonn reply=false subj=line side=RIGHT cid=1382581035
You are missing these in several places. I won't point out every single one, so please go through and fix them.
```suggestion
    /// Returns whether the linked list does not contain any node.
```

## [74] tokio/src/util/sharded_list.rs:123 author=Darksonn reply=false subj=line side=RIGHT cid=1382581156
Generally, you have an empty after the first line of documentation.
```suggestion
    /// Gets the shard size of this SharedList.
    ///
    /// Used to help us to decide the parameter `shard_id`` of the `pop_back` method.
```

## [75] tokio/src/util/sharded_list.rs:130 author=Darksonn reply=false subj=line side=RIGHT cid=1382581305
```suggestion
        // Safety: This modulo operation ensures that the index is not out of bounds.
```

## [76] tokio/src/util/sharded_list.rs:149 author=Darksonn reply=false subj=line side=RIGHT cid=1382581387
```suggestion
            F: FnMut(&L::Handle),
```

## [77] tokio/src/util/sharded_list.rs:151 author=Darksonn reply=false subj=line side=RIGHT cid=1382581459
```suggestion
            let mut guards = Vec::with_capacity(self.lists.len());
```

## [78] tokio/src/util/sharded_list.rs:152 author=Darksonn reply=false subj=line side=RIGHT cid=1382581486
```suggestion
            for list in self.lists.iter() {
```

## [79] tokio/src/util/sharded_list.rs:155 author=Darksonn reply=false subj=line side=RIGHT cid=1382581497
```suggestion
            for g in &mut guards {
```

## [55] tokio/src/util/shared_list.rs:1 author=Darksonn reply=false subj=file side=RIGHT cid=1368517445
The filename is incorrect. It should be `sharded_list.rs`.
