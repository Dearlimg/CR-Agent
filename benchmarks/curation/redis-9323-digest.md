# digest redis-9323 : 173 anchored candidates (of 188 total)

## [1] src/cluster.c:723 author=oranagra reply=false subj=line side=RIGHT cid=684999246
i think it may be a cleaner approach to turn on both `server.loading` and `server.async_loading` in our scenario.
this way, all the existing code that checks the `loading` flag will not need a change, and we'll only need to add a few exceptions like `server.loading && !server.async_loading`.



## [13] src/cluster.c:723 author=eduardobr reply=true subj=line side=RIGHT cid=685484844
Thanks for the review and feedback @oranagra 
Sounds good with applying the behavior only when replica was previously in sync with a master. Could the check rely on master_link_status == up?
And what about the cases where we're running standalone and replica started from an unavailable master but managed to load it's own RDB from disk (so it's a master temporarily), then master comes up shortly and starts full sync?

## [14] src/cluster.c:723 author=oranagra reply=true subj=line side=RIGHT cid=685941509
@eduardobr i gave it a lot of thought, here's what i think.

Here's a list of scenarios:
1. a replica gets disconnected a gets a full-sync when it reconnects
2. a replica was connected to one master and got redirected to a new master (of the same replication chain) and got a full sync for some reason.
3. redis was restarted from rdb file (after a graceful shutdown), and has replicaof config, attempted a partial sync with the replid it got from the rdb file, but gets a full sync.
4. the replica was a master till now and got some data from that era, then gets a REPLICAOF command and does a full-sync
5. the replica started empty and is attempting it's it's first sync ever.
6. the replica was replicating from a certain master, and is now full-syncing from another master that's completely unrelated to the previous one (i.e. imagine a sharded db with two shards, and two replicas, m1, m2, r1, r2, and now r2 that has the data of m2 is made a replica of r1, so it gonna get the data from m1. we don't want it to serve the data it has from the past).

i think the first 3 scenarios want so allow clients to read the data, but on the last 3 we don't!
i think that the rule here is that if the current dataset in the db represent a valid point in time of of the master're were full-syncing from, then we wanna allow reads, but if it doesn't represent a certain point in time of that master (possibly represents data of another master, or replication chain), then we don't want to serve reads.

I think the only way to tell that is if in `slaveTryPartialResynchronization` we get a full-sync with the same replid that we asked for (different offset since the offset we asked for doesn't exist in the backlog anymore).

the disadvantage is that there's one case that it'll miss: if the master at the head of the replication chain changed, and we switched the replid, then we reconnect and instead of getting successful partial-sync, we happen to get a full one. in this case we have no way to tell that in fact the old replid is actually an valid point in time of the new one.

this explanation is a bit long and messy, i hope i managed to convey the idea.

## [20] src/cluster.c:723 author=eduardobr reply=true subj=line side=RIGHT cid=686826035
Thinking about the case that is hard to deal with:

> "if the master at the head of the replication chain changed, and we switched the replid, then we reconnect and instead of getting successful partial-sync, we happen to get a full one."

I assume this is case 6.
When could that happen (that a replica will follow a master of another shard)? Is that only if some REPLICAOF command was raised?

Could we track if any REPLICAOF was executed since last successful replication? Then if there was any, we simply don't serve reads during replication. Sounds like a simple bit value that we set 1 when REPLICAOF is executed and reset to 0 when successful replication finishes.

## [21] src/cluster.c:723 author=oranagra reply=true subj=line side=RIGHT cid=687475169
the case i was describing last, which is not part of 1..6 is this:

* imagine a replication chain A <- B <- C (i.e. A is the master at the head of the chain)
* now normally, if B is promoted to be the new master, and A becomes a replica of B or C, what will happen is that B generates a new replid, and disconnects C. C then re-connects and succeeds a partial sync and acquires the new replid
* but if instead C takes too long to reconnect and the PSYNC fails and results in a full-sync, then it'll see that the replid it got is different than the one it asked for, and in our case (if we choose to take my suggestion in this comment), will avoid doing a swapdb based replication (or at least avoid serving traffic during the loading).

Lets call this case 7. it is different than 6 since in 6 we were talking of two different masters, not holding different points in time of the same data.

I think this compromise is ok, and i don't think case 7 is very likely, so i still think we can continue with the suggestion i gave in my comment.

@soloestoy @madolson @guybe7 @yossigo feel free to suggest a better idea.
the context is, how to decide in which cases to use swapdb based diskless repl and still serve clients during loading, and in which cases to avoid that (read my suggestion [here](https://github.com/redis/redis/pull/9323#discussion_r685941509))

## [22] src/cluster.c:723 author=eduardobr reply=true subj=line side=RIGHT cid=687498993
Applying this logic, when you have a setup with cluster disabled containing 1 master and some replicas and the master restarts, then we still won't serve reads during full sync because master will come up with a new replication id, but if it's not cluster and we know that no REPLICAOF was issued, isn't always the case it's safe to continue serving reads?

## [23] src/cluster.c:723 author=oranagra reply=true subj=line side=RIGHT cid=687633866
@eduardobr you're right, but that's actually another bug. when the master restarts the replica shouldn't do full-sync: #8015
I think that counting REPLICAOF commands, or keeping track of whether or not the REPLICAOF command was with the same ip+port pair or different ones, is the wrong thing to do.
also note that it would also mean that when you switch to replicate from one source replica to another (in a replica chain), this will fail (we'll think we got connected to a new master), whereas my replid design will succeed.
in fact we have that replid mechanism for exactly that reason (to know which master/timeline we're part of).

We can even solve the case that i said is problematic (case 7), if we improve the protocol. i.e. some future PSYNC can use multiple replid+offset pairs when asking for sync, or and the PSYNC reply can carry multiple pairs back, and even carry part of the backlog. but we should lave that out of the scope now, since it requires protocol changes.

## [24] src/cluster.c:723 author=eduardobr reply=true subj=line side=RIGHT cid=687673837
Thanks for clarifying @oranagra 

> also note that it would also mean that when you switch to replicate from one source replica to another (in a replica chain), this will fail (we'll think we got connected to a new master), whereas my replid design will succeed

That suggestion about checking for an issued REPLICAOF would be only when replid is different in the handshake (and only for cluster disabled if that helps). But I see the point to make a proper fix in the protocol.
Also, the PR mentioned relies on master loaded from RDB (I guess masters using RDB as primary persistence are not common anymore because that has high data loss potential and AOF is just available to be used, or I’m wrong?)

In summary, I’ll be glad to move on, but the original issue I thought to be solving wouldn’t be solved, even though there are still other improvements.

Drifting a bit, the scenario I’m trying to cover is to make redis more reliable when used as a primary database and not only as cache. I’m using a standalone setup to achieve virtually no data loss (having fsync=always in master for example). But it’s impossible to restart master in this scenario without putting down all replicas afaik (unless there’s some manual job to disconnect them and reconnect one by one). But in Kubernetes everything could happen.


## [25] src/cluster.c:723 author=oranagra reply=true subj=line side=RIGHT cid=687998750
@eduardobr you're referring to graceful restart, right? (Not a crash recovery) 
See the comment I posted in https://github.com/redis/redis/pull/8015#issuecomment-897876142 I think maybe that can solve both of your problems, and also get you faster startup time. 

I don't like the idea of matching REPLICAOF commands and looking at their args. 
I do like to solve this problem for graceful restarts, even when AOF is configured. 
P. S. There is some [discussion](https://github.com/redis/redis/discussions/9282) about annotating AOF, but I still think it's not much good for graceful restarts, only crash recovery. 

## [26] src/cluster.c:723 author=oranagra reply=true subj=line side=RIGHT cid=691922936
I had a lengthy discussion about this with @yossigo.
we think the current plan is good.
i.e.
* If there's a PSYNC, there's no problem.
* If there's a FULL sync with the same replid, and the user uses repl-diskless-load=swapdb, we'll keep serving reads.
* If there's both a FULL sync and a replid change (either because of replication topology change or a master restart), we won't serve reads during sync.
* however, since we're gonna implement #8015, we don't expect FULL syncs after a graceful master restart.

we can proceed to implement this mechanism then.

## [28] src/cluster.c:723 author=eduardobr reply=true subj=line side=RIGHT cid=694827528
@oranagra I believe the last commit settles this strategy then. Added an extra test as well.

## [120] src/cluster.c:6118 author=oranagra reply=false subj=line side=RIGHT cid=711250886
I think that the change in your last commit is technically correct, but I must say I don't like it too much. 
I think it would be better to move this structure to the redisDb struct (to be allocated only when cluster is enabled). 
This way dbAdd would handle it correctly, and the tempDB we allocate during replication would be enough (no need for an extra member in the cluster struct). 
I think it's OK for server.h to be aware of the slots count, it doesn't have to be abstracted in cluster.h. 
@madolson WDYT?

@eduardobr btw, maybe we better block async loading of someone enabled a writable replica? 
I don't usually care for writable replica, enabling it is an invitation but bugs and inconsistencies.
But looking at the current code ("forChanging"), it'll obviously do the wrong thing.. 

## [121] src/cluster.c:6118 author=eduardobr reply=true subj=line side=RIGHT cid=711267696
I started with that solution, but then felt forced to have it in a public place in order to not flood methods with tempDb argument. But also agree it would be more self-contained.

About writable replicas, they are more for caching calculated results, like those commands with STORE flag and things like that, right? I'm indifferent, your call ;)
BTW, now that we just need minor changes, please feel free to make commits. Unfortunately I'll be short on time during next week, then I'm more available from the 26th.

As I see, we have:
- This possible refactoring for the placing temporary slots_to_keys in different location https://github.com/redis/redis/pull/9323#discussion_r711726175
- Adjust some async_loading conditionals as mentioned here https://github.com/redis/redis/pull/9323#discussion_r709567812
- Maybe better support deprecated module api https://github.com/redis/redis/pull/9323#discussion_r708332098, which maybe is just about doing this: https://github.com/redis/redis/pull/9323#discussion_r709087671


## [124] src/cluster.c:6118 author=zuiderkwast reply=true subj=line side=RIGHT cid=711991355
I like the idea of putting the slot-to-key mapping (pointer) inside the redisDb struct. Then, a temp db can just be a redisDb and we don't need a specific tempDb struct.

The slot-to-key mapping can still be opaque and delegated from db to cluster if we like, independently of that.

## [158] src/cluster.c:6118 author=eduardobr reply=true subj=line side=RIGHT cid=735156282
@zuiderkwast Thanks for all the suggestions. Closing as this was implemented.

## [138] src/cluster.c:6182 author=oranagra reply=false subj=line side=RIGHT cid=725636565
i think it looks odd that we have that method.
the code is identical to the other one, and the `tempDb` argument is not necessarily temp (could be the real db)...
i suggest to have just one method doing memset, which takes a db.
if we wanna avoid modifying the few calls to slotToKeyFlush, then we need to make it a wrapper method (that take no arguments and calls the other one)

## [59] src/config.c:2694 author=oranagra reply=false subj=line side=RIGHT cid=707329882
re-posting an old comment that got lost:
i think it may be a cleaner approach to turn on both `server.loading` and `server.async_loading` in our scenario.
this way, all the existing code that checks the `loading` flag will not need a change, and we'll only need to add a few exceptions like `server.loading && !server.async_loading`.

## [100] src/config.c:2694 author=eduardobr reply=true subj=line side=RIGHT cid=709567812
Made the change, but need to confirm the places where it should be `(server.loading && !server.async_loading)`

From my search, this is what needs to be analyzed:
module.c
```
int RM_GetContextFlags(...)
    ...
    if (server.loading)
        flags |= REDISMODULE_CTX_FLAGS_LOADING;
```

multi.c
```
void execCommand(...)
    ...
    call(c,server.loading ? CMD_CALL_NONE : CMD_CALL_FULL);
```

rdb.c
```
void rdbReportError(...)
    ...
    if (!server.loading) {
        /* If we're in the context of a RESTORE command, just propagate the error. */
        /* log in VERBOSE, and return (don't exit). */
        serverLog(LL_VERBOSE, "%s", msg);
        return;
    }

...
serverLog(server.loading? LL_WARNING: LL_VERBOSE, "rdbLoadLzfStringObject failed allocating %llu bytes", (unsigned long long)clen);
...
serverLog(server.loading? LL_WARNING: LL_VERBOSE, "rdbLoadLzfStringObject failed allocating %llu bytes", (unsigned long long)len);
...
serverLog(server.loading? LL_WARNING: LL_VERBOSE, "rdbGenericLoadStringObject failed allocating %llu bytes", len);
...
serverLog(server.loading? LL_WARNING: LL_VERBOSE, "rdbGenericLoadStringObject failed allocating %llu bytes", len);
```

scripting.c
```
    /* Write commands are forbidden against read-only slaves, or if a
     * command marked as non-deterministic was already called in the context
     * of this script. */
    if (cmd->flags & CMD_WRITE) {
        int deny_write_type = writeCommandsDeniedByDiskError();
        if (server.lua_random_dirty && !server.lua_replicate_commands) {
            luaPushError(lua,
                "Write commands not allowed after non deterministic commands. Call redis.replicate_commands() at the start of your script in order to switch to single commands replication mode.");
            goto cleanup;
        } else if (server.masterhost && server.repl_slave_ro &&
                   !server.loading &&
                   !(server.lua_caller->flags & CLIENT_MASTER))
        {
            luaPushError(lua, shared.roslaveerr->ptr);
            goto cleanup;
        }
...

    /* If we reached the memory limit configured via maxmemory, commands that
     * could enlarge the memory usage are not allowed, but only if this is the
     * first write in the context of this script, otherwise we can't stop
     * in the middle. */
    if (server.maxmemory &&             /* Maxmemory is actually enabled. */
        !server.loading &&              /* Don't care about mem if loading. */
        !server.masterhost &&           /* Slave must execute the script. */
        server.lua_write_dirty == 0 &&  /* Script had no side effects so far. */
        server.lua_oom &&               /* Detected OOM when script start. */
        (cmd->flags & CMD_DENYOOM))
    {
        luaPushError(lua, shared.oomerr->ptr);
        goto cleanup;
    }
...
    /* If this is a Redis Cluster node, we need to make sure Lua is not
     * trying to access non-local keys, with the exception of commands
     * received from our master or when loading the AOF back in memory. */
    if (server.cluster_enabled && !server.loading &&
        !(server.lua_caller->flags & CLIENT_MASTER))
    {
        int error_code;
        /* Duplicate relevant flags in the lua client. */
```

## [104] src/config.c:2694 author=oranagra reply=true subj=line side=RIGHT cid=709892891
* module.c - i think we can keep the current code that depends only on `loading` so the module will know we're in loading state.
* multi.c - this is complicated, that code decides if `call` should put things in slowlog / stats and propagate to the AOF / replicas. so this may depend if the EXEC was received from the AOF file or another client, in either case, we don't want to propagate (and it'll also not allow any write commands), but for the slowlog and stats we wanna make that distinction.
* rdb.c - this may be similar to the above, i.e. a RESTORE command can fail differently when called from AOF vs a client. but actually, we don't expect a RESTORE command during loading on a read-only replica, so i think the code can stay as is (use `server.loading)
* scripting.c:
  * write commands - i don't currently understand how we could have got an EVAL command on a replica during loading before this PR. maybe that means a replica that's restarting from AOF? in that case, we wanna make a distinction again depending on the source of the command. commands that arrived from AOF should never fail, and ones that arrive from clients should.
  * oom - similar to the above. before this PR, EVAL commands from clients would be blocked during loading, and the check here was in order to never fail EVALs from AOF. we need to make a distinction depending on the client that triggered it.
  * cluster - same as the above.

bottom line, the main complication here is to detect the client while loading.
we could check `c->id == CLIENT_ID_AOF` instead of `server.loading`
or in the case of scripting.c check `server.lua_caller`.

@guybe7 @soloestoy i would love if you can validate these.

## [107] src/config.c:2694 author=guybe7 reply=true subj=line side=RIGHT cid=710244303
in all the cases above (apart from module.c) we should check the client instead of server.loading

in module.c I suggest adding REDISMODULE_CTX_FLAGS_ASYNC_LOADING

## [108] src/config.c:2694 author=oranagra reply=true subj=line side=RIGHT cid=710276214
Ok about module.c we can add that. 
And indeed most other cases actually meant to check if the client is the aof client rather than look at the loading flag. 
However for rdb.c it's a bit more complicated, since the loading could be either RESTORE command (coming from the master client, an AOF or a normal client), but could also be an RDB file (not a command). 
So fat the loading flag check meant to distinguish between a restore command and an RDB file loading... 
So now we'll need two checks, first see if this is a command or an RDB file, and then check which client issued it. 
However, since we don't expect RESTORE commands during async loading, we can decide to keep the current code too..

On a second thought, I'd vote for the complicated one (being more explicit) 

## [130] src/config.c:2694 author=eduardobr reply=true subj=line side=RIGHT cid=720852157
@oranagra I don't quite get the c->id == CLIENT_ID_AOF check as solution. I'm missing something.
Let's take multi.c
```
if (c->id == CLIENT_ID_AOF)
    call(c,CMD_CALL_NONE);
else
    call(c,CMD_CALL_FULL);
```
For all situations:
- No SYNC or ASYNC loading: we're fine with this code, always call FULL.
- ASYNC loading, command is not from RDB or AOF: ok, will call FULL
- ASYNC loading, command from AOF: ok, will call NONE
- ASYNC loading, command from RDB (replication, for example): ?
- SYNC loading: what if it's loading from RDB, CLIENT_ID_AOF won't cover it and we will end up calling FULL, right?

## [131] src/config.c:2694 author=oranagra reply=true subj=line side=RIGHT cid=720866974
I think that what you're missing is that RDB file doesn't contain commands, just raw data. So the only ways for `call` to be called during loading is that it's either from the AOF fake client or real client connection. 

## [133] src/config.c:2694 author=eduardobr reply=true subj=line side=RIGHT cid=720876004
Changes done, but the complicated way for rdb.c would get quite complicated as we don't have the client there.
Do you believe it worth passing around for the explicitness?

## [134] src/config.c:2694 author=oranagra reply=true subj=line side=RIGHT cid=720888834
In rdb.c we can use `server.current_client`
If it's NULL then we're processing an RDB file (or a preamble AOF). 
If it's non-null we can check the ID to see if it's the AOF client. 
If it non null and not the aof client, then it's a restore command.
Like other places, no need to look at the `loading` flag at all. 
Let's wrap that in some macro at the top and document that. 

## [154] src/db.c:452 author=oranagra reply=false subj=line side=RIGHT cid=730451581
isn't that change adding a leak? it used to do only memset, and now it also does malloc.

## [155] src/db.c:452 author=eduardobr reply=true subj=line side=RIGHT cid=730460855
Before, slots_to_keys was a variable declared in clusterState struct, now it became just a pointer in redisDb struct.
If it wasn't a pointer, we would have automatically wasted memory the size of CLUSTER_SLOTS for each db in the array.
We just need it to be allocated for db[0]. We actually refer to it without providing the array index all the time.
About leak, aren't the calls to `slotToKeyDestroy` enough?

## [156] src/db.c:452 author=oranagra reply=true subj=line side=RIGHT cid=730461673
which call to slotToKeyDestroy? where is it called in the context of emptyDB?
note that `emptyDB` is called every time user runs FLUSHDB. and also on normal disk-based full sync.

## [157] src/db.c:452 author=eduardobr reply=true subj=line side=RIGHT cid=730473927
Hah, right. Implemented slots to keys as opaque type in last commit. This fix included.

## [2] src/db.c:483 author=oranagra reply=false subj=line side=LEFT cid=685044438
unlike before, now we're not copying the other db members (e.g. blocking_keys, watched_keys).
we should at least set them all to NULL (calloc or memset).

## [15] src/db.c:483 author=eduardobr reply=true subj=line side=LEFT cid=686703222
Trying understand better (sorry the lack of C skills)
`blocking_keys, watched_keys, ready_keys` are all dictionaries the same way as dict and expires, but only `dict` and `expires` are swapped in the end.
Does it make sense to init `dict` and `expires` with NULL and then `blocking_keys, watched_keys, ready_keys` with `dictCreate`?

## [16] src/db.c:483 author=oranagra reply=true subj=line side=LEFT cid=686714639
you can either just replace the `tempDb->dbarray = zmalloc()` with `calloc` (zeros the memory it allocates).
or, inside your loop over `server.dbnum`, you can `memset(&tempDb->dbarray[i], 0, sizeof(redisDb));`.

in theory, we should have created an array that only holds the `dict` and `expires` members, but since we pass that `redisDb*` to functions that work with that type, we can't.
so i'm just trying to make sure we have NULL pointers there, and not random (uninitialized) ones.

## [3] src/db.c:497 author=oranagra reply=false subj=line side=RIGHT cid=685124417
we may have a few issues with modules (the more complicated ones, which have out of keyspace global data).

first, till now the modules were aware of a concept of backup and restore, and who knows what they actually do with it, and now instead they may need to be aware of a concept of temp db.

secondly, they may rely on the fact that during that loading time no commands are received.

thirdly, some may even themselves call RM_Call commands on the data during loading.

@MeirShpilraien please share your thoughts.

p.s. i think it may be ok to avoid using this mode when modules are involved, or maybe decide to make a breaking change in the module API around that area, i think not many modules started using it, and not many people use this SWAPDB feature.

## [17] src/db.c:497 author=MeirShpilraien reply=true subj=line side=RIGHT cid=686786804
@oranagra I am not aware of a module that performance RM_Call during load. But modules definitely assume there will be no traffic during load. with this PR we will have to change this assumption. I believe, in order not to break existing modules, we will need to disable this new feature if a module does not state that it supports it (just like with short-read error during rdb load). WDYT?

## [27] src/db.c:497 author=oranagra reply=true subj=line side=RIGHT cid=691928309
This change is gonna break modules that use the `RedisModuleEvent_ReplBackup` mechanism anyway, so i suggest the following:

1. We deprecate `RedisModuleEvent_ReplBackup`. i.e. starting redis 7.0, we never fire that event. we do that since in the new "swapdb" mechanism there's no concept of backup and restore, instead there's a concept of temporary db.
2. We create a new API named `RedisModuleEvent_ReplAsync`, holding 3 sub-events; STARTED, COMPLETEED, ABORTED.
3. We add another module flag for `RedisModule_SetModuleOptions` with which the module can declare it supports this mechanism, i.e. REDISMODULE_OPTIONS_HANDLE_REPL_ASYNC.
4. in replication.c, If there are modules loaded which registered a data type, and didn't declare they're supporting this, we fall back to the alternative (either the `on-empty-db` diskless replication, or disk-based replication).

## [31] src/db.c:497 author=eduardobr reply=true subj=line side=RIGHT cid=695956417
@oranagra Changes for module events done in last commit.
Ajusting the current tests soon, but I would like to hear how much of the deprecated events we need to keep first.

## [142] src/db.c:500 author=zuiderkwast reply=false subj=line side=RIGHT cid=725662202
The slot-to-key array needs to be free'd somewhere before the tempDb is free'd. Flush just memsets it to zero.

Maybe we can replace slotToKeyFlush with two functions: slotToKeyInit (allocates it) and slotToKeyDestroy (frees it)?

## [8] src/db.c:1315 author=oranagra reply=false subj=line side=RIGHT cid=685446527
this function is not as generic as it seems (by it's name), the implementation and behavior are quite specific for placing the newly loaded temp db at the main active one, and the old active one in the temp.
logically, that's where we change the main one....
so i think we need a better name for it, and we certainly need a beefier doc comment.

## [143] src/db.c:1322 author=zuiderkwast reply=false subj=line side=RIGHT cid=725663704
This code doesn't swap them. It just copies one to the other. Don't we want to swap them like we do for dict, expires, avg_ttl and expires_cursor below?

Also, we don't need memcpy here. We can just swap the pointers, like this:

If we want to swap them, don't we want to do something like this?

```Suggestion
        clusterSlotsToKeysData *tmp = server.db->slots_to_keys;
        server.db->slots_to_keys = tempDb->slots_to_keys;
        tempDb->slots_to_keys = tmp;
```

... or simpler, we can do this for all db numbers in the for loop below. It will be NULL for all DBs except 0, but it does matter IMO.

## [151] src/db.c:1322 author=eduardobr reply=true subj=line side=RIGHT cid=730447575
Thank you for the suggestions @zuiderkwast. Commited changes for swapping pointers instead of memcpy and splitting slotToKeyFlush into 2 functions

## [61] src/db.c:1339 author=oranagra reply=false subj=line side=RIGHT cid=707580073
did you copy that from somewhere?
it look logical, but maybe missing from the old code, even in a non-swapdb-based diskless loading..

## [83] src/db.c:1339 author=eduardobr reply=true subj=line side=RIGHT cid=707665181
I have raised this question to @zuiderkwast here: https://github.com/redis/redis/pull/9323#issuecomment-918484591

## [86] src/db.c:1339 author=zuiderkwast reply=true subj=line side=RIGHT cid=707698235
It's right, but it's better if the implentation of slot-to-key can be hidden in cluster.c. slotToKeyCopyToBackup and slotToKeyRestoreBackup did this.

I have an idea about this. I'll comment more tomorrow.

## [87] src/db.c:1339 author=oranagra reply=true subj=line side=RIGHT cid=708041030
@eduardobr i was asking about the `dirty++`

## [91] src/db.c:1339 author=eduardobr reply=true subj=line side=RIGHT cid=708206435
Oh, yes. I saw this in many places where db is touched and felt uncomfortable in not having it here as well.

## [93] src/db.c:1339 author=oranagra reply=true subj=line side=RIGHT cid=708303396
i was thinking for a moment that this should actually be incremented by the number of keys in the db, and that this should certainly be done in any diskless replication rather than just on swapdb mode.

i.e. in disk-based replication, the rdb we got from the master serves for persistence too, so server.dirty can be set to 0, but in diskless, it should be incremented for each key we load.
(somewhat related to https://github.com/redis/redis/pull/6217)

however, i see that on disk-based we don't reset it to 0, so i'm not certain if we should touch it in diskless at all.
@yossigo @soloestoy @madolson please let me know if there's anything i'm missing.

## [97] src/db.c:1339 author=yossigo reply=true subj=line side=RIGHT cid=709091202
@oranagra The save policy refers to "write operations", not sure if loading an entire rdb should be considered as N operations depending on number of keys or just a single operation. I lean towards leaving it as is.

## [99] src/db.c:1339 author=oranagra reply=true subj=line side=RIGHT cid=709250331
ok, by "as is" i assume you mean to increment the counter only once.
i.e. before this PR it wasn't incremented at all.
and if we do that, we should increment it also in any of the other diskless loading modes (not just swapdb / async).
@eduardobr please make it happen, and also mention this "bugfix" in the top comment of the PR (will be used for release notes and commit comment when squash-merging)

## [105] src/db.c:1339 author=soloestoy reply=true subj=line side=RIGHT cid=709916298
I checked the codes about `server.dirty`, sadly its usages and definitions are ambiguous(I can't figure it out it means how many keys changed or event happened), and expiration and eviction doesn't increment it neither, I will open a new issue to discuss it.

## [62] src/db.c:1464 author=oranagra reply=false subj=line side=RIGHT cid=707581188
why is that done here? (there's another copy of the same logic below)

maybe a merge conflict resolution issue..

## [77] src/db.c:1464 author=eduardobr reply=true subj=line side=RIGHT cid=707637731
Seems to be really some merge issue, but also interesting, why it's not the first statement? Seems cheaper than the getExpire thing to return 0 (to be done in some other PR if that's the case, I'm reverting). 

## [43] src/module.c:4813 author=oranagra reply=false subj=line side=RIGHT cid=698018532
```suggestion
 * REDISMODULE_OPTIONS_HANDLE_REPL_ASYNC_LOAD, in which case diskless async loading should be avoided
 * because module doesn't know there can be traffic during database full resynchronization. */
int moduleAllDatatypesHandleReplAsyncLoad() {
```

## [80] src/module.c:4819 author=oranagra reply=false subj=line side=RIGHT cid=707643554
styling: now that the `if` is a one liner, the curly brackets go in that same line.

## [32] src/module.c:4820 author=oranagra reply=false subj=line side=RIGHT cid=698016644
@MeirShpilraien it occurred to me for a moment that maybe the "data type" part should be dropped, since modules with aux data can also be affected.
then i realized that we are lucky that these aux fields are only possible when data type being registered.

so i just wanna check with you that you don't see any problem with other modules, i.e. ones that use RM_Call or RM_OpenKey

## [56] src/module.c:4820 author=MeirShpilraien reply=true subj=line side=RIGHT cid=707308254
@oranagra I actually believe that we should not check if a module created a datatype here, a module might need to be aware of this feature even if it does not have a datatype. For example, a module that collects stats about the keyspace. Such module will probably register on keyspace events, it will collect stats and it will want to be aware of possible swap db to know how to handle loaded events correctly. I agree that without loaded events, only modules with datatypes need to be aware of this feature, but the loaded event breaks this assumption.

## [57] src/module.c:4820 author=oranagra reply=true subj=line side=RIGHT cid=707310256
ok. thanks.
so let's remove the `listLength(module->types)` part

## [75] src/module.c:4820 author=eduardobr reply=true subj=line side=RIGHT cid=707632310
So I guess method name becomes `moduleHandleReplAsync`? (see change)


## [79] src/module.c:4820 author=oranagra reply=true subj=line side=RIGHT cid=707641443
actually, `module` is the prefix for all methods.
so maybe `moduleAllModulesHandleReplAsync`?

## [37] src/module.c:8354 author=oranagra reply=false subj=line side=RIGHT cid=698017364
```suggestion
 * * RedisModuleEvent_ReplAsyncLoad
```

## [39] src/module.c:8365 author=oranagra reply=false subj=line side=RIGHT cid=698017992
```suggestion
 *     * `REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_STARTED`
 *     * `REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_ABORTED`
 *     * `REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_COMPLETED`
```

## [40] src/module.c:8446 author=oranagra reply=false subj=line side=RIGHT cid=698018022
```suggestion
    case REDISMODULE_EVENT_REPL_ASYNC_LOAD:
        return subevent < _REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_NEXT;
```

## [41] src/module.c:8986 author=oranagra reply=false subj=line side=RIGHT cid=698018096
```suggestion
    if (module->options & REDISMODULE_OPTIONS_HANDLE_REPL_ASYNC_LOAD)
        output = sdscat(output,"handle-repl-async-load|");
```

## [38] src/module.c:9173 author=oranagra reply=false subj=line side=LEFT cid=698017782
@yossigo if we deprecate `RedisModuleEvent_ReplAsync`, we surely still wanna keep it in the header file, so old modules can still handle it (although their code that handles it is now dead code) when used on new redis.
but what about the documentation? do we wanna drop the old API from the documentation, or keep it here with a deprecation notice?

## [96] src/module.c:9173 author=yossigo reply=true subj=line side=LEFT cid=709087671
I think we need to keep the documentation and mention it is deprecated, as well as use `__attribute__((deprecated))` to produce a compile time warning.

## [98] src/module.c:9173 author=oranagra reply=true subj=line side=LEFT cid=709248488
ok. @eduardobr, please revive the doc comment and add the deprecation notice and attribute

## [125] src/module.c:9173 author=eduardobr reply=true subj=line side=LEFT cid=716195028
Change made, but I couldn't identify based on current code how to precisely apply the deprecated attribute in some cases. For example, before or after the elements?

## [126] src/module.c:9173 author=oranagra reply=true subj=line side=LEFT cid=716195617
i'm not sure myself. i suggest you try to revive the test code that uses them and see that you get the warning.
i guess at least one warning is ok too (if we can't get them all to work).

## [127] src/module.c:9173 author=eduardobr reply=true subj=line side=LEFT cid=716974729
In current state, it will generate this warning on ./runtest-moduleapi
![image](https://user-images.githubusercontent.com/3770445/134970494-e6c49ec6-559f-4081-bddf-8824fa391041.png)
Using the attribute on REDISMODULE_SUBEVENT_REPL_BACKUP_* variables wouldn't work.
Please let me know if that's the desired outcome of the deprecation overall

## [128] src/module.c:9173 author=oranagra reply=true subj=line side=LEFT cid=718208183
i think one warning is enough.
and i think we should convert the test in `tests/modules/testrdb.c` to use the new API.
no sense in keeping the dead code anyway (apart from getting the annoying deprecation warning).

## [129] src/module.c:9173 author=eduardobr reply=true subj=line side=LEFT cid=720669329
Just one thing before I commit. Is the goal of testrdb.c:replBackupCallback to check if the order of events is correct?
We could use some comments to understand this file a bit better.

## [132] src/module.c:9173 author=oranagra reply=true subj=line side=LEFT cid=720868215
i think it was attempting to use the callback for the purpose for which they were added, and see that they work correctly.
i.e. it's a module with global data, that's stored into the aux fields of the rdb, and it was using the callbacks to create a backup and restore / discard it.

similarly i guess the new test should use the new apis, and check that during rdb loading the old data is still accessible, and that if loading fails the temp data (that's already been loaded) is freed correctly, and that it the loading succeeds, the new data is accessible and the old one is released.

## [136] src/module.c:9173 author=eduardobr reply=true subj=line side=LEFT cid=725479740
@oranagra I've been learning the last few days about the module API by looking documentation and code but couldn't understand the reasoning behind the replBackupCallback in testrdb.c. Also, I couldn't find what actually uses it or tests it (runtest-moduleapi tests pass if we just remove the callback in current unstable branch).

Probably we need good documentation about the aux_save and aux_load and how it plays with replication and SWAPDB (independent of previous implementation or upcoming with this PR).
Currently:
```
aux_save: A callback function pointer that saves out of keyspace data to RDB files. 'when' argument is either REDISMODULE_AUX_BEFORE_RDB or REDISMODULE_AUX_AFTER_RDB.

aux_load: A callback function pointer that loads out of keyspace data from RDB files. Similar to aux_save, returns REDISMODULE_OK on success, and ERR otherwise.
```

We could use some description on what REDISMODULE_AUX_BEFORE_RDB and REDISMODULE_AUX_AFTER_RDB are. Is it before/after saving/loading it? Is it where the aux variables are stored phisically in the RDB stream (before it or after it)? I've learned that the last is the case by reading the code, but "when" could be renamed to "where" to be explicit it's not a moment in time but a position in the stream.

As I understand now, what we'll need to do for the new test - to demonstrate what a real module would need to do - is:
- Change aux_load to start loading the global vars from stream into equivalent temp vars (if server.async_loading or if some flag set by STARTED tells the async load is in progress (not sure what is better)).
- On ReplAsync COMPLETED event, set the aux to the temp aux and clear the temp aux.
- On ReplAsync ABORTED event, clear temp aux.

Makes sense or I missed some part?

Thanks

## [137] src/module.c:9173 author=oranagra reply=true subj=line side=LEFT cid=725602971
the `when` means it is called before and after the callbacks to save / load the keys.
for example, the module has an opportunity to store the total count of keys before the actual keys (so on loading an array can be pre-allocated) or store any other info that he'll need when loading individual keys, or compute some some checksum on their values during serialization and then store that at the end of the rdb.
maybe the documentation is not clear enough and needs improvements.

you're right that the current code passes even if the callback is completely skipped. i assume it still had some value detecting issues from when it **is** called (like bugs and leaks)

your plan seems right, maybe in order to verify this is really working, we need to add some assertions in the tcl code.
like make sure to modify these "runtime" values during / before async loading, and see that we get the correct value during loading, and also the correct value in case the loading was aborted or succeeded.

## [67] src/rdb.c:2483 author=oranagra reply=false subj=line side=RIGHT cid=707599734
as suggested earlier, i think the `loading` flag should always be set, even when `async_loading` is also set.
this will let you revert many lines where you did `loading || async_loading` or `!loading && !async_loading`.

## [63] src/redismodule.h:233 author=oranagra reply=false subj=line side=RIGHT cid=707585261
```suggestion
#define REDISMODULE_OPTIONS_HANDLE_REPL_ASYNC_LOAD    (1<<1)
```

## [33] src/redismodule.h:254 author=oranagra reply=false subj=line side=RIGHT cid=698016919
```suggestion
#define REDISMODULE_EVENT_REPL_BACKUP 12 /* Deprecated since Redis 7.0, not used anymore. */
```

## [34] src/redismodule.h:256 author=oranagra reply=false subj=line side=RIGHT cid=698016984
```suggestion
#define REDISMODULE_EVENT_REPL_ASYNC_LOAD 14
```

## [35] src/redismodule.h:317 author=oranagra reply=false subj=line side=RIGHT cid=698017048
```suggestion
    /* Deprecatedsince Redis 7.0, not used anymore */
```

## [36] src/redismodule.h:323 author=oranagra reply=false subj=line side=RIGHT cid=698017112
```suggestion
    RedisModuleEvent_ReplAsyncLoad = {
        REDISMODULE_EVENT_REPL_ASYNC_LOAD,
```

## [42] src/redismodule.h:383 author=oranagra reply=false subj=line side=RIGHT cid=698018164
```suggestion
#define REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_STARTED 0
#define REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_ABORTED 1
#define REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_COMPLETED 2
#define _REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_NEXT 3
```

## [4] src/replication.c:1534 author=oranagra reply=false subj=line side=RIGHT cid=685410493
I think this method belongs in db.c, maybe we want a thin wrapper for it here, or maybe we wanna give up all the thin wrappers (their main purpose was their doc comments i think)
```suggestion
/* Logically, this discards (flush) the old main database, and apply the newly loaded
 * database (temp) as the main (active) database, the actual freeing of old database
  * (which will now be placed in the temp one) is done later. */
void swapMainDbWithTempDb(tempDb* tempDb) {
```

## [19] src/replication.c:1534 author=eduardobr reply=true subj=line side=RIGHT cid=686799366
Added your method comment, renamed to swapMainDbWithTempDb, moved to db.c (merging with dbSwapAllDatabases), removed thin wrapper from replication.c.

## [5] src/replication.c:1734 author=oranagra reply=false subj=line side=RIGHT cid=685422022
you mean the call to emptyDb? i think that's exactly what you intended.
i.e. you're at this point rdb.c won't touch the main active db, will only load data into the temp one, and only when you swap them eventually you're logically flushing the old one.
for WATCHed keys, etc, we'll handle everything then, and for modules, there's no good way to tell what can happen, we'll discuss that in the different comment.

or am i missing your point in that TODO?



## [9] src/replication.c:1734 author=eduardobr reply=true subj=line side=RIGHT cid=685463726
The TODO was more questioning what is mentioned in the original comment:
/* OLD COMMENT: We call to emptyDb even in case of REPL_DISKLESS_LOAD_SWAPDB
        * (Where disklessLoadMakeBackup left server.db empty) because we
        * want to execute all the auxiliary logic of emptyDb (Namely,
        * fire module events) */
        
Mainly about modules, so touching this again after discussion on how to handle them.

## [64] src/replication.c:1748 author=oranagra reply=false subj=line side=RIGHT cid=707589752
some of your multi-line block comments have indentation issues .
in this case it's not even a new comment, so i suppose maybe your editor is doing this?
please go over them.
```suggestion
     * handler, otherwise it will get called recursively since
     * rdbLoad() will call the event loop to process events from time to
     * time for non blocking loading. */
```

## [29] src/replication.c:1749 author=oranagra reply=false subj=line side=RIGHT cid=695163347
```suggestion
        if (server.repl_diskless_load == REPL_DISKLESS_LOAD_SWAPDB) {
            /* Async loading means we continue serving read commands during full resync, and
             * "swap" the new db with the old db only when loading is done.
             * It is enabled only on SWAPDB diskless replication when master replication ID hasn't changed.
             * because in that state the old content of the db represents a different point in time of the same
             * data set we're currently receiving from the master. */
            if (memcmp(server.replid, server.master_replid, CONFIG_RUN_ID_SIZE) == 0) {
```

## [30] src/replication.c:1755 author=oranagra reply=false subj=line side=RIGHT cid=695163690
some styling issues.. i can also fix later, when we're near complete...
```suggestion
        } else {
```

## [65] src/replication.c:1767 author=oranagra reply=false subj=line side=RIGHT cid=707593878
i now realize (or maybe i managed to forget it), that there's a case in which we'll do a swapdb diskless loading, but without async loading, i.e. we don't allow reads during loading.

in that scenario, maybe we still wanna support the old module api (the one about backups)? and still do swapdb?
current code will just fall back to disk-based loading.

i must say i don't like my suggestion above (too complicated), but wanted to raise it anyway.

## [84] src/replication.c:1767 author=eduardobr reply=true subj=line side=RIGHT cid=707669496
_"current code will just fall back to disk-based loading."_

Maybe it's the late time of the day =D, but I can't see this. It should still do the diskless load, but returning LOADING status, right?

## [88] src/replication.c:1767 author=oranagra reply=true subj=line side=RIGHT cid=708043072
```c
        else if (server.repl_diskless_load == REPL_DISKLESS_LOAD_SWAPDB && !moduleAllModulesHandleReplAsyncLoad()) {
            serverLog(LL_WARNING,
                "Skipping diskless-load because there are modules that are not aware of async replication.");
            enabled = 0;
        }
```
turning off `enabled` in `useDisklessLoad` means it'll be disk-based.

## [92] src/replication.c:1767 author=eduardobr reply=true subj=line side=RIGHT cid=708215386
Ok, so it's about the modules. That sounds a bit hard to solve.
Falling to disk-based load creates a requirement on disk space that was not there before, I see.

Is your suggestion to do a non-async swapdb, and fire the old events for backwards compatibility, or actually bring the whole mechanism of backups back?

## [94] src/replication.c:1767 author=oranagra reply=true subj=line side=RIGHT cid=708332098
i don't want to bring back the old backup/restore code (too complicated to maintain both).

I was thinking that maybe in this mode (no traffic during loading), we can support the old module API without bringing back the old backups mechanism, and maybe the modules will be ok with it.
i.e. if the modules don't do RM_OpenKey or any other odd things during loading, they won't be able the tell the difference between the old backup/restore/discard approach, and the new temp/apply/discard approach?

@MeirShpilraien you can probably tell me i'm wrong.

## [95] src/replication.c:1767 author=oranagra reply=true subj=line side=RIGHT cid=708333325
regarding the fact it falls back to disk-based, which it didn't before, i don't think that's a major issue.
i don't think swapdb is very commonly used anyway, and so are modules, so their combination is not a high concern.

## [44] src/replication.c:1796 author=oranagra reply=false subj=line side=RIGHT cid=698019132
i think that maybe we better fire the event before discarding the db.
maybe modules will find that more useful.
i.e. they're gonna get a bunch of `free` callbacks and they'll wanna know if these callback are for actual data or temp data.
@MeirShpilraien do you think we need to have a post discard event too?

## [46] src/replication.c:1796 author=MeirShpilraien reply=true subj=line side=RIGHT cid=705205344
Yes, I believe pre and post could be useful.

## [50] src/replication.c:1796 author=eduardobr reply=true subj=line side=RIGHT cid=706833966
Should it then be:
1. REDISMODULE_SUBEVENT_REPL_ASYNC_ABORTING
2. REDISMODULE_SUBEVENT_REPL_ASYNC_ABORTED
?

## [53] src/replication.c:1796 author=oranagra reply=true subj=line side=RIGHT cid=707285843
i'm ok with what you suggested, but maybe a better alternative could be something like:
1. REDISMODULE_SUBEVENT_REPL_ASYNC_DISCARD_START
2. REDISMODULE_SUBEVENT_REPL_ASYNC_DISCARD_DONE

(or ABORT_START / ABORT_DONE).
the advantage is that it's a common prefix for a start and end of the same operation (rather than two similar words with ING and ED suffix)

## [54] src/replication.c:1796 author=oranagra reply=true subj=line side=RIGHT cid=707291761
ohh, i just realized the discard is (always) async. so the module can't use these START / DONE notifications to know know if a free callback is part of the discarded database or the other one...
i suppose that can be a real issue for some modules, and they'll have no choice but avoid declaring this capability.
for the other ones, i suppose firing the event before we start discarding is slightly better.
@MeirShpilraien what do you think?

## [58] src/replication.c:1796 author=MeirShpilraien reply=true subj=line side=RIGHT cid=707310559
@oranagra agree, do not see other choose ...

## [60] src/replication.c:1796 author=eduardobr reply=true subj=line side=RIGHT cid=707579653
@oranagra @MeirShpilraien, maybe a small confusion, but what we have added is REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_ABORTED, for the case we started loading on tempDb then discarded due to failure.
There's also the discard that happens after successful load. In this case we fire a REDISMODULE_SUBEVENT_REPL_ASYNC_LOAD_COMPLETED

Should the ABORT really become DISCARD (from your comment here) and be fired only on the failure scenario?

## [72] src/replication.c:1796 author=oranagra reply=true subj=line side=RIGHT cid=707613641
@eduardobr please disregard our recent comments about the before and after.
i.e. all the ABORTING / ABORTED and DISCARD_START / DISCARD_DONE.
they where all about a way for the module to be able to distinguish between `free` callbacks of the main db, and the temp db, but since the freeing is always done in the background, the before and after notifications are no good.

the only think that's left to change IMHO is that we prefer that the ABORTED and COMPLETED events be fired before the freeing starts rather than after it.

## [81] src/replication.c:1816 author=oranagra reply=false subj=line side=RIGHT cid=707646689
i don't think that's right (to put that `if`).
you mean that if `asyncLoading` is true, it means we're still connected to the same master, and thus there's no need to discard the replication backlog and disconnect replicas?
i don't think that's right..
if we are forced to do a full sync (even if that's the same master), we have no way to know what changed, and we can't afford future partial syncs.

anything i'm missing?

## [52] src/replication.c:1820 author=ShooterIT reply=false subj=line side=RIGHT cid=706960428
I think your change for #9398 is right, since, before `swap db`, all data is not changed. Maybe you could remove the same comments and add some new comments for explaining current `swapdb` mode special behaviors.

BTW, for redis multi line annotation, the format is
```
/*
 *
 */
instead of
/*
*
*/

'*' should be aligned 
```


## [66] src/replication.c:1820 author=oranagra reply=true subj=line side=RIGHT cid=707597482
@eduardobr i agree with your earlier suggestion to extract this code (that's now exists in two different places) to a function.
i think a proper name can be:
```c
/* Called on a replica when it gets attached to a new master */
void replicationAttachToNewMaster() {
```

## [76] src/replication.c:1820 author=eduardobr reply=true subj=line side=RIGHT cid=707633508
Done, extra change here is that this routine is now only executed for swapdb in case it's not asyncLoading.

## [45] src/replication.c:1831 author=oranagra reply=false subj=line side=RIGHT cid=698019351
maybe we better call this event before the call for `swapMainDbWithTempDb`.
@MeirShpilraien WDYT?

## [47] src/replication.c:1831 author=MeirShpilraien reply=true subj=line side=RIGHT cid=705207943
I believe yes when the main DB (which is now the temp DB) will be deleted, a module will get the free function called, right? I believe a module would like to know that those free happen on the main DB after it was swapped.

## [48] src/replication.c:1831 author=MeirShpilraien reply=true subj=line side=RIGHT cid=705208341
And maybe also a done notifcation? Or do we get any other notification that will indicate done?

## [51] src/replication.c:1831 author=eduardobr reply=true subj=line side=RIGHT cid=706833986
Should it then be:
1. REDISMODULE_SUBEVENT_REPL_ASYNC_SWAPPING_DB
2. REDISMODULE_SUBEVENT_REPL_ASYNC_COMPLETED
?

## [55] src/replication.c:1831 author=oranagra reply=true subj=line side=RIGHT cid=707293000
same as the other discussion: https://github.com/redis/redis/pull/9323#discussion_r698019132
there's probably no value in both START and END events, and if we have just one, better have it before the action IMHO.

## [185] src/replication.c:2018 author=oranagra reply=false subj=line side=RIGHT cid=744605630
@MeirShpilraien noticed that the failure here can happen after we already announced ASYNC_LOAD_COMPLETED, swapped the databases and discarded the backup.
@eduardobr can you please look into it and issue a fix?

## [135] src/scripting.c:820 author=oranagra reply=false subj=line side=RIGHT cid=721166601
```suggestion
        server.lua_caller->id != CLIENT_ID_AOF &&  /* Don't care about mem if loading from AOF. */
```

## [68] src/server.c:4940 author=oranagra reply=false subj=line side=RIGHT cid=707600625
as you suggested, unlike the code inside redis, which will always set the two flags, i agree that the `loading` flag in INFO should be off while doing an async loading.
so you'll need this change:
```suggestion
            (int)(server.loading && !server.async_loading),
            (int)server.async_loading,
```

## [69] src/server.c:4940 author=oranagra reply=true subj=line side=RIGHT cid=707602428
you'll also need to modify `processCommand`:
```c
    if (server.loading && !server.async_loading && is_denyloading_command) {
```

## [186] src/server.c:6451 author=enjoy-binbin reply=false subj=line side=RIGHT cid=753635512
somehow i think it should not be added `!server.async_loading` here? (a bit confused?)
btw,  i think `async_loading` should also require a document, maybe missing,  i added it in https://github.com/redis/redis-doc/pull/1686

## [187] src/server.c:6451 author=oranagra reply=true subj=line side=RIGHT cid=753655567
It is indeed a bit confusing that in Redis both flags are set in this mode, but in info, it's either one or the other. 
But we concluded that for each one (users or developers) it makes sense to look at it this way. 

## [0] src/server.h:91 author=madolson reply=false subj=line side=RIGHT cid=683689833
Can we leave this in cluster.h and import it where we need it?

## [184] src/server.h:91 author=oranagra reply=true subj=line side=RIGHT cid=742627255
@madolson looks like for some reason you where looking at an outdated version, or maybe when you posted your approval, GH also posted an old comment you composed in the past.
either way, in the last version the definition of CLUSTER_SLOTS didn't move (it's still in cluster.h)

## [141] src/server.h:780 author=zuiderkwast reply=false subj=line side=RIGHT cid=725661403
If we make `clusterSlotsToKeysData` opaque (like I didn't in a code example posted earlier in another comment), we don't need to move these structs and CLUSTER_SLOTS to server.h. We only need a typedef and an incomplete struct.

## [90] src/server.h:785 author=zuiderkwast reply=false subj=line side=RIGHT cid=708100762
To keep the encapsulation of slot-to-key in cluster.c, we can make clusterSlotsToKeysData an opaque type. It means that it can only be accessed as a pointer. Functions in cluster.c need to be called to do anything with it.

Here in server.h, just declare it as a struct without declaring the fields.

```C
typedef struct clusterSlotsToKeysData clusterSlotsToKeysData; // <--- size of struct unknown here

typedef struct tempDb {
    redisDb *dbarray;
    clusterSlotsToKeysData *slots_to_keys; // <---- pointer
} tempDb;
```

The rest can be in cluster.c and cluster.h. (Actually encapsulation is not that great in Redis. We have too much stuff in the header files. Things like CLUSTER_SLOTS could actually be defined in cluster.c instead since it's only used there, but let's not solve that problem in this PR.)

```C
// cluster.h
#define CLUSTER_SLOTS 16384

void slotToKeySwapData(clusterSlotsToKeysData **slots_to_keys);

// cluster.c
typedef struct clusterSlotToKeys {
    uint64_t count;             /* Number of keys in the slot. */
    dictEntry *head;            /* The first key-value entry in the slot. */
} clusterSlotToKeys;

struct clusterSlotsToKeysData {
    clusterSlotToKeys by_slot[CLUSTER_SLOTS];
}

void slotToKeySwapData(clusterSlotsToKeysData **slots_to_keys) {
    clusterSlotsToKeysData *tmp = server.cluster.slots_to_keys;
    server.cluster.slots_to_keys = *slots_to_keys;
    *slots_to_keys = tmp;
}
```

I think `server.cluster.slots_to_keys` can also be a pointer to an allocated structure. It makes it possible to just swap the pointers without using memcpy.

Does this make sense?

## [102] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=709599816
Thanks a lot for the tips @zuiderkwast
Do you mind adding a commit with your suggestion? (Or maybe a separated PR focused on organizing these types after this is merged as this is probably happening soon?)

## [103] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=709608986
I can maybe make the opaque type preparations before this PR is merged. I prefer that code isn't moved back and forth too much in unstable.

## [106] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=710193408
Here I made clusterSlotToKeyMapping opaque. I also made tempDb opaque (implementation fully in db.c). It's based on your branch with one commit added. https://github.com/zuiderkwast/redis/tree/feature/draft-use-tempdb-swapdb/opaque-types

`./runtest-cluster` fails, but it fails also with your branch for me. Did you run it?

## [109] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=710449596
Oh, I always thought these were succeeded on PR build.
I just debugged and found a few things:
It crashes on this block of initTempDb():
```
    /* Init cluster slots to keys map if enable cluster. */
    if (server.cluster_enabled) {
        memset(&tempDb->slots_to_keys, 0, sizeof(tempDb->slots_to_keys)); // <- crashing line
    }
```
Removing it and, as it should be, adjusting the test to watch for async_loading instead of loading will make it pass.
And of course, the test needs to be renamed to fit the changes made.

Considering it's an area you have previous experience, any suggestion?
This block is inspired on your:
```
/* Empty the slots-keys map of Redis Cluster. */
void slotToKeyFlush(void) {
    memset(&server.cluster->slots_to_keys, 0,
           sizeof(server.cluster->slots_to_keys));
}
```
But I added to ensure a clean tempDb->slots_to_keys on the when we initialize it prior to loading data on it.

## [110] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=710494856
The slot-to-key data is supposed to match what's in server.db[0]. If the db is cleared the slot-to-key data should also be cleared. There is nothing complicated about it.

If you fixed it, then I guess you can add my commit (cherry-pick) and fix it the same way?

## [111] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=710497773
A bit of confusion here, I haven't fixed ;)
I mentioned it works when the line is removed, but not necessarily that's the correct to do.
I don't understand why the current code to init this tempDb->slots_to_keys simply fails.
It's something on other side of the application that blows up when I run this memset.

## [112] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=710504944
I don't know either. I'm not familiar with async load. :)

## [114] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=710540326
@oranagra @zuiderkwast 
See last commit "Fix slots to keys handling on swapdb"
Correct me if I'm wrong but I don't think we actually needed to store it in the tempDb.
Previously it was flushed during backup, now I flush right before swapDb.
It the replication fails, it's still intact.
The cluster test also won't crash with this change anymore (crash unrelated to async_loading fix).

## [115] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=710788812
Wait a second.

When we do async load, do we load the dump into tempDb and then swap? Is this how it works? Doesn't it mean that we need one slot-to-key mapping for the tempDB and another one for the main DB?

We can only clear the slot-to-key mapping when we clear the database. If we swap with a non-empty database, we need a matching slot-to-key mapping. If we clear the slot-to-key mapping when the database is not empty, it is easy to create a test case which crashes: Delete all keys. There is an assert in slotToKeyDelEntry which will fail.

## [116] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=710827011
Having a bad feeling about the complexity of this whole change now =)
So while doing the replication, the calls to slotToKeyAddEntry should apply to a tempDb slots_to_keys, not to server.cluster->slots_to_keys as it has been in this PR so far? If that's the case, we need to send a pointer to the methods that change server.cluster->slots_to_keys to work on either the temp or server.cluster according to the situation.
Makes sense @oranagra?

## [117] src/server.h:785 author=oranagra reply=true subj=line side=RIGHT cid=710998129
yes. sorry, i didn't closely follow the discussion about slots_to_keys.
as long as we keep serving from server.db we need the server.cluster->slots_to_keys to remain the old one.
during async loading, we also need dbAdd to update the new temporary slots_to_keys.
then if we discard the temp db, we discard the temp slots_to_keys, and if we swap the db to be the primary db, we need to copy the tmp slots_to_keys over the primary one.

## [118] src/server.h:785 author=oranagra reply=true subj=line side=RIGHT cid=711002743
the last committed changes don't look correct yet.

p.s. i don't mind the fact that CLUSTER_SLOTS is moved to server.h, and maybe slots_to_keys being moved to be part of the database.
it should only be allocated when cluster is enabled, but i think trying to isolate that part in cluster.c/h and make redis unaware of how many hash slots there are is not worth it.
if @zuiderkwast has another idea i don't mind trying (didn't follow that part of the discussion).

p.p.s the cluster tests are indeed not executed on the PR CI (they're too slow), and also the only existing test for that may need some improvement to properly catch errors around this change, please look into that.

## [119] src/server.h:785 author=eduardobr reply=true subj=line side=RIGHT cid=711229394
Now hopefully that's what we need, please check again.
And thanks for the patience.

## [122] src/server.h:785 author=zuiderkwast reply=true subj=line side=RIGHT cid=711315541
The commit I made above makes slot-to-key mapping an opaque type. The isolation is good IMO. It's better in cluster.c because the slot hashing and everything else for cluster is there and does not need to be visible outside it.

It's easy to extend those functions to pass a SlotToKeyMapping* to slotToKeyAddEntry() and the other functions, as you said @eduardobr. Let's do that! :) That's all we need AFACT.

## [123] src/server.h:785 author=oranagra reply=true subj=line side=RIGHT cid=711726175
@zuiderkwast as i said, i don't mind to expose this part of the cluster code in server.h (it's not as complicated as the cluster bus gossip or anything like that), it it's just a trivial listing of keys per slot,
i also don't have an objection to the opaque mechanism you created, which seems nice.
but the problem is still that unlike the handling of `dbArray` argument to `rdbLoadRio` and `db` argument to `dbAddRDBLoad`, which is able to make sure we add data into the temp db, while commands are serving from another (the one in the `server` struct), this mechanism isn't handling that yet. right?

I think that in theory, an opaque `slots_to_keys` pointer should be stored inside the redisDb struct, and if we do that, it solved the above problem (since the `db` pointer is passed around).
till now the cluster code got away from that problem because unlike non-clustered redis, redis-cluster only supports one database. but now, we actually have two.

p.s. we do have some future plan we have, to drop support for Sentinel, by allowing non-sharded clusters with multiple databases and voting replicas. in that case cluster will support multiple databases, but will actually not need the slot-to-key mapping.

## [139] src/server.h:794 author=oranagra reply=false subj=line side=RIGHT cid=725637355
i think the indentation changes you added are unwanted, it causes the diff to look as if you changed all these lines, when in fact you just added one (harder to review and also trace back history of things in git log).
considering that anyway, one line has a unique indentation to it, i think we can revert the others.

maybe the middle ground is to move the `expires_cursor` line to be one before the last, and then the first bulk of lines will have the old (short) indentation, and the last two will have the same long one?

or, i'm also ok with leaving all the existing lines as is.
i.e. the short ones have extra indentation, and the two long ones have their own way...

## [140] src/server.h:794 author=eduardobr reply=true subj=line side=RIGHT cid=725642377
Amended last commit reverting this indentation and consolidating the 2 slotToKeyTempDbFlush method

## [178] tests/integration/replication.tcl:401 author=oranagra reply=false subj=line side=RIGHT cid=741478239
i'm not sure why we needed that, since we did have this:
```
                    # Speed up shutdown
                    $master config set rdb-key-save-delay 0
```
we had it only in the aborted path, but that's also the only one that set it to anything other than 0 in the first place.
can you share some time measurements of before and after?
in any case, these lines (which i just quoted) are now no longer needed.

one more thing, i didn't bother to verify that the new numbers (delay and number of keys) you use are safe to avoid failures due to race conditions and slowness.
please make sure they're causing the replication to be long enough so that we're sure we'll be able to interrupt it in the middle (i.e some 5 or 10 seconds should be ok)

## [179] tests/integration/replication.tcl:401 author=eduardobr reply=true subj=line side=RIGHT cid=741482601
The new numbers make replication longer. Master has twice the keys (from 500 to 1000).
The only impact I can see from the number of keys in replica is rdb flushing time to disk (only on valgrind test or when I use absurd amount of keys in my machine).

About not needing:
`$master config set save ""`

Aren't the 1000 keys in master going to be flushed to disk when we terminate master server after Aborted path?

Edit: tbh, even in real life I see the server being blocked and slow to terminate when RDB is enabled (delays are after "Saving the final RDB snapshot before exiting.") so rdb-key-save-delay 0 doesn't save us from this, especially on valgrind test where delays are more noticeable

## [180] tests/integration/replication.tcl:401 author=eduardobr reply=true subj=line side=RIGHT cid=741490368
Can you run that valgrind again so we can see if the "Waiting for process nnn to exit" disappeared from all these tests after the `set save ""` was added?

> can you share some time measurements of before and after?

Changing the replica keys to 20_000_000 changed total running time of runtest-moduleapi from 10s to 30s here, and started showing `Waiting for process n to exit` about 5 times.
But with these same 20 million keys and `set save ""` it's back to the 10 to 11s and there's no `Waiting for process`

Same applies to master, achieved `Waiting for process` by increasing to 100000 keys of 10000, but I get fast shutdown in this config when `set save ""`


## [181] tests/integration/replication.tcl:401 author=oranagra reply=true subj=line side=RIGHT cid=741497215
i meant that we no longer need this, didn't suggest to remove `config set save ""`
```tcl
                    # Speed up shutdown
                    $master config set rdb-key-save-delay 0
```

i've re-triggered the tests (same links).

ideally we should tune it so that the "successful" test takes about a second or two,
and the "aborted" test sets delays that would take at least 5 (or better yet 20) seconds, but since we abort it rather early, it also takes a second or two.

## [182] tests/integration/replication.tcl:401 author=eduardobr reply=true subj=line side=RIGHT cid=741502877
Successful replication here takes about 1.1s.
Aborted (if not interrupted) about 11s, and interrupted as it is 1.2s

## [183] tests/integration/replication.tcl:401 author=oranagra reply=true subj=line side=RIGHT cid=741509349
great! that's what we wanted.
thank you!

## [162] tests/integration/replication.tcl:410 author=oranagra reply=false subj=line side=RIGHT cid=738031943
making the test faster (2.5 seconds instead of 8), risks timing issues (especially in valgrind runs).
we can't afford to use the same test for both reading from the replica while it's loading and also checking what happens when it succeeds.
we must split it to two tests (or two iterations of the same test), one with a delay and one without.

## [169] tests/integration/replication.tcl:410 author=eduardobr reply=true subj=line side=RIGHT cid=738807301
Replying here for all your test comments:
- I was definitely underestimating the possible timing issues ;)
- Made another version following your idea to always achieve fast executions and makes total sense
- Followed the other suggestions in general
- Asserts now printed as individual test descriptions

I know it will be tempting to say we can make variations for loading and async_loading on same test instead of splitting like I did, but I felt it would get quite bloated and hard to read. Still, in this version we cover more things than before with just 2 main tests in replication.tcl. Test time always under 1s each here.

## [6] tests/integration/replication.tcl:419 author=oranagra reply=false subj=line side=RIGHT cid=685426895
i mentioned in one of my first comments that internally, `server.loading` should be also set when async_loading is active, i'm not sure yet if we wanna apply that on the INFO loading flag too.
i.e. if we do, then existing code that looks at that flag will notice redis is loading (even though commands don't fail with `-LOADING`).

## [10] tests/integration/replication.tcl:419 author=eduardobr reply=true subj=line side=RIGHT cid=685472943
That's what I was afraid. Could be that some tool (for example a load balancer) is checking for the "loading" from INFO instead of sending another kind of command.
But at the same time, it's not a breaking change to show INFO loading: 1 without responding commands with `-LOADING`. Won't change behavior for those that simply upgrade redis, it's just that it will require tweaking to take advantage of this implementation IF they rely on INFO. So wouldn’t this option be on the table?

I'd be glad to change it to not have the INFO async_loading as well, but we have plenty of INFO loading_* properties during loading that can be useful, even to monitor the state of the server during this period of degraded performance or in case something goes wrong.
This information without any loading or async_loading to tell that these status properties are present there sounds inconsistent.

Personally I’d like to see either one or both in INFO during async_loading, but my familiarity on its usage out there is limited and in practice, for our individual use cases it’s not relevant because we use PING for probes. Just thinking more on the general public of course.

## [163] tests/integration/replication.tcl:426 author=oranagra reply=false subj=line side=LEFT cid=738033393
why did you remove this?
i think the replica can reconnect before we realize it stopped loading and then we'll end up waiting for it to finish

## [70] tests/integration/replication.tcl:438 author=oranagra reply=false subj=line side=RIGHT cid=707606945
this wait is now ineffective.
we need another way to check.
maybe look at `master_sync_in_progress` or set `replicaof no one` and look at `role`?

## [85] tests/integration/replication.tcl:438 author=eduardobr reply=true subj=line side=RIGHT cid=707679631
What happens?
Because this test still runs under and test swapdb on sync loading mode

## [89] tests/integration/replication.tcl:438 author=oranagra reply=true subj=line side=RIGHT cid=708055981
ohh, right, no async loading because replid changed.
i saw you removed this code, and concluded it's an async loading:
```tcl
            # waiting slave to do flushdb (key count drop)
            wait_for_condition 50 100 {
                2000 != [scan [regexp -inline {keys\=([\d]*)} [$slave info keyspace]] keys=%d]
```

so a recap, we now have:
1. either disk-based or disk-less that's not `swapdb`, in which case the loading flag is set, and INFO KEYSPACE will be emptied.
2. diskless swapdb when replid changed or modules with issues are present, in which case loading flag is set, and keyspace is NOT emptied.
3. diskless swapdb when replid didn't change and no modules with issues, in which case loading flat is off, and keyspace is NOT emptied.

i.e. before this change we had case 1 (which was also the case when swapdb was used), and no such distinction between the loading flag and the keyspace.

bottom line, the change you made in the test was only needed for the keyspace, not the loading flag. so the test is fine.

## [71] tests/integration/replication.tcl:451 author=oranagra reply=false subj=line side=RIGHT cid=707611364
where there any changes in the new tests since my last review (before you rebased)?
i rather review the diff and not re-read the whole thing, but i can't find the old commit in GH.

## [73] tests/integration/replication.tcl:451 author=eduardobr reply=true subj=line side=RIGHT cid=707628286
No changes in the tests. The big rewrite is just a rebase + integrating with the 2 conflicting PRs

## [170] tests/integration/replication.tcl:458 author=oranagra reply=false subj=line side=RIGHT cid=739791128
sorry to bother you again, but maybe we don't need this "delayed" test? i.e. these assertions can be added into the "aborted" one before we abort it.
so this means we only have two tests "aborted" and "succeeds".

also, let's rename "fast" since the speed is just an internal concern of the test (not to take forever), but what it tests is what happens when it succeeds and fails replication..

## [164] tests/integration/replication.tcl:467 author=oranagra reply=false subj=line side=RIGHT cid=738035299
again, for a successful test, i don't think we can accord to attempt to make it slow but not too slow.
the test will be unstable and fail from time to time.
i think we wanna run the same code twice, once with a delay and once without, and in each run test a different aspect.

## [165] tests/integration/replication.tcl:528 author=oranagra reply=false subj=line side=RIGHT cid=738036585
in this case we don't need to do any funny things, so there's no real need to look at log lines.
we can just `wait_for_condition` on `master_link_status` to be `up`

## [171] tests/integration/replication.tcl:589 author=oranagra reply=false subj=line side=RIGHT cid=739791660
same here..
i.e. on one hand, we wanna test 3 things:
1. state during loading
2. after it is aborted
3. and when it succeeds

so it is nice to see 3 tests with 3 titles.
but on the other hand, we can combine the the first two and save some time.


## [174] tests/modules/testrdb.c:203 author=oranagra reply=false subj=line side=RIGHT cid=740848018
i meant to return the value of `before_str_temp`, so we don't just wait for the notification that async loading started, but also make sure the REDISMODULE_AUX_BEFORE_RDB was called.

## [176] tests/modules/testrdb.c:203 author=oranagra reply=true subj=line side=RIGHT cid=740849978
let's also put some comment above the function to mention it's a testing hack to control the timing of the test, and not something a valid module would do.

## [147] tests/unit/moduleapi/testrdb.tcl:133 author=oranagra reply=false subj=line side=RIGHT cid=730433722
i see you added `external:skip`, but maybe we should also add the `repl` tag like other tests (they kinda cover the same thing, but who knows what will someone try to skip...)

## [152] tests/unit/moduleapi/testrdb.tcl:133 author=eduardobr reply=true subj=line side=RIGHT cid=730448249
These 2 new tests are under `tags {repl} {`, isn't already tagged with that?

## [166] tests/unit/moduleapi/testrdb.tcl:161 author=oranagra reply=false subj=line side=RIGHT cid=738037379
in this case we don't need to do any funny things, so there's no real need to look at log lines.
we can just `wait_for_condition` on `master_link_status` to be `up`

## [148] tests/unit/moduleapi/testrdb.tcl:174 author=oranagra reply=false subj=line side=RIGHT cid=730434100
this will need to be adjusted once #9323 is merged (not sure which one will be merged first)
@ShooterIT FYI

## [159] tests/unit/moduleapi/testrdb.tcl:174 author=eduardobr reply=true subj=line side=RIGHT cid=736493978
Same thing as https://github.com/redis/redis/pull/9323/commits/fb7b46cbb175b46d50c1aac76e328bdaca5e3d94?

## [160] tests/unit/moduleapi/testrdb.tcl:174 author=oranagra reply=true subj=line side=RIGHT cid=736540978
ohh, sorry, i meant to mention #9166 (not 9323), but yes. i see you copied the modified code to trigger exhaustion of the backlog.
resolving this comment.. 

## [167] tests/unit/moduleapi/testrdb.tcl:186 author=oranagra reply=false subj=line side=RIGHT cid=738039085
i think there's a good chance that this line will miss the train.
we didn't set any `repl-diskless-sync-delay`, and there's a chance the replica will re-connect and start bgsave before this config is applied.
maybe we can just move it to before we disconnect the replica, or we need `repl-diskless-sync-delay` to delay the full sync a bit.

## [175] tests/unit/moduleapi/testrdb.tcl:191 author=oranagra reply=false subj=line side=RIGHT cid=740849329
let's also update the comment so state that we wanna abort only after the temp db was populated by REDISMODULE_AUX_BEFORE_RDB 

## [168] tests/unit/moduleapi/testrdb.tcl:201 author=oranagra reply=false subj=line side=RIGHT cid=738040628
this line doesn't affect a fork that's already in progress (the fork child doesn't see this config change).
in the test that checks for completion, we should just avoid setting this config in the first place.
again, i think we need 3 tests.
1. checks the status during sync (needs a delay)
2. checks that status after failure (maybe it can be in the same run as the one above.
3. check for successful run (without any delays)

## [149] tests/unit/moduleapi/testrdb.tcl:203 author=oranagra reply=false subj=line side=RIGHT cid=730434352
this can take a long time (5-10 seconds). maybe we rather rely on another test (one with no delay) rather than let this one complete loading.

## [161] tests/unit/moduleapi/testrdb.tcl:203 author=oranagra reply=true subj=line side=RIGHT cid=736896849
@eduardobr do you wanna try to handle this comment (and the one below)?
do you understand my suggestion?
a for loop running the same code 3 times with small variations,
one uses delay so we can test the state during loading.
one that succeeds (which doesn't have the delay so it's fast),
and one that gets aborted and we can check recovery. 

## [150] tests/unit/moduleapi/testrdb.tcl:217 author=oranagra reply=false subj=line side=RIGHT cid=730434656
these two (or maybe soon to be 3) tests have a lot on common.
maybe we can run them in a `foreach` loop that executes the same code 3 times is a few `if`s to do the variations between them?
will also make it easier to realize what's different and make future adjustments.

## [172] tests/unit/moduleapi/testrdb.tcl:223 author=oranagra reply=false subj=line side=RIGHT cid=739792540
same here... we can run that same check (the value of `testrdb.get.before`), both before we abort and after.

one more interesting complication, maybe we can find a way to make sure we abort only after the module got the callback and loaded a new value into it's temp variable?
i.e. without that check, there's a race, and maybe in some cases we abort even before...
maybe we'll add a command like `testrdb.async_loading.get.before` and do a `wait_for_condition` on it?
i know it means that module is no longer a naive module that uses the new api to get things done, but it does improve the test...

WDYT?

## [173] tests/unit/moduleapi/testrdb.tcl:223 author=eduardobr reply=true subj=line side=RIGHT cid=740486983
Good point about the race. Suggestions applied.
Thanks
