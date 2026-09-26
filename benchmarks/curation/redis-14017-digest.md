# digest redis-14017 : 42 anchored candidates (of 50 total)

## [10] src/config.c:3171 author=oranagra reply=false subj=line side=RIGHT cid=2106124779
i personally think this is far too low level to be made a user config (and p.s. i don't see it in redis.conf).
maybe we can completely drop it, and if not, let's make it a HIDDEN one

## [12] src/config.c:3171 author=ShooterIT reply=true subj=line side=RIGHT cid=2106393711
I truly added it in `redis.conf` https://github.com/redis/redis/pull/14017/files#diff-c683e35ac8c539e4f6c402fc159fe0e57086e3e661dd9ac7e48cedaee2c79c14R1337

I agree it is a low level thing, i am not sure if memory prefetching can work well in all platform (with different cache size), but i tested on EC2 type of AMD, Intel, ARM, it shows enabling memory prefetching is better. so if you want to hide this config, i can do that

## [19] src/config.c:3171 author=ShooterIT reply=true subj=line side=RIGHT cid=2109387354
hided this config in https://github.com/redis/redis/pull/14017/commits/1fe343cc48931bf2e09d685be550b94412cde91e

## [45] src/config.h:118 author=sundb reply=false subj=line side=RIGHT cid=2113316756
is it equivalent to redis_prefetch_read?  what about also adding `/* Read with high locality */` for this line.

## [48] src/config.h:118 author=ShooterIT reply=true subj=line side=RIGHT cid=2113339817
i remove this one, just use `redis_prefetch_read`

## [11] src/db.c:335 author=oranagra reply=false subj=line side=RIGHT cid=2106125576
if we do that, maybe we can cache the result so it can serve others usages (ACL, Cluster, ROF).
besides, maybe instead of just computing the slot of the first key, it can already check for cross slot, and then the main thread won't have to.
we did that in lookahead.

## [13] src/db.c:335 author=ShooterIT reply=true subj=line side=RIGHT cid=2106402973
yes. we can let IO threads do this and cache the result. This PR is based on https://github.com/valkey-io/valkey/pull/861, valkey hasn’t optimized for this issue, so I kept the original behavior, and i don't want to make it bigger, maybe a new PR is better, as you said, we can cache the key result, even calculate if all keys are in a single slot in the IO thread.

besides, I have tried to cache key result but there is no significant performance improvement, so i think it is not urgent.

## [14] src/db.c:335 author=oranagra reply=true subj=line side=RIGHT cid=2108218069
no significant performance improvement.. did you check with cluster mode or ACL?
seems odd that we invest if offloading argument parsing, and command lookup, but claim that offloading key name extraction and slot number / cross slot isn't significant.

in any case, i don't mind taking it in a separate PR, arguing this one is about memory prefetching only. the reason i commented was because this change does add a call to getKeysFromCommand, so essentially adding some work that's now done twice.

anyway, if / when we'll combine the lookahead project this this, we'll get it cached anyway.

## [15] src/db.c:335 author=ShooterIT reply=true subj=line side=RIGHT cid=2108300010
I tested without cluster mode or ACL, so i only offload get keys result, here is a flame graph i have save in the previous test with 8 IO threads [8write](https://github.com/user-attachments/assets/d48e5bf4-4bd5-4246-a19a-80488b99e977). It shows getting keys result costs 0.1% CPU of the main thread.
<img width="494" alt="flame graph" src="https://github.com/user-attachments/assets/453d9299-1034-47ec-a031-416374d6fc5d" />
If in cluster mode, calculating slot number may cost more CPU.

## [16] src/db.c:335 author=ShooterIT reply=true subj=line side=RIGHT cid=2108649810
> anyway, if / when we'll combine the lookahead project this this, we'll get it cached anyway.

since the lookahead project did this, we can have this feature (cache keys result) after merging it?

## [17] src/db.c:335 author=oranagra reply=true subj=line side=RIGHT cid=2108738958
not sure i understand, 0.1% of the main thread CPU in which scenario? if there's no ACL or cluster, it's not used by the main thread.

in any case, we agree it should be cached, and we agree it can be done later. the only question is if we have some small regression because we can now call it twice (e.g. in cluster mode). i guess we don't care as long as the impact of the PR is still positive and we have a plan to improve later.

## [18] src/db.c:335 author=ShooterIT reply=true subj=line side=RIGHT cid=2108988222
the scenario is under standalone mode with 8 IO threads, 100% SET command stress test.

yes, do agree

## [20] src/db.c:335 author=tezc reply=true subj=line side=RIGHT cid=2112710820
I see that main thread calls getKeysFromCommand() inside addCommandToBatch(). Maybe it can be avoided as well once we cache the results. 

## [40] src/db.c:335 author=ShooterIT reply=true subj=line side=RIGHT cid=2113193350
Yes. Keys result can be used
- memory prefetch
- slot calculating for cluster/cluster-compatibility
- ACL check

And as oran said, the IO thread also should check if all keys are in the same slot, instead of only the first key, so the main thread can just report cross-slot error (MULTI-EXEC requires special treatment), so i want to put these works in a separate PR (Valkey also intends to do this, but hasn’t implemented it yet). lookahead project already did this, maybe we just reuse this logic when merging. Besides, as i showed above,  the regression  of calling  `getKeysFromCommand` is not notable.

## [0] src/dict.h:184 author=ShooterIT reply=false subj=line side=LEFT cid=2092240627
Hi @moticless I think this function should be removed after https://github.com/redis/redis/pull/13806, i added a new `dictCompareKeys` in `dict.c`

## [4] src/dict.h:184 author=moticless reply=true subj=line side=LEFT cid=2096964601
Right. Please lmk if you want me to remove it.

## [5] src/dict.h:184 author=ShooterIT reply=true subj=line side=LEFT cid=2097028805
Thanks for confirmation, i removed it in this PR, `dictCompareKeys` is deprecated, no place to call except this PR, so it is no harm.

## [21] src/iothread.c:354 author=tezc reply=false subj=line side=RIGHT cid=2112720904
does it make sense to do something like:

```
int to_prefetch = getConfigPrefetchBatchSize(len);
```

we may pass `len` into that function and do the calculation there? Maybe it will be easier to follow, please consider if that makes sense. 

## [38] src/iothread.c:354 author=ShooterIT reply=true subj=line side=RIGHT cid=2113117170
make sense. if so, maybe we should adopt a new function name, how about `determinePrefetchCount`



## [41] src/iothread.c:354 author=tezc reply=true subj=line side=RIGHT cid=2113283700
sounds good, maybe `calculatePrefetchCount()`

## [49] src/iothread.c:354 author=ShooterIT reply=true subj=line side=RIGHT cid=2113343491
i prefer `determine` :) since it is not pure computation logic, but rather a strategy-based decision-making process.

## [22] src/iothread.c:363 author=tezc reply=false subj=line side=RIGHT cid=2112730713
we don't actually stop prefetching right? If batch is full, we prefetch commands in the batch? Maybe we can adjust the comment here a bit. 

## [37] src/iothread.c:363 author=ShooterIT reply=true subj=line side=RIGHT cid=2113113956
yes, it is a bit confused, how about
```
/* A single command may contain multiple keys. If the batch is full,
 * we stop adding clients to it. */
```

## [1] src/iothread.c:425 author=sundb reply=false subj=line side=RIGHT cid=2094061165
don't run this code if `HAS_BUILTIN_PREFETCH` isn't supported?

## [2] src/iothread.c:425 author=ShooterIT reply=true subj=line side=RIGHT cid=2094546138
I’d prefer not to mix in condition checks here—it would make the code a bit messy. The `resetCommandsBatch` above also needs to be checked. If you’re concerned that older GCC versions might cause the prefetch to be ineffective, we could consider lowering the compiler version requirement. WDYT?
```c
/* Supported in GCC since 3.1 but we use 4.9 given it's too old: https://gcc.gnu.org/gcc-3.1/changes.html. */
#if defined(__clang__) && (__clang_major__ > 2 || (__clang_major__ == 2 && __clang_minor__ >= 9))
#define HAS_BUILTIN_PREFETCH 1
#elif defined(__GNUC__) && (__GNUC__ > 4 || (__GNUC__ == 4 && __GNUC_MINOR__ >= 9))
#define HAS_BUILTIN_PREFETCH 1
#else
#define HAS_BUILTIN_PREFETCH 0
#endif
```

## [3] src/iothread.c:425 author=sundb reply=true subj=line side=RIGHT cid=2094701076
i have no objection to current one, let's leave it as it is.

## [9] src/iothread.c:425 author=ShooterIT reply=true subj=line side=RIGHT cid=2100139587
Hi @sundb I changed the support version is from gcc 4.8, although 4.8 is old, but it is widely used, and our `test-old-chain-jemalloc ci` also tests gcc 4.8

## [34] src/memory_prefetch.c:74 author=ShooterIT reply=true subj=line side=RIGHT cid=2113099998
it seems unused, maybe valkey wants a metric to show that, but didn't. and we have `server.stat_total_prefetch_entries`, i think it can be removed

## [35] src/memory_prefetch.c:74 author=charsyam reply=true subj=line side=RIGHT cid=2113101382
@ShooterIT I have a question. cur_idx is used in memory_prefetch.c 
do you mean it is unused?

```
/* Prefetch the given pointer and move to the next key in the batch. */                               static inline void prefetchAndMoveToNextKey(void *addr) {
    redis_prefetch(addr);
    /* While the prefetch is in progress, we can continue to the next key */
    batch->cur_idx = (batch->cur_idx + 1) % batch->key_count;
}
```

## [39] src/memory_prefetch.c:74 author=ShooterIT reply=true subj=line side=RIGHT cid=2113120908
Thank you for caring @charsyam long time no see :).`cur_idx` is needed, the keys_done is not actually used since we only increase it, but don't expose it as a metric, and `server.stat_total_prefetch_entries` has similar meaning.

## [43] src/memory_prefetch.c:74 author=tezc reply=true subj=line side=RIGHT cid=2113293856
I just pointed it in case you are not aware of it, we can keep it if we have other concerns. 

## [46] src/memory_prefetch.c:74 author=ShooterIT reply=true subj=line side=RIGHT cid=2113327911
i think we can remove it, since it does not take real effect.

## [32] src/memory_prefetch.c:78 author=ShooterIT reply=true subj=line side=RIGHT cid=2113095118
yes, in my implementation, it is unused, removed

## [29] src/memory_prefetch.c:85 author=tezc reply=false subj=line side=RIGHT cid=2112834152
```suggestion
    GetValueDataFunc get_value_data_func; /* Function to get the value data */
```

## [23] src/memory_prefetch.c:110 author=tezc reply=false subj=line side=RIGHT cid=2112755029
not sure I get it, are we doubling it to prevent misconfiguration? 

## [36] src/memory_prefetch.c:110 author=ShooterIT reply=true subj=line side=RIGHT cid=2113108664
i think prefetching in very small batches tends to be ineffective because the technique
relies on a small gap—typically a few CPU cycles—between issuing the prefetch
and performing the actual memory access. If the batch is too small, this delay
cannot be effectively inserted, and the prefetching yields little to no benefit

To avoid wasting effort, when the remaining data is small (less than twice the
maximum batch size), we simply prefetch all of it at once. Otherwise, we only
prefetch a limited portion, capped at the configured maximum.

for example, prefetch_batch_max_size is 16, if there are 30 keys, we want to prefetch them in a batch. if there are 33 keys to prefetch, we will have two batches, 16 and 17.

Therefore, I doubled the value to cover those scenarios. does it make sense?

## [6] src/memory_prefetch.c:231 author=moticless reply=false subj=line side=RIGHT cid=2097420269
Small kv objects of type string their values is embedded in the kv (i.e. `OBJ_ENCODING_EMBSTR`).
In that case, maybe we can optimize and skip the step of `PREFETCH_VALDATA`.


## [7] src/memory_prefetch.c:231 author=ShooterIT reply=true subj=line side=RIGHT cid=2097810556
in this function, we just want to prefetch the kvobject, now we still can't access the kvobject. Of course, If this dict entry just is the pointer of kvobject, we can access it, but we still need to compare the keys, we need to move to the next entry if not.

If I understand correctly, you want to avoid an iteration if the dict entry is just kvobject, please know that we already did it since we don't move to the next key in this case, just continue to the next step, as below.
```
if (!is_kv) prefetchAndMoveToNextKey(kv);
```
In next step, for `OBJ_ENCODING_EMBSTR` object, ` batch->get_value_data_func(kv);` return NULL, we will skip prefetching data.



## [24] src/memory_prefetch.c:338 author=tezc reply=false subj=line side=RIGHT cid=2112771801
Function comment mentions that goal is to bring data to L1. Then, here it says I/O thread already did the look up but on a multi core machine (most machines nowadays), iothread and main thread will be on different cores and not sharing L1. I wonder if this comment is accurate or am I missing something. Maybe this was done for a very specific CPU :man_shrugging: 

## [25] src/memory_prefetch.c:338 author=tezc reply=true subj=line side=RIGHT cid=2112787482
hmm, maybe the comment is trying to say main thread will not touch argv[0].

## [26] src/memory_prefetch.c:394 author=tezc reply=false subj=line side=RIGHT cid=2112804186
```suggestion
            batch->keys_dicts[batch->key_count] =
                kvstoreGetDict(c->db->keys, (c->slot > 0 ? c->slot : 0));
```
If I'm not mistaken, we can do above change and delete `batch->slots`.

## [31] src/memory_prefetch.c:394 author=ShooterIT reply=true subj=line side=RIGHT cid=2113091647
yes, you are right, i have noticed that, but when I saw Valkey doing this, I was too lazy to change it. OK, let me change it

