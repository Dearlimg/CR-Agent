# digest redis-9788 : 272 anchored candidates (of 357 total)

## [352] .gitignore:19 author=yangbodong22011 reply=false subj=line side=RIGHT cid=815555837
should be `appendonly.aof*` instead of `appendonly.aof.*`  
now, `appendonly.aof` will not be ignored.

## [353] .gitignore:19 author=chenyang8094 reply=true subj=line side=RIGHT cid=815592564
Maybe you're right, but the current implementation doesn't create a file named `appendonly.aof` unless you manually copy an old-style AOF for upgrade testing. So I think we just need to ignore the file we're going to create. @oranagra WDYT? 



## [354] .gitignore:19 author=oranagra reply=true subj=line side=RIGHT cid=815608833
i think it's a good idea to ignore both.
i.e. both the ones we create, and the ones we created in the past, that are still left in the folder.

## [355] .gitignore:19 author=yangbodong22011 reply=true subj=line side=RIGHT cid=815617083
Only developers pay attention to `.gitignore`, and when I updated the `unstable brance` code, `appendonly.aof` caught my attention, so I started this discussion.

## [356] .gitignore:19 author=chenyang8094 reply=true subj=line side=RIGHT cid=815625209
@oranagra @yangbodong22011  Sounds reasonable, I'll make a PR to add it, thanks.

## [0] redis.conf:1312 author=oranagra reply=false subj=line side=RIGHT cid=750395364
```suggestion
# The base name of the append only file
```

## [1] redis.conf:1319 author=oranagra reply=false subj=line side=RIGHT cid=750401799
I don't think we must document the history here (unless there are backwards compatibility concerns that should be in the foot notes)
```suggestion
# Redis can persist the data to a set of append-only files, they are divided
# into two types, one is the the base type, it represents an initial (RDB
# serialized) snapshot of the data present when the AOF is rewritten. The others
# are incremental type, they contain the incremental commands since the last AOF
# rewrite.
```

## [3] redis.conf:1321 author=oranagra reply=false subj=line side=RIGHT cid=750412827
let's stick to one terminology, either "meta" or "manifest"
```suggestion
# In order to manage these AOF files, Redis uses a manifest file to track them.
```

## [2] redis.conf:1329 author=oranagra reply=false subj=line side=RIGHT cid=750404230
being a base file name, i wanna see just name here, and i wanna add the suffix (file type) at runtime.
i don't mind trimming the `.aof` suffix we get from the user (in case he didn't update his config file), and i also don't mind keeping it and adding another suffix after it.
but for new users / deployments, i want to have something that's cleaner, so i suggest:
```suggestion
appendfilename "appendonly"
```

## [10] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=750782283
If the user configures the name as `appendonly.xxxxx` or `appendonly.xxx.yyy` (not the default `appendonly.aof`), should we also trim it to appendonly?

## [15] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=750901819
i don't think so. as i said, i'm also willing to keep this suffix, but since it's a common config i figured we can make a minimal effort to clean it. i.e. if the string has at least 5 chars, and ends with that 4 chars suffix, trim the last 4 chars.

but now it occurs to me that maybe if some piece of external software will try to take the value of that config and search for the files based on this base name, this trimming can cause some confusion.
on the other hand, it is likely that this "server management software" is coupled with the deployment scripts and the server configuration, so i don't think it's a real issue.

let's seek additional feedback. @yossigo @soloestoy ?

## [112] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=756632679
@oranagra  Do we have any conclusions about this? 

Can we directly use the original name (after all, it is configured by the user or default),  and then we use `base` or `incr` to indicate the type of this file,  such as `appendonly.aof.1.base`, `appendonly.aof.1.incr`, `appendonly.aof.2.incr`。

The current solution  does not seem to be very good: `appendonly.aof.2.rdb`, `appendonly.aof.3.aof`, it is difficult to understand that a name contains both `rdb` and `aof`. WDYT ?

## [117] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=756827028
for now, let's keep the simple approach you have (just use it as base, and possibly generate odd file names).
we can focus on other aspects of this PR and leave that one for last.
then i guess we need to map a few cases and what would be the outcome of each to decide.
i.e. users who upgrade from an old system who use the default config, vs users who upgrade from an old system who have explicitly overwritten it, and how will redis behave on upgrade (should load the old data in either one of the cases)

## [124] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=756884617
i see you changed the suffix (saw your last commit), i meant to just use what ever is in the config as base file name and apply the suffixes on it (i.e. that there's no need for now to strip the ".aof" suffix that the user may have provided).
i.e that i'm ok with "appendonly.aof.0.rdb"
maybe i don't mind the base/incr suffix, but i think it is redundant (it's also indicated by the meta file).
what the suffix came to denote in my "design" is the format inside the file (i.e. considering we'll deprecate the possibility for AOFRW to generate AOF command content, the base file is an RDB format).

## [125] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=757163072
Well, I understand what you mean. One reason for changing `.rdb` to `.base` is because in the current pr, the BASE file may be in AOF or RDB format (depending on the `server.aof_use_rdb_preamble` is yes or no), if BASE is in AOF format but we still end with `.rdb` may be confusing (the user will try this features of this pr when merged, but the next pr hasn't merged yet ). Note that what I said is only in this pr. If we remove `server.aof_use_rdb_preamble` and `rewriteAppendOnlyFileRio` in the next pr, we can say that our BASE is rdb and INCR is aof. So  `appendonly.aof.1.rdb` and  `appendonly.aof.2.aof`  will be certain (reflecting their internal format).

This may be my concern. After all our next PR (remove `server.aof_use_rdb_preamble` and `rewriteAppendOnlyFileRio`) should be merged soon (The workload is not much, but we have to think clearly about the impact it brings). So if we don’t mind this short-term confusing, I am willing to roll back the suffix to the previous version.

@oranagra WDYT？


## [126] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=757320913
Theoretically, if we want this PR to be really clean on it's own (and not count on the next change), maybe we can either add a `.aof` or `.rdb` suffix for the rewrite depending on `server.aof_use_rdb_preamble` (to indicate the format).
and if additionally we also want some notation if it's a base or incr, we can keep these too.
then maybe in the next pr we can remove this complication.

on the other hand, i don't mind letting it be one way or the other in the short term, and i'm quite sure we're gonna trim the AOF generation code very soon, i'm just worried that we won't forget it and keep only the "base" notation and no indication on the format. so maybe it's better to just add both (not a big overhead in coding)

## [127] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=757332106
Currently `base` and `incr` use separate seq, they can be incremented separately. If `base` also uses the aof suffix(`server.aof_use_rdb_preamble` is no), there may be a file name conflict between `base` and `incr`. If `base` and `incr` share a seq, then seq is not continuously increasing. @oranagra  Which do you think is better?



## [128] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=757337039
i guess we can invest a few extra lines of code (which we may clean in the future), and add both "base" / "incr" notation in the file name, as well as ".aof" or ".rdb" to denote the format.
shouldn't be too complicated, and we may consider changing that in a few weeks.

## [129] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=757364347
@oranagra WDYT about this?

```
appendonly.aof_1.base.rdb
appendonly.aof_1.base.aof

appendonly.aof_1.incr.aof
appendonly.aof_2.incr.aof
```

In addition, do we necessary to reflect its internal encoding format on the file name (or whether the user really cares about this). we divided the AOF into one BASE file and multiple INCR files (That is Multi Part). We add the `.rdb` suffix just to tell the user that the BASE file is an RDB, but I think it can be explained in the document and release notes(That is, Multi Part = one BASE RDB + many INCR AOFs). This can simplify our naming rules： ` basename_seq.type`

for example:
```
appendonly.aof_1.base
appendonly.aof_1.incr
appendonly.aof_2.incr
appendonly.aof_3.incr
```
or

```
anyname_1.base
anyname_1.incr
anyname_2.incr
anyname_3.incr
```



## [130] redis.conf:1329 author=oranagra reply=true subj=line side=RIGHT cid=757881784
for some reason i prefer the suffix to denote the format, maybe i'm looking too much into it, but what complications does it add? is it more than some 4 lines of code?

## [131] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=757893037
I have modified the PR according to the rules mentioned above.

```
appendonly.aof_1.base.rdb
appendonly.aof_1.base.aof

appendonly.aof_1.incr.aof
appendonly.aof_2.incr.aof
```

## [135] redis.conf:1329 author=chenyang8094 reply=true subj=line side=RIGHT cid=758099526
@oranagra PLZ review this part again and see if there are any more problems? thanks.

I will remove `base` and `incr` suffix in the next PR (remove `server.aof_use_rdb_preamble` and `rewriteAppendOnlyFileRio` related code), so as to use the following rules:

```
appendonly.aof_1.rdb
appendonly.aof_1.aof
appendonly.aof_2.aof
```


## [4] redis.conf:1333 author=oranagra reply=false subj=line side=RIGHT cid=750421819
```suggestion
# These files mentioned above have a certain naming rule. The appendfilename
# config will be used as the base part of the file name, which will be added
# suffixes to denote role and sequence number.
# 
# For example: 
# The base could be: appendonly.12.rdb.
# The incremental ones: appendonly.25.aof, appendonly.26.aof.
# META FILE: appendonly.manifest
```

btw, it looks like your editor is inserting some non-ASCII characters, e.g. `、` instead of `,` and ` `

## [11] redis.conf:1333 author=chenyang8094 reply=true subj=line side=RIGHT cid=750803078
I think you provide the name `appendonly.12.rdb, appendonly.25.aof, appendonly.26.aof` does look more intuitive, I decided to use your rules.

## [205] redis.conf:1336 author=oranagra reply=false subj=line side=RIGHT cid=775051173
you missed one 8-)
```suggestion
# into two types, one is the the base type, it represents an initial (RDB
```

## [223] redis.conf:1337 author=yoav-steinberg reply=false subj=line side=RIGHT cid=775230416
Try to avoid using the term "rewrite" before explaining it.
```suggestion
# or AOF format) snapshot of the data present when the AOF is initially created (or rewritten). The 
```

## [283] redis.conf:1337 author=chenyang8094 reply=true subj=line side=RIGHT cid=775751807
the AOF initialization don't create base AOF. 

## [295] redis.conf:1337 author=oranagra reply=true subj=line side=RIGHT cid=775799963
it will soon (see #9794).
but also, considering the state of this PR, isn't it better to tag the first AOF file we create as "base"?
i.e. when loading an aof manifest, the first file we load must always be a "base" file.
i.e. it would be wrong to start from an "incremental" file since it implies we're starting from the middle with no view of the history, so in that sense i think that on empty startup, we should change the code and tag the first file as "base"

## [299] redis.conf:1337 author=chenyang8094 reply=true subj=line side=RIGHT cid=775853586
Maybe we'll change that text in the next PR.

## [201] redis.conf:1339 author=oranagra reply=false subj=line side=RIGHT cid=774949981
I don't think the documentation should refer to "BASE" and "INCR" as terms or format, but rather use plain English ("base" and "incremental")
```suggestion
# into two types, one is the the base type, it represents an initial (RDB
# or AOF format) snapshot of the data present when the AOF is rewritten. The 
# others are incremental type, they contain the incremental commands since the last 
# AOF rewrite.
```

## [247] redis.conf:1339 author=yoav-steinberg reply=false subj=line side=RIGHT cid=775248408
```suggestion
# base AOF file was created.
```

## [281] redis.conf:1339 author=chenyang8094 reply=true subj=line side=RIGHT cid=775745694
i think they mean the same thing, right?

## [293] redis.conf:1339 author=oranagra reply=true subj=line side=RIGHT cid=775798234
i think base is better.. rewrite is just one way to create a base file (see plans in #9794 )

## [294] redis.conf:1339 author=chenyang8094 reply=true subj=line side=RIGHT cid=775799341
Can you explain in more detail "always generate base rdb (even when starting empty)"

## [296] redis.conf:1339 author=oranagra reply=true subj=line side=RIGHT cid=775832657
when rdb-preamble is enabled (or maybe even if it isn't), and redis starts up empty configured to persist to AOF, i want it to do an rdbSaveRio and generate an empty base file.

this may be required for some modules that want to persist their configuration into rdb aux fields, so that when we recover from persistence, they know with which config that persistence was created.

## [194] redis.conf:1356 author=oranagra reply=false subj=line side=RIGHT cid=774487221
commenting here, for a discussion about another block in the config that i can't comment on (no changes).

```
# When rewriting the AOF file, Redis is able to use an RDB preamble in the
# AOF file for faster rewrites and recoveries. When this option is turned
# on the rewritten AOF file is composed of two different stanzas:
#
#   [RDB file][AOF tail]
#
# When loading, Redis recognizes that the AOF file starts with the "REDIS"
# string and loads the prefixed RDB file, then continues loading the AOF
# tail.
aof-use-rdb-preamble yes
```

first, the term "preamble" is no longer applicable, since they're now in different files.
but obviously we can't rename the config.
what we can do, is update the documentation, which should state that this config determines if the REWRITE will generate an RDB format, or an AOF (RESP) format.

## [200] redis.conf:1356 author=chenyang8094 reply=true subj=line side=RIGHT cid=774905688
```
# The BASE AOF encode type.
# 
# When rewriting the AOF file, Redis will generate a new INCR file and a 
# BASE file. INCR file is always AOF (RESP) encoded , but BASE file can be 
# in AOF format and RDB format. When this option is turned on, the BASE file
# use RDB format (the file name uses .rdb as the suffix), otherwise use AOF 
# format (the file name uses .aof as the suffix).
# 
# When loading, Redis recognizes that the BASE AOF file starts with the "REDIS"
# string and loads the BASE RDB file, then continues loading the INCR AOF
# file.
aof-use-rdb-preamble yes
```

## [19] redis.conf:1442 author=oranagra reply=false subj=line side=RIGHT cid=750978292
```suggestion
# Automatic deletion of old AOF files can be disabled, in which case some other
# process will need to move them away.
# aof-enable-auto-gc yes
```

## [20] redis.conf:1442 author=oranagra reply=true subj=line side=RIGHT cid=750979190
maybe it will be better to negate this? and call it `aof-disable-auto-gc` (have it 0 by default)?

## [40] redis.conf:1442 author=oranagra reply=true subj=line side=RIGHT cid=751331192
besides the documentation / phrasing, i'd like to start a discussion as to why we need that feature at all?
what are the use cases for that?

## [48] redis.conf:1442 author=chenyang8094 reply=true subj=line side=RIGHT cid=751777540
At present, I only use `aof-enable-auto-gc` in the test to assert that expected history AOF will be generated in AOFRW.

However, I am wondering whether there is such a scenario: the user has a backup system that will continuously upload all incremental AOFs, because our AOFs have timestamp annotations, so we can even recovery the data to each previous point in time. Therefore, after each AOFRW, we cannot delete the history AOF immediately, unless the user has actually uploaded these AOFs . This is just a guess of mine. I don't mind that it is only used for testing purposes like `aof-child-rewrite-delay` and not exposed in `redis.conf`.


## [52] redis.conf:1442 author=oranagra reply=true subj=line side=RIGHT cid=752003270
ok, i have a feeling that such an external mechanism will require more assistance from redis.
at the very least it be able to trigger GC manually, or clean the manifest in some way (before/after deleting the files).

I guess we should leave this feature out of the scope for now until we design it properly.
so in that case, let's mark this flag as a testing one (comment), and remove the documentation from redis.conf.

i also think it's still a good idea to negate it.

p.s. we do plan to soon add some config.c flag on all test testing configs see https://github.com/redis/redis/issues/9684
i.e. it'll probably be the ones that are not documented in redis.conf.

## [242] redis.conf:1453 author=oranagra reply=false subj=line side=RIGHT cid=775239610
@yoav-steinberg i'm not sure which version did you review?
IIUC we eliminated the references to uppercase BASE and INCR in the docs.
maybe your phrasing comments are still applicable, but maybe you should dismiss them and make new suggestions base on the latest?

## [248] redis.conf:1453 author=yoav-steinberg reply=false subj=line side=RIGHT cid=775248527
```suggestion
# The base AOF encodeing type.
```

## [249] redis.conf:1455 author=yoav-steinberg reply=false subj=line side=RIGHT cid=775248667
```suggestion
# When creating the initial AOF file (or rewriting it), Redis will generate a new incremental file and a 
```

## [280] redis.conf:1455 author=chenyang8094 reply=true subj=line side=RIGHT cid=775745544
When we initialize (for example, when redis is started), we will open INCR AOF instead of creating a new BASE, so I think the original comments is fine.

## [292] redis.conf:1455 author=oranagra reply=true subj=line side=RIGHT cid=775797507
we're gonna change this soon (will always create a base file even in that case), see #9794 
but maybe we can find a way to say that without mentioning neither "initial", nor "rewrite" which is also not in line with the above mentioned plan.

I see this section is below all the text that defines the folder and base / incremental.
so maybe we can just refer to the "base"?

## [202] redis.conf:1462 author=oranagra reply=false subj=line side=RIGHT cid=774951964
There was too much detail here IMHO.
users and admins don't need to understand what RESP is (that's for client library developers). and don't care about the file format header bytes.
```suggestion
# The base AOF rewrite encodeing type.
# 
# When rewriting the AOF file, Redis will generate a new incremental file and a 
# new base file. The base file can be in AOF (commands) format or RDB
# (serialized) format. When this option is turned on, the base file uses RDB
# format, otherwise it uses AOF format.

```

## [326] src/aof.c:57 author=yossigo reply=false subj=line side=RIGHT cid=776828275
```suggestion
 * BASE: Every time AOFRW succeeds, a BASE file will be generated, which represents 
```

## [21] src/aof.c:59 author=oranagra reply=false subj=line side=RIGHT cid=751040730
```suggestion
 *       AOFs will become HISTORY. they will be cleaned regularly unless GC is disabled. 
```

## [22] src/aof.c:62 author=oranagra reply=false subj=line side=RIGHT cid=751042612
```suggestion
 * INCR: There may be more than one (during AOFRW, and after AOFRW failure), and 
 *       together they represent all the incremental commands executed by redis
 *        after the last successful AOFRW.
```

## [23] src/aof.c:78 author=oranagra reply=false subj=line side=RIGHT cid=751043835
```suggestion
#define MANIFEST_TEMP_NAME_PREFIX "temp_"      
```
is this just for the manifest? don't we have temp files elsewhere? what about the double-write files?
p.s. i think we can just use a hard coded `.tmp` suffix for all of these, not necessarily define it per use.

## [181] src/aof.c:81 author=chenyang8094 reply=true subj=line side=RIGHT cid=773741198
Because I want to make them more unified:
```
appendonly.aof_1.base.aof 
appendonly.aof_1.incr.aof 
appendonly.aof_manifest
```

## [182] src/aof.c:81 author=oranagra reply=true subj=line side=RIGHT cid=773761166
ok, in my eyes it's a table:
```
base-prefix      idx   role   format
appendonly.aof   1     base   aof 
appendonly.aof   1     incr   aof 
appendonly.aof                manifest
```
maybe if we change the `_` to `.`, or change the first `.` to `_` it'll make more sense?
i.e. either:
`appendonly.aof.1.base.aof`
or
`appendonly.aof_1_base.aof`

## [184] src/aof.c:81 author=chenyang8094 reply=true subj=line side=RIGHT cid=773799466
I don't like to have too many `.` ,  it can easily lead to misunderstandings because it contains the `.aof` `.1` `.base` `.aof` suffix

## [186] src/aof.c:81 author=chenyang8094 reply=true subj=line side=RIGHT cid=773803755
I think it is not a strong requirement to include the encoding format in file name. We can add the format field in the manifest, if anyone is interested in this. 

aof dir:
```
 appendonly.aof_1.base 
 appendonly.aof_1.incr  
 appendonly.aof_2.incr  
 appendonly.aof_manifest
```

the content of manifest:
```
file appendonly.aof_1.base seq 1 type b format rdb
file appendonly.aof_1.incr seq 1 type i format aof
file appendonly.aof_2.incr seq 2 type i format aof
```

## [189] src/aof.c:81 author=oranagra reply=true subj=line side=RIGHT cid=773852970
i don't see the problem with multiple `.` in the file name.. it's not uncommon, and it's a well established pattern to only look past the last `.` for the file format, so in that respect, in my eyes the manifest is the format and should use `.`
also, there are already two `.` in your other file patterns, so i'm not sure what's bothering you about it.
if the other `_` bother us, we can change it to `.` as well.

as for the format suffix, i don't think it should be part of the manifest, i think it should be in the file name, and what's bothering us is that we have two mentions of the "format" in the file name (`.aof` can appear twice)

## [190] src/aof.c:81 author=chenyang8094 reply=true subj=line side=RIGHT cid=773858136
I think it is necessary for us to vote for this rule. Let me summarize that we now have blow options (@oranagra Maybe you have other plans to add):

**Note**: we have concluded that the original `appendfilename` (default `appendonly.aof`) will be used as the `basename` of the new file name, and we **cannot do any trim**  operations on `appendfilename`, so we are discussing how to combine the suffix part of the new name.

1.  Use  `. ` to combine `basename` and `suffix`
```
 appendonly.aof.1.base.aof 
 appendonly.aof.1.incr.aof 
 appendonly.aof.2.incr.aof 
 appendonly.aof.manifest
```

2. Use  `_` to combine `basename` and `suffix`
```
 appendonly.aof_1.base.aof 
 appendonly.aof_1.incr.aof 
 appendonly.aof_2.incr.aof 
 appendonly.aof_manifest
```

3. Use  `_` to combine `basename` and `suffix` and remove format `suffix`
```
 appendonly.aof_1.base 
 appendonly.aof_1.incr
 appendonly.aof_2.incr
 appendonly.aof_manifest
```

4. Use  `_` to combine `basename` and `suffix`  and remove format `suffix`, and add format field  to manifest file.
```
 appendonly.aof_1.base 
 appendonly.aof_1.incr  
 appendonly.aof_2.incr  
 appendonly.aof_manifest
```
and the content of manifest (add format field ):
```
file appendonly.aof_1.base seq 1 type b format rdb
file appendonly.aof_1.incr seq 1 type i format aof
file appendonly.aof_2.incr seq 2 type i format aof
```

5. Use  `. ` to combine `basename` and `suffix`, remove format `suffix`, and remove one more  `.` to simplify naming
```
 appendonly.aof.base1
 appendonly.aof.incr1
 appendonly.aof.incr2
 appendonly.aof.manifest
```
and the content of manifest (add format field ):
```
file appendonly.aof.base1 seq 1 type b format rdb
file appendonly.aof.incr1 seq 1 type i format aof
file appendonly.aof.incr2 seq 2 type i format aof
```

@oranagra @yossigo @soloestoy @madolson Please vote which one of the above are more satisfactory to you, or if you have a better suggestion, thank you.

## [191] src/aof.c:81 author=oranagra reply=true subj=line side=RIGHT cid=773873218
i had a discussion about this (the annoying presence of ".aof" in file names derived from `appendfilename`) with @yoav-steinberg to try to see if we can come up with something.

one idea that came up is this:
1. when creating the manifest on upgrade, use `server.aof_filename`, but without any ".aof" suffix if present
2. when creating the base file entry for the upgrade in the manifest, do the same (remove the ".aof" suffix)
3. after when moving the existing AOF file into the folder, do the same.
4. after the upgrade is done (rename is completed), and on normal startup if there was no upgrade, we actually trim the ".aof" suffix from `server.aof_filename` (modify the variable)

so now, if someone does `CONFIG GET appendfilename` or `CONFIG REWRITE`, the value of the config no longer has the `.aof` suffix.

so if i had a concern that that someone will write a bash script doing
```
cp `config get appenddirname`/`config get appendfilename` <some destination>
```
this would still work.

notes:
* i don't particularly like this idea (yet)
* I must note that we have other configs for which the CONFIG GET value is mutated. e.g. `CONFIG SET maxmemory 1g` will translate to a different value in CONFIG GET.
* it would still look ugly for anyone looking at the redis.conf file that's shipped with redis (mentioning `appendonly.aof`, in case there are no config rewrites), but what we can do is delete that line from the default config, and only rely on the default value hard-coded in redis.
* we can update the comment in redis.conf to just mention `# appendfilename appendonly` (without the suffix)
* this means that the only way a user of a new deployment to observe that the hard coded default value is with `.aof` is by looking at the code, since at startup we change it right after handling the upgrade procedure, that default is not observable in any way.
* unlike the `maxmemory` config, this one is immutable, so you can't observe that you do CONFIG SET, and get a different value in CONFIG GET.

## [192] src/aof.c:81 author=chenyang8094 reply=true subj=line side=RIGHT cid=773895207
I don’t think it’s a good idea to modify the user’s configuration without authorization. `appendfilename` and `maxmemory` are different. If we trim `appendfilename`, we won’t be able to distinguish between these two configurations: `appendfilename appendonly `and `appendfilename appendonly.aof`.

## [193] src/aof.c:81 author=oranagra reply=true subj=line side=RIGHT cid=773897506
i agree..
i don't see any other good way to get rid of ".aof" we have in the middle of the file names we create.

I vote for 1

## [195] src/aof.c:81 author=yossigo reply=true subj=line side=RIGHT cid=774494703
I'm also in favor of 1.

I don't see the `.aof` as such a big problem as it only affects users who transition from an older version and don't bother updating the configuration file. It's important not to do something terribly wrong in this case, but having less-than-ideal file naming seems reasonable to me.

## [196] src/aof.c:81 author=yangbodong22011 reply=true subj=line side=RIGHT cid=774542121
I voted for 5, just from my personal aesthetic point of view.

## [197] src/aof.c:81 author=soloestoy reply=true subj=line side=RIGHT cid=774545399
First of all, I still don't like the method that using `appendfilename` as prefix, it makes the new files' name ugly and hard to understand. I prefer using file lock (i.e. a unified `LOCK` file in folder), if user start some redis instances in the same folder, just log and exit.

But if you insist on handling some strange Schrodinger's scenarios and want suffix, I vote for 2.

Because use `.` to combine may lead to a problem, if someone like me don't like the prefix, he or she may set the prefix `appendfilename` to empty, file start with `.` is invisible.

## [198] src/aof.c:81 author=oranagra reply=true subj=line side=RIGHT cid=774560029
i don't see why anyone would set the prefix to empty.
you'd better set it to just "appendonly", or "my_redis", or "redis-1".

anyway, i don't mind to use `_` in between the different parts of the template, but i think the last part should be the file format (obviously using `.`).

so maybe this could be a plan:
```
 appendonly.aof_1_base.aof 
 appendonly.aof_1_incr.aof 
 appendonly.aof_2_incr.aof 
 appendonly.aof_manifest.csv
```

note that the manifest is now a CSV instead of being space separated.
and maybe that brings another concern: whatever we choose ad a separator, we must make sure the user doesn't include in his file names `appendfliename` since it'll mess up our parsing (i.e. if he uses space or comma)

## [199] src/aof.c:81 author=madolson reply=true subj=line side=RIGHT cid=774824722
Coming in late, I would also prefer option 1, but don't feel strongly between option 1 and 2.

## [24] src/aof.c:83 author=oranagra reply=false subj=line side=RIGHT cid=751047406
i think this the "file" prefix and camel-case may be redundant
```suggestion
#define AOF_MANIFEST_KEY_FILE_NAME   "file"
#define AOF_MANIFEST_KEY_FILE_SEQ    "seq"
#define AOF_MANIFEST_KEY_FILE_TYPE   "type"
```

## [25] src/aof.c:115 author=oranagra reply=false subj=line side=RIGHT cid=751198657
```suggestion
    return aofInfoDup(item);
```

p.s. i think we can give up this thin wrapper, and just copy the dup code here (we don't use it elsewhere)

## [54] src/aof.c:115 author=chenyang8094 reply=true subj=line side=RIGHT cid=752077704
Also used in `aofManifestDup` to dup base_aof_info

## [26] src/aof.c:139 author=oranagra reply=false subj=line side=RIGHT cid=751206580
this one returns an sds (should be freed with sdsfree), let's change the return type.
same goes for the one below, and maybe others.

## [28] src/aof.c:143 author=oranagra reply=false subj=line side=RIGHT cid=751209049
```suggestion
    return sdscatprintf(sdsempty(), "%s%s%s", MANIFEST_TEM_NAME_PREFIX, server.aof_filename, MANIFEST_NAME_SUFFIX);
```

## [27] src/aof.c:168 author=oranagra reply=false subj=line side=RIGHT cid=751208470
i'd rather add spaces between arguments in function calls.
i know redis is inconsistent in that (each file and function has its own style), but i think that's where we should aim for in new code.
```suggestion
                AOF_MANIFEST_KEY_FILE_NAME, info->file_name,     \
                AOF_MANIFEST_KEY_FILE_SEQ, info->file_seq,       \
                AOF_MANIFEST_KEY_FILE_TYPE, info->file_type)     \
```

## [132] src/aof.c:172 author=yossigo reply=false subj=line side=RIGHT cid=757902946
```suggestion
    sdscatprintf((buf), "%s %s %s %lld %s %c\n",                \
                 AOF_MANIFEST_KEY_FILE_NAME, (info)->file_name, \
                 AOF_MANIFEST_KEY_FILE_SEQ, (info)->file_seq,   \
                 AOF_MANIFEST_KEY_FILE_TYPE, (info)->file_type)
```

Safer use of macro arguments. BTW do we really need a macro here?

## [150] src/aof.c:172 author=chenyang8094 reply=true subj=line side=RIGHT cid=758825536
I think it is necessary. Although there is only one `sdscatprintf` function in the macro, the definition of the format is repeated. Imagine that we need to change the format of the manifest in the future (for example, add new fields in the back), then we only need to change the macro. 

## [29] src/aof.c:179 author=oranagra reply=false subj=line side=RIGHT cid=751258142
i think it looks a bit odd that this macro returns `buf` but doesn't take it as an argument (like sdscat does).
it looks like a memory leak overriding `buf`.
let's either pass `buf` as an argument in addition to `info` (which i prefer), or have the macro do the part of writing to `buf` too (and have no return).

## [30] src/aof.c:200 author=oranagra reply=false subj=line side=RIGHT cid=751261882
```suggestion
 *  when the redis server starts.
```

## [139] src/aof.c:214 author=yossigo reply=false subj=line side=RIGHT cid=758328557
Consider some decoupling, such as
* Populate an arbitrary `aofManifest` rather than directly access `struct server`
* Returns an error and let the caller decide on how to handle it

## [154] src/aof.c:214 author=chenyang8094 reply=true subj=line side=RIGHT cid=758863343
1. I think there is no need to pass the aofManifest parameter here. As you can see, in all the functions I implemented, some passed the aofManifest parameter and some did not. The principle is that if it is a public API, such as being used in server.c, then it is the top-level API, and then it can directly access `server.aof_manifest`. Other functions with aofManifest parameters , They are mainly used internally (aof.c), and may handle two cases of `server.aof_manifest` and `temp_manifest`, so they need a variable parameter.

There are similar top-level APIs (declared in server.h):
```
void aofLoadManifestFromDisk(void);
void aofOpenIfNeededOnServerStart(void);
int aofDelHistoryFiles(void);
int aofRewriteLimited(void);
```
As you can see, if I pass all server.xxxx parameters in the main function of server.c, it will seem meaningless and the style is not uniform.
```
moduleInitModulesSystemLast();
moduleLoadFromQueue();
ACLLoadUsersAtStartup();
InitServerLast();
aofLoadManifestFromDisk();
loadDataFromDisk();
aofOpenIfNeededOnServerStart();
aofDelHistoryFiles();
```

2. I don't think a return value is needed here. The reason is that manifest is a very important file (even more important than `redis.conf`), so once an error occurs, it is safest to exit directly and print the error message.

The following is the function declaration for loading redis.conf:
```
void loadServerConfig(char *filename, char config_from_stdin, char *options);
```

## [140] src/aof.c:235 author=yossigo reply=false subj=line side=RIGHT cid=758330348
Why is this necessary? If we want to distinguish a non-existing file from other errors, we can just consult `errno` no?

## [141] src/aof.c:235 author=yossigo reply=false subj=line side=RIGHT cid=758332334
What is the purpose of the additional `redis_stat`? If all we need is to distinguish an error from a non-existing file, we could just look at `errno` no?

## [161] src/aof.c:235 author=chenyang8094 reply=true subj=line side=RIGHT cid=758876763
First of all, I wrote this code with reference to the existing redis code (similar writing in many places). In addition, I have researched it myself. I think it is not a safe practice to use errno directly.
I give you a ref link:
https://stackoverflow.com/questions/52178334/will-errno-enoent-be-a-sufficient-check-to-check-if-file-exists-in-c

## [31] src/aof.c:248 author=oranagra reply=false subj=line side=RIGHT cid=751268189
i know config.c does the same, but what's the advantage of first reading the lines one by one using `fgets` and concatenating them to a big string, only to split it in a second loop?

## [96] src/aof.c:248 author=chenyang8094 reply=true subj=line side=RIGHT cid=755721167
I have optimized a version, please review it.

## [133] src/aof.c:267 author=yossigo reply=false subj=line side=RIGHT cid=757903567
Consider handling of lines longer than buf - maybe even treat it as an error?

## [134] src/aof.c:270 author=yossigo reply=false subj=line side=RIGHT cid=757903725
Consider forward compatibility and just ignore extra args?

## [151] src/aof.c:270 author=chenyang8094 reply=true subj=line side=RIGHT cid=758828351
have removed 'argc != 6' and add 'argc < 6',  this will allow we to add new fields in the future.

## [32] src/aof.c:275 author=oranagra reply=false subj=line side=RIGHT cid=751270387
i guess it would be a good idea to use `pathIsBaseName` to validate that there's no abuse here.

## [59] src/aof.c:275 author=chenyang8094 reply=true subj=line side=RIGHT cid=753647386
It means to verify whether this file ends with .rdb?

## [64] src/aof.c:275 author=oranagra reply=true subj=line side=RIGHT cid=753845148
no, it verifies that it's a file name without folder names or absolute files (locks redis inside it's `dir` config)

## [142] src/aof.c:276 author=yossigo reply=false subj=line side=RIGHT cid=758334632
Is there any specific reasoning behind both using a key/value line structure *and* insisting on the order of keys per line?

## [160] src/aof.c:276 author=chenyang8094 reply=true subj=line side=RIGHT cid=758870727
I may not understand your question too well, but I think the manifest is a meta file of AOF, which requires a strict format restriction. And the manifest is different from `redis.conf`. `redis.conf` allows users to modify and customize by themselves, but in theory the manifest file can only be generated and modified by redis, so the manifest does not need flexibility, it needs a definite format. I don't know if my answer can solve your question, and I look forward to your feedback.

## [163] src/aof.c:276 author=oranagra reply=true subj=line side=RIGHT cid=759319537
I think Yossi meant that if the manifest lines look like: 
```
file <filename> seq <sequence> type <type> 
```
it implies that they're pairs of key and value, and can be ordered differently, in which case the parser can iterate on the pairs in an if-else chain inside a while loop and don't care about the order.

if it is mandatory that the order is fixed, then we may not need the "key names", and just do
```
<file> <sequence> <type>
```

i.e. regardless of the fact users aren't expected to edit it. the fact we chose this flexible format, means we better also have a flexible parser.

## [164] src/aof.c:276 author=chenyang8094 reply=true subj=line side=RIGHT cid=759332980
Yes, the manifest format is fixed, I think we do not allow the following situations:
```
file appendonly.aof_1.incr.aof seq 1 type i
seq 2 type i file appendonly.aof_2.incr.aof
type i file appendonly.aof_3.incr.aof seq 3
```
The reason why it is displayed in the form of key-value (not just value) is because the user may often look at this file (after all, it is plain text and readable), and the key can clearly tell user the meaning of value. Imagine that when we add other information to the manifest in the future, having a strict key identification can make it easier for us to do format verification. And, in the manifest file, i think we should not lose readability by skimping on a few bytes.

## [166] src/aof.c:276 author=oranagra reply=true subj=line side=RIGHT cid=759397549
i agree, but changing the parsing code to use a while with if-else chain will not complicate the parser much, and will make it flexible.

## [168] src/aof.c:276 author=chenyang8094 reply=true subj=line side=RIGHT cid=759824288
I have modified it to use for loop, Plz review this part  again, thanks.

## [170] src/aof.c:276 author=oranagra reply=true subj=line side=RIGHT cid=759933974
LGTM.
maybe add a comment next to the last `else` and the `<6` to note that it was done for forward compatibility.

## [222] src/aof.c:355 author=oranagra reply=false subj=line side=RIGHT cid=775227953
why did you choose to print to stderr here and not to the log file.
note that if redis was started daemonized, then these prints go nowhere.
at the time this code is executed (in loadDataFromDisk), the old code in redis used to print failures to the log file.

## [143] src/aof.c:370 author=yossigo reply=false subj=line side=RIGHT cid=758338142
Probably safe to simply assert here, AFAIR it should only fail if `zmalloc()` fails - which should end up in an earlier panic anyway.

## [67] src/aof.c:435 author=oranagra reply=false subj=line side=RIGHT cid=753848961
```suggestion
    /* server.aof_fd != -1 means that AOF is open, then we must
     * skip the last AOF, because this one is our currently writing. */
```

## [33] src/aof.c:445 author=oranagra reply=false subj=line side=RIGHT cid=751303363
we're running from tail to head, don't we want to push to head? (not that it really matters)

## [55] src/aof.c:445 author=chenyang8094 reply=true subj=line side=RIGHT cid=752179441
Here I want to ensure the order they were before.

## [56] src/aof.c:445 author=oranagra reply=true subj=line side=RIGHT cid=752184958
yes, i'm arguing that you're reversing the order.
maybe i'm missing something... 
since you're iterating the old list from tail to head, if you're inserting to the tail of the new list, it'll reverse the order.

## [93] src/aof.c:445 author=chenyang8094 reply=true subj=line side=RIGHT cid=755205320
Yes you are right, i am wrong. fixed ：)

## [34] src/aof.c:490 author=oranagra reply=false subj=line side=RIGHT cid=751311400
styling. i suggest to indent by 4 rather than align to some position (certainly no the offset of the second argument, but also not the first).
this way, if the log function we call (in this case serverLog) is renamed, we don't need to re-align the arguments.
i also don't see why in this case we wanna designate a separate line per argument (certainly not if we didn't for LL_WARNING)
Feel free to apply to others calls (the reason i commented on this one is because it attracted my eye due to an excessive space on the last line)
```suggestion
        serverLog(LL_WARNING,"Fail to fsync the temp AOF file %s: %s.", 
            temp_amname, strerror(errno));
```

## [35] src/aof.c:505 author=oranagra reply=false subj=line side=RIGHT cid=751312637
we get here on `open` failures too
```suggestion
    if (fd != -1) close(fd);
```

## [36] src/aof.c:511 author=oranagra reply=false subj=line side=RIGHT cid=751316500
do we really need to designate a special function for that?
i think it should always come together with a call to `getAofManifestAsString` anyway, so i think we should collapse this one into persistAofManifest

## [38] src/aof.c:549 author=oranagra reply=false subj=line side=RIGHT cid=751324399
i think this log message shouldn't be LL_DEBUG. i guess `LL_NOTICE` is good, and even LL_WARNING may be ok (there are not a lot of these).

## [144] src/aof.c:553 author=yossigo reply=false subj=line side=RIGHT cid=758343160
We probably want to keep it dirty if write failed.

## [145] src/aof.c:557 author=yossigo reply=false subj=line side=RIGHT cid=758343374
```suggestion
/* When AOFRW success, the previous BASE and INCR AOFs will 
```

## [39] src/aof.c:568 author=oranagra reply=false subj=line side=RIGHT cid=751325416
so if redis crashes after we wrote that file, we'll be left with garbage on the disk.
this garbage isn't affecting us, so i suppose we're ok with that, but why don't we use the `bg_unlink` mechanism?
i.e. logically delete now, and pay the deletion price in the thread...

it still wouldn't guarantee there's no garbage since we don't sync the directory structure, but the difference is that we'll leave garbage only on power / system failures (not if the process crashes or is killed)

## [58] src/aof.c:568 author=chenyang8094 reply=true subj=line side=RIGHT cid=752220571
Changed to `bg_unlink` and deleted the code added in bio

## [172] src/aof.c:580 author=oranagra reply=false subj=line side=RIGHT cid=770893232
I think the temp dir must be based on the server.aof_filename pattern, and not a constant one.
otherwise, if there are two servers in the same dir, their temp upgrade dir clash.

## [328] src/aof.c:586 author=yossigo reply=false subj=line side=RIGHT cid=776832871
```suggestion
        serverLog(LL_WARNING, "Can't open or create append-only dir %s: %s", 
            server.aof_dirname, strerror(errno));
```

## [173] src/aof.c:596 author=oranagra reply=false subj=line side=RIGHT cid=770895001
does that cause a restart of the upgrade? looks like we're exiting with an error?
maybe i'm missing something (looking at the diff of this commit in GH rather than the full source tree)

## [174] src/aof.c:596 author=oranagra reply=true subj=line side=RIGHT cid=772143017
ohh, looking at this again, i realize the error is not related to the comment above it (the upgrade restarts only if we successfully remove the dir, but if we can't then we fail and exit.
i suppose this is not expected to ever happen.

## [180] src/aof.c:602 author=oranagra reply=false subj=line side=RIGHT cid=773684494
maybe it's a better idea actually rename it to the normal format and add a `base` suffix etc, and not just move it to the folder?

we probably can't afford to put a suffix on it (we don't know the content), though, we maybe that's not a good idea, unless we conclude to remove the suffix from our file name template anyway.

note that if we do that, we need to relax the upgrade failure recovery conditions in `strcmp(am->base_aof_info->file_name, server.aof_filename)`, but i'm not sure that's a problem.

## [183] src/aof.c:602 author=chenyang8094 reply=true subj=line side=RIGHT cid=773795695
No, we cannot add a suffix when rename here,  because this is the only way to identify the interrupted upgrade.

`am->base_aof_info->file_name` is same with `server.aof_filename` only appears when upgrading.

Otherwise, if a file with a suffix appears in the manifest but does not exist in the AOF dir, we consider this to be a serious error and the process exits directly.

## [187] src/aof.c:602 author=oranagra reply=true subj=line side=RIGHT cid=773837992
It's not the "only way" we identify it. it's at the very least a combination of a file in the manifest that's missing on the disk, and the presence of the old (legacy location) file on the disk. but i do agree that this make our interrupted upgrade detection better, so i'll drop it.

p.s. if we wanted we could have also added an indication in the manifest that this is the upgrade file.

## [43] src/aof.c:615 author=oranagra reply=false subj=line side=RIGHT cid=751420552
let's be sure to have a test case for this flow.

## [51] src/aof.c:615 author=chenyang8094 reply=true subj=line side=RIGHT cid=751965265
@oranagra  About this test i wanna modify and improve it, please review the tests part later, thank you.

## [327] src/aof.c:618 author=yossigo reply=false subj=line side=RIGHT cid=776832493
```suggestion
    serverLog(LL_NOTICE, "Successfully migrated an old-style AOF file (%s) into the AOF directory (%s).", server.aof_filename, server.aof_dirname);
```

Updated the log message to be more clear to users.

## [146] src/aof.c:637 author=yossigo reply=false subj=line side=RIGHT cid=758344850
```suggestion
    /* Dup a temp aof_manifest to modify. */
```

## [329] src/aof.c:642 author=yossigo reply=false subj=line side=RIGHT cid=776833132
```suggestion
        serverLog(LL_NOTICE, "Removing the history file %s in the background.", ai->file_name);
```

## [97] src/aof.c:651 author=chenyang8094 reply=false subj=line side=RIGHT cid=755992070
@oranagra I implemented this function according to the previous description, because I think we should need it now, because in actual applications AOFRW is often happens (such as  the child OOM), and redis  automatically retry AOFRW is so fast ( default 10HZ), so it is easy to create a bunch of small INCR AOF files.

I am just solving it by increasing the delay linearly (of course there may be other non-linear better algorithms), I think it can solve our problem, if AOFRW has failed 10 times (maybe we need to reduce this value ), then the subsequent AOFRW will become slower and slower.

## [102] src/aof.c:651 author=oranagra reply=true subj=line side=RIGHT cid=756159044
the simple mechanism seems ok, or we can maybe improve (i don't mind too much).
maybe just make it clear in the top comment what's the return value of this function (true to prevent rewrite)

maybe a slightly better one would be to start with a 1 minute delay and then double the delay time on each failure up to a limit of one day?
WDYT?

## [104] src/aof.c:651 author=oranagra reply=true subj=line side=RIGHT cid=756163048
> 16 times (maybe we need to reduce this value

16 seem too high to me too, maybe 8 or even 4 would work?
just remember it can't be lower since we don't count failures, we're counting files, so the base is 3

## [109] src/aof.c:651 author=chenyang8094 reply=true subj=line side=RIGHT cid=756523137
`AOF_REWRITE_LIMITE_THRESHOLD` has been changed to 3, which means that AOFRW may have failed 2 or 3 times continuously. 

`AOF_REWRITE_LIMITE_NAX_DELAY` has been modified to 1440 minutes (1 day). Although I think it may be a bit long and 1440 is not an exponent of 2, but it is fine.

## [114] src/aof.c:651 author=oranagra reply=true subj=line side=RIGHT cid=756759019
maybe you're right (that a day is too long).
if it fails consistently, then i don't care to delay it for one day, the only problem i see with it is that if the admin fixes the problem (adds more disk / ram space), it'll want to trigger a retry manually ASAP rather than wait for the next interval, and waiting one day may be too long.
so either we add some mechanism to override that (like `BGREWRITEAOF FORCE`), or reduce back to one hour, which may be an acceptable time to wait.
WDYT?

either way, let's be sure to document this in the top comment for the purpose of release notes and other reviewers (which will only look at the description, not the code)

## [119] src/aof.c:651 author=chenyang8094 reply=true subj=line side=RIGHT cid=756834907
I think 1 hour and 1 day are fine, because we can still use the 'bgrewriteaof' command to execute AOFRW immediately during the limit period.

So we reduce back to one hour, just like: 1, 2, 4, 8, 16, 32, 60 

## [330] src/aof.c:670 author=yossigo reply=false subj=line side=RIGHT cid=776833643
```suggestion
        serverLog(LL_WARNING, "Can't open or create append-only dir %s: %s", 
            server.aof_dirname, strerror(errno));
```

## [147] src/aof.c:685 author=yossigo reply=false subj=line side=RIGHT cid=758348200
Perhaps we should consider a smaller max delay. Even if we use 5 minutes, that's max 288 files per day which is still not a HUGE number of files (assuming someone is monitoring the system at reasonable interval).

## [149] src/aof.c:685 author=oranagra reply=true subj=line side=RIGHT cid=758440240
LOL.. i was arguing for a day.
i think there are cases where something can fail for weeks and no one will notice.
as discussed in another thread, if the admin solves the problem he can run BGREWRITEAOF instead of waiting for the next interval, so why make it that short?

## [159] src/aof.c:685 author=chenyang8094 reply=true subj=line side=RIGHT cid=758868972
Yes, I discussed with @oranagra . It was originally one day. I changed it to 1 hour later. In any case, once the user finds and solves the related problem, he can use BGREWRITEAOF to execute AOFRW immediately without waiting for the limit. So I think there is no need to change here. @yossigo WDYT?

## [41] src/aof.c:714 author=oranagra reply=false subj=line side=RIGHT cid=751408662
it could be that this is the first time a user tries to enable AOF at runtime, in which case i think a disk error should not be fatal. but i see that `openNewIncrAofForAppend` does `exit(1)` (unlike `openLastOrCreateIncrAofForAppend`).
lets see if we can figure out a solution, maybe it should do the `openNewIncrAofForAppend` before the fork?

## [60] src/aof.c:714 author=chenyang8094 reply=true subj=line side=RIGHT cid=753649606
Okay, I have made aof open before redis-fork so that we can find disk errors in advance.

## [68] src/aof.c:714 author=oranagra reply=true subj=line side=RIGHT cid=753850079
i see that now `startAppendonly` creates a new file, but it doesn't write to `server.aof_fd` until after `rewriteAppendOnlyFileBackground` (same as it is in unstable), however, rewriteAppendOnlyFileBackground does this:
```c
if (server.aof_fd != -1) openNewIncrAofForAppend();
```
so i think openNewIncrAofForAppend still has a chance to `exit`.
and in any case, it'll open the file again, writing to `server.aof_fd`, and we probably have an FD leak.

## [89] src/aof.c:714 author=chenyang8094 reply=true subj=line side=RIGHT cid=755176188
Sorry I didn't understand what you mean. When we  calls `rewriteAppendOnlyFileBackground` in `startAppendonly` function, `server.aof_fd` must be -1, so 
 `rewriteAppendOnlyFileBackground` will not be opened new aof file. I don’t understand under what circumstances the FD leak you mentioned happened。

## [99] src/aof.c:714 author=oranagra reply=true subj=line side=RIGHT cid=756127541
ohh, i now see i got the condition in rewriteAppendOnlyFileBackground wrong (the one in step 4 below)

1. startAppendOnly does `newfd = open()`
2. startAppendOnly calls rewriteAppendOnlyFileBackground
3. rewriteAppendOnlyFileBackground does `if (server.aof_fd != -1) openNewIncrAofForAppend();`
4. openNewIncrAofForAppend does `server.aof_fd = open()`
5. startAppendOnly does `server.aof_fd = newfd;` (after rewriteAppendOnlyFileBackground returns with success)

i'd still argue that this is a bit confusing, and that maybe it would have been better to open the new file just in one place.
i understand that the `open()` in `startAppendOnly` is the one i asked for (for graceful failure). and that we'll need one for reoccurring rewrite (which can be in rewriteAppendOnlyFileBackground).

maybe we can solve it in a cleaner way if we move the opening of the new file in rewriteAppendOnlyFileBackground to before `fork()` is called (it doesn't really matters if it is done in the parent before or after fork), and then maybe it's nicer to have just one call to `open`, or even two in some if-else chain, and it'll be less confusing as to which function is responsible of opening the new file and which one is responsible of setting `server.aof_fd` (they'll all be in just one place).

## [108] src/aof.c:714 author=chenyang8094 reply=true subj=line side=RIGHT cid=756489946
@oranagra  I refactored the `openNewIncrAofForAppend` function to achieve consistency guarantee without exit when an error occurs, and I move it before redis-fork. PLZ  review this part again, thanks.

## [209] src/aof.c:796 author=oranagra reply=false subj=line side=RIGHT cid=775213392
doesn't this mean the max limit will be 32?
i suppose we should remove the condition for `limit_deley_minutes < AOF_REWRITE_LIMITE_NAX_MINUTES` and always do the ` <<= `.

p.s. since we're aiming to multiply by 2, and not do some bit manipulation, i think the code will be clearer if we use ` *= 2`. (performance is all the same anyway)

## [331] src/aof.c:804 author=yossigo reply=false subj=line side=RIGHT cid=776834519
```suggestion
                "Background AOF rewrite has repeatedly failed %ld times and triggered the limit, will retry in %d minutes", 
                incr_aof_num, limit_deley_minutes);
```

## [45] src/aof.c:1055 author=oranagra reply=false subj=line side=RIGHT cid=751435086
styling. `if` with multiple lines should move the `{` to the next line (less confusing indentation).
```suggestion
    if (server.aof_state == AOF_ON ||
        (server.aof_state == AOF_WAIT_REWRITE && server.child_pid != -1))
    {
```

## [75] src/aof.c:1075 author=oranagra reply=false subj=line side=RIGHT cid=754928722
maybe move that to be next to loadingProgress / startLoadingFile, and rename to just `loadingIncrProgress`.
i.e. a generic progress report that reports the delta rather than absolute.

## [76] src/aof.c:1115 author=oranagra reply=false subj=line side=RIGHT cid=754929451
is `base_size` better named `last_progress_report_size`?
i think "base" is not a good word here (since it keeps changing).

## [85] src/aof.c:1115 author=oranagra reply=false subj=line side=RIGHT cid=755096863
maybe it would be cleaner if `progress_size` will be a local variable declared when setting it?
and / or call it `progress_delta`?

## [46] src/aof.c:1139 author=oranagra reply=false subj=line side=RIGHT cid=751436032
so i understand this PR doesn't have the double write code....
indeed it probably makes the code cleaner, but we need to discuss it to make sure we all agree if this is ok.

## [50] src/aof.c:1139 author=chenyang8094 reply=true subj=line side=RIGHT cid=751806860
Yes,  double writing will make the code look more complicated (I implemented it in #9539 ).  

 In addition, although redis may create infinite number of files on the disk if AOFRW is repeatedly failing. But this is almost impossible or very rare in reality, because AOFRW will bring fork overhead every time, and users will not let AOFRW fail like this.

## [53] src/aof.c:1139 author=oranagra reply=true subj=line side=RIGHT cid=752006248
can you estimate how much more overhead this feature adds?
i guess the double writing code on it's own is not more than 5 lines.
but the file tracking / renaming may be a bit more...

I do think repeated failures will be common... user can find out he has insufficient memory after a week of repeated failures and maybe thousands of files on the disk.

## [61] src/aof.c:1139 author=chenyang8094 reply=true subj=line side=RIGHT cid=753675790
I don’t think there will be a lot of work. The key is that we are sure that it is necessary to do this. I think this will cause two problems:
1. Code understanding may bring some burdens
2. When double writing files (obviously we can't get around the atomicity problem), when one  successfully and another file fails written, should we directly exit?

Of course,  double writing can indeed solve the possibility of hundreds of INCR AOFs.

Therefore, we can make a decision as soon as possible, I can proceed to implement it.

## [71] src/aof.c:1139 author=chenyang8094 reply=true subj=line side=RIGHT cid=754227580
@oranagra Do you have any ideas here? I wonder if we still have a solution, that is, once we find that our INCR AOF number reaches a threshold (for example 32, this means AOFRW have  consecutive failed 32 times), we stop the automatic retry AOFRW and print a log and set a global status. I think the current unlimited AOFRW is not very reasonable(If AOFRW keeps failing so many times, it means there must be some serious problem, such as insufficient memory, some modules have bugs when implementing AOF rewrite, etc., I think it is meaningless to automatically retry AOFRW in this case.), AOFRW also will bring fork overhead every time. what do you think?

I think we can forcibly set `server.aof_rewrite_base_size` or clear `server.aof_rewrite_scheduled` to delay or close automatic AOFRW.

## [72] src/aof.c:1139 author=oranagra reply=true subj=line side=RIGHT cid=754893676
I discussed it with the core team today.
we concluded that we rather not invest in double write for now, and we can always add that later if we get feedback about problems with infinite number of files.

The suggestion of some AOFRW throttling also came up.
i'm not certain how to do it (don't like to change a config), we can try thinking of it, but that's also something that can be added in the future.

## [73] src/aof.c:1139 author=chenyang8094 reply=true subj=line side=RIGHT cid=754903329
@oranagra  Well, I don't think this requires a configuration. We can simulate the tcp retransmission algorithm (Binary Exponential Back off). When AOFRW fails, we have at least twice the time (or size) to retry AOFRW again (Don't try again immediately like now). If it fails again, it will double. We only need to determine a maximum internally.

This is just my simple idea, maybe we have a better way.

## [74] src/aof.c:1139 author=oranagra reply=true subj=line side=RIGHT cid=754907586
ohh, sorry, i was confusing `server.aof_rewrite_base_size` with `server.aof_rewrite_perc` (didn't want the backoff be visible in `CONFIG GET`).

## [136] src/aof.c:1177 author=yossigo reply=false subj=line side=RIGHT cid=758177201
```suggestion
/* Replay an append log file. On success AOF_OK is returned,
```

## [77] src/aof.c:1191 author=oranagra reply=false subj=line side=RIGHT cid=754930412
maybe this better be after the loop rather than inside the condition that breaks?

## [137] src/aof.c:1200 author=yossigo reply=false subj=line side=RIGHT cid=758178138
```suggestion
            serverLog(LL_WARNING,"Failed to open the append log file %s: %s",filename,strerror(errno));
```

The error message was incorrect, it may exist but fail to open for other reasons.

## [153] src/aof.c:1200 author=chenyang8094 reply=true subj=line side=RIGHT cid=758857987
The situation you mentioned has been handled in the if branch, and definitely doesn't exist error in the else branch (this code is currently redis used, I did not modify it)

## [332] src/aof.c:1288 author=yossigo reply=false subj=line side=RIGHT cid=776835168
```suggestion
            serverLog(LL_WARNING,"Failed to access the append log file %s: %s", filename, strerror(errno));
```

Updating the log message because the error may not necessarily be due to a non-existing file.

## [341] src/aof.c:1288 author=chenyang8094 reply=true subj=line side=RIGHT cid=776893763
No, the file must not exist here, because we ruled out this possibility in the if branch.

## [348] src/aof.c:1288 author=chenyang8094 reply=true subj=line side=RIGHT cid=776894648
and note we will return AOF_NOT_EXIST error code.

## [349] src/aof.c:1288 author=oranagra reply=true subj=line side=RIGHT cid=777004480
what we know here is that both `fopen` and `fstat` failed.
i guess you could change
```diff
-if (redis_stat(aof_filepath, &sb) == 0) {
+if (redis_stat(aof_filepath, &sb) == 0 || errno!=ENOENT) {
```
then when you get here, you know for sure it doesn't exist.

## [350] src/aof.c:1288 author=oranagra reply=true subj=line side=RIGHT cid=777019408
Yes, but does it mean that when it returns -1 the file must not exist? 
It could be a range of other errors. 

## [210] src/aof.c:1337 author=oranagra reply=false subj=line side=RIGHT cid=775214574
this division always gave me the shivers. maybe it's time to change that constant to 1024?

## [47] src/aof.c:1367 author=oranagra reply=false subj=line side=RIGHT cid=751601176
when loading an RDB, i think we may want to set `rdbFileBeingLoaded` (which `startLoadingFile` sets) in some way,
this controls behavior when corruption is detected.
and it would also be a good idea to pass the file size.
on the other hand, i don't think we wanna do a separate startLoading / stopLoading per file, and instead use just one pair like before.
so we need to find a way to determine the file size ahead of time, and update the file name when needed.
let's look into it and see what we can come up with.

## [78] src/aof.c:1382 author=oranagra reply=false subj=line side=RIGHT cid=754931203
nit pick. i prefer just one line, it's not long enough to justify ugly line break IMHO

## [63] src/aof.c:1394 author=chenyang8094 reply=false subj=line side=RIGHT cid=753676533
@oranagra   Do you mean that all INCR AOF loads only use a pair of `startload/stopload`? Then use an function like `updateLoadSize` to update the file size in the middle?

## [66] src/aof.c:1394 author=oranagra reply=true subj=line side=RIGHT cid=753848583
yes, just one pair of startLoad / stopLoad, ideally we should know the total size of all when we start loading, and fix the relevant places not to reset the read counters when we switch files.
thing is that the arguments for this startLoading function are used for two things:
1. the file name is used in order to know what to report on failures, and be able to distinguish between diskless and dis-based replication (see `rdbReportError`)
2. the size argument is used for progress reporting, see `INFO persistence`, i.e. `loading_total_bytes`, `loading_loaded_bytes`, `loading_loaded_perc`.

we need to somehow make them work correctly. 

## [70] src/aof.c:1419 author=chenyang8094 reply=false subj=line side=RIGHT cid=754214398
@oranagra I added a `startLoadingIncrAofFiles` function to set the size of the entire INCR AOFs, and then in the `loadSingleAppendOnlyFile` function, `loadingIncrAofProgress` will be used to update the progress of the loading. Unlike `loadingRdbProgress` (originally `loadingProgress`), `loadingIncrAofProgress` receives a relative size (not a Absolute file offset).  Please review the code here if there is any problem, thank you.

## [79] src/aof.c:1419 author=oranagra reply=true subj=line side=RIGHT cid=754933223
I meant just one startLoading for the whole thing (not one for the base and one for all incr files)

regarding the new functions that's ok, just that maybe the function naming should be neutral and just state the fact of what they do (relative or absolute instead of RDB or AOF) 

## [95] src/aof.c:1419 author=chenyang8094 reply=true subj=line side=RIGHT cid=755226738
@oranagra  Please review this part again，thanks.

## [98] src/aof.c:1448 author=chenyang8094 reply=false subj=line side=RIGHT cid=755993022
@oranagra  PLZ review this part again, thanks.

## [300] src/aof.c:1451 author=oranagra reply=false subj=line side=RIGHT cid=775864818
this looks odd to me, as it could override other valid statuses so it looks dangerous (like failure or different types of success).
in practice, as far as i can tell, we init it to OK at the top, and after that the only way to get here with a non-OK status is the TRUNCATED status. so i think this line should be removed.

## [62] src/aof.c:1456 author=chenyang8094 reply=false subj=line side=RIGHT cid=753676307
@oranagra Here I pass size as a parameter to startLoadingFile. I think the original parameter `fp` is too restrictive for caller (must be a file opened in advance).

## [211] src/aof.c:1479 author=oranagra reply=false subj=line side=RIGHT cid=775215030
i think we must reflect that truncation in the return value, and let the caller handle it.
if this was not the last AOF file in the list, we can't afford to continue to handle the next one.
we can choose between:
1. skip any remaining AOF files.
2. fail with an error since we only expect truncation in the last file

i think i prefer 2.
maybe we should reflect this decision in the PR top comment, for other reviewers to see.

## [257] src/aof.c:1479 author=chenyang8094 reply=true subj=line side=RIGHT cid=775452024
Yes, under normal circumstances, only the last AOF will have abnormal MULTI/EXEC, i prefer 2 too.

## [148] src/aof.c:1493 author=yossigo reply=false subj=line side=RIGHT cid=758386138
Consider logging "file [x/n]" to help troubleshooting and give better visibility into the loading process?

## [162] src/aof.c:1493 author=chenyang8094 reply=true subj=line side=RIGHT cid=758884107
Do we really need this? Currently, we can get the progress of loading through `server.loading_loaded_bytes` and `server.loading_loaded_perc` (print in redis info). In addition, if there is an error in the intermediate loading, we will also print out the file name with the error. I think the information we printed is enough now, @oranagra @yossigo WDYT?

## [165] src/aof.c:1493 author=oranagra reply=true subj=line side=RIGHT cid=759342340
the code we're looking at, prints a message when the file is present and was loaded successfully,
and obviously when there's some parsing error, it prints a log message to (inside the function).
besides that i see the function also has a specific print when the file is missing, and when it's empty.
so i think that log-wise we're covered.

i do see that we fail the loading when the file is present and empty, and maybe that's not good?
the old code used to succeed when a file was either missing or empty.
for the new code, we don't expect it to be missing (if we added it to the manifest, it should be there), but it could be empty (if there's no traffic after rewrite started).

maybe the base file must never be empty, since that's either an RDB, or an AOF with at least a SELECT statement?
but what if the server just started (a new deployment with no pre-existing data or persistence files), and then it is restarted after it created the AOF file and didn't yet process any write command?

i suppose the same could also be with upgrades (no manifest file).
and i guess this concern will be resolved once we implement #9794 and always generate an RDB as base (even when starting empty).

## [167] src/aof.c:1493 author=chenyang8094 reply=true subj=line side=RIGHT cid=759789250
@oranagra I think you may have misunderstood it. The current implementation is: Only when AOF does not exist will load error, when it is empty, we will directly load the next one, The following is my handling of the return value:
```
 /* If an AOF exists in the manifest but not on the disk, we consider 
 * this to be a fatal error. */
if (ret == AOF_NOT_EXIST) ret = AOF_FAILED;

if (ret != AOF_OK && ret != AOF_EMPTY) {
    goto cleanup;
}
```

## [169] src/aof.c:1493 author=oranagra reply=true subj=line side=RIGHT cid=759932120
yes, you're right, i misinterpreted that code.
so we skip empty files, and fail on non existing ones, and either of the 4 states (missing, empty, error, success) there is a log print.
i think we're good, but maybe i'm missing Yossi's intention in the original comment.

## [178] src/aof.c:1511 author=oranagra reply=false subj=line side=RIGHT cid=773680879
```suggestion
     * 2. If the 'server.aof_dirname' directory exists but the manifest file is missing
```

## [179] src/aof.c:1520 author=oranagra reply=false subj=line side=RIGHT cid=773682145
indentation and line break
```suggestion
             !strcmp(am->base_aof_info->file_name, server.aof_filename) && !aofFileExist(server.aof_filename)))
        {
```

## [188] src/aof.c:1520 author=oranagra reply=true subj=line side=RIGHT cid=773838756
i think the indentation change is in some way missing (because of the parenthesis)

## [208] src/aof.c:1524 author=oranagra reply=false subj=line side=RIGHT cid=775212338
```suggestion
             !strcmp(am->base_aof_info->file_name, server.aof_filename) && !aofFileExist(server.aof_filename))) 
```

## [301] src/aof.c:1558 author=oranagra reply=false subj=line side=RIGHT cid=775874649
i think it's odd to hide (override) the TRUNCATED status, let's propagate it to the caller when it's valid.
and override it with FAILED when it's not in the last file.
we'll need to update the two callers of that function to handle that value correctly.
debug.c isn't expecting it, so it can consider it as failure (maybe revert your change there?)
and server.c can consider it as success (which it already does AFAICT).

## [318] src/aof.c:1565 author=oranagra reply=false subj=line side=RIGHT cid=775909556
need to add TRUNCATED here, or maybe just change it to an `else` for the above?

## [321] src/aof.c:1565 author=chenyang8094 reply=true subj=line side=RIGHT cid=775914804
Changed to `if (ret == AOF_OPEN_ERR || ret == AOF_FAILED)`

## [322] src/aof.c:1565 author=oranagra reply=true subj=line side=RIGHT cid=775916165
is any of the other states valid in this case? like NOT_EXIST and OPEN_ERR?

## [323] src/aof.c:1565 author=chenyang8094 reply=true subj=line side=RIGHT cid=775917548
#define AOF_OK 0     -> OK
#define AOF_NOT_EXIST 1  -> AOF_FAILED -> goto cleanup
#define AOF_EMPTY 2  -> OK
#define AOF_OPEN_ERR 3 -> goto cleanup
#define AOF_FAILED 4 -> goto cleanup
#define AOF_TRUNCATED 5 -> AOF_FAILED (if not last file) -> goto cleanup

## [325] src/aof.c:1565 author=oranagra reply=true subj=line side=RIGHT cid=775937228
ok, i think you're right. wanted to double check.

## [320] src/aof.c:1593 author=chenyang8094 reply=true subj=line side=RIGHT cid=775914671
Changed to `if (ret == AOF_OPEN_ERR || ret == AOF_FAILED) `

## [42] src/aof.c:2109 author=oranagra reply=false subj=line side=RIGHT cid=751418070
we're soon gonna completely remove the code that generates RESP code from inside a fork, i.e. AOFRW will always create an RDB file.
This means that the feature of calling BGREWRITEAOF when AOF is not enabled, should no longer be supported (it's the same as calling BGSAVE).

We can clean up this code in a later PR, but since i see this scenario is causing issues in this one, maybe we can remove that now.
i.e. don't remove yet all the AOF text generation, but do change this command to return an error if `server.aof_state != AOF_ON` and then you can delete the `createnew` argument..
or we can do that later.

p.s. if we do that now, let's mention it in a behavior changes section in the top comment.

## [90] src/aof.c:2109 author=chenyang8094 reply=true subj=line side=RIGHT cid=755177820
I have restored the declaration of the `rewriteAppendOnlyFileBackground` function.

## [100] src/aof.c:2109 author=oranagra reply=true subj=line side=RIGHT cid=756132458
do we have no other complications in this PR due to the fact BGREWRITEAOF can be called when AOF is not enabled?
if we do, let's return an error here in that case, and simplify the code.
if we don't then this case will be deleted anyway in a few weeks by another PR.

## [107] src/aof.c:2109 author=chenyang8094 reply=true subj=line side=RIGHT cid=756246041
Calling BGREWRITEAOF when AOF is disable is no problem. The only difference is that we will not open a new INCR AOF (similar to not write aof rewrite buf). But we will still update and generate the correct BASE file (it can be AOF or RDB, depending on whether we will enable `aof-use-rdb-preamble`). These have corresponding TCL tests.

I think our ultimate goal is to create BASE file as RDB file (such as `aof-use-rdb-preamble` is enabled by default). But I don't quite understand why `bgrewriteaofCommand` must return err (when AOF is off) or be deleted.





## [111] src/aof.c:2109 author=chenyang8094 reply=true subj=line side=RIGHT cid=756606530
@oranagra I understand what you mean, we let BASE directly become rdb, delete `rewriteAppendOnlyFileRio` and related code. But I'm not sure if this will cause any problems, such as use for analysis or parsing relying on the AOF format, and now it must face both the RDB format (base) and the AOF format (incr). Do we still keep `aof-use-rdb-preamble` to let users make choices?

## [113] src/aof.c:2109 author=oranagra reply=true subj=line side=RIGHT cid=756753619
> The only difference is that we will not open a new INCR AOF (similar to not write aof rewrite buf). But we will still update and generate the correct BASE file 

examples of issues: will it create a meta file? what happens if we then restart redis with aof enabled? will it load that AOF file?

Deleting the `aof-use-rdb-preamble` config and the code behind `rewriteAppendOnlyFileRio` is a topic for the next PR.
I do think we wanna delete them, that's a lot of code that is hard to maintain, and i know it gives module authors a hard time.
But anyway, for the purpose of this PR, i just say that if the option of calling bgrewriteaofCommand when AOF is disabled is causing any complications, i think we can delete it right now, since sooner or later we don't want to support that option (it'll be identical to bgsaveCommand)

## [116] src/aof.c:2109 author=chenyang8094 reply=true subj=line side=RIGHT cid=756822672
Yes, manifest file will be created, because we will generate a BASE AOF/RDB, as long as there is a change in the AOF file, we have to track it.

If redis restarts and AOF is enabled, of course we have to load it. This logic is the same as the original redis。

AOF enable and disable do not affect the creation of the manifest, it only affects whether we write AOF files and whether a new INCR AOF will be generated during AOFRW.

If we are sure to delete the `aof-use-rdb-preamble` configuration and the `rewriteAppendOnlyFileRio` related code, I think I can do it in this PR because they have more or less impact on the current PR. I am happy to do this.

## [118] src/aof.c:2109 author=oranagra reply=true subj=line side=RIGHT cid=756831765
> Yes, manifest file will be created .... This logic is the same as the original redis

ok good.

so it does seem like these (rewrite that generates an AOF format, and the possibility to call BGREWRITEAOF when aof is disabled) aren't complicating this PR too much, and in that case it is better to do that change in a different PR.
it'll make the discussion on these easier (to discuss them separately), and will not block the merging of this PR.

## [80] src/aof.c:2253 author=oranagra reply=false subj=line side=RIGHT cid=754937734
maybe that's another metadata to keep in the manifest?

## [83] src/aof.c:2253 author=chenyang8094 reply=true subj=line side=RIGHT cid=755083385
I understand what you mean. If we add `size` in manifest, do we need to check whether the size is the same as the actual size of the file when we load it?

There are still some questions about the manifest, that is, now we only have “name/type/seq” meta in manifest, do we need other meta, such as the timestamp of file creation, or other information (maybe we will use it in the future), because Once the manifest format is publish, it will not be modified later.

## [84] src/aof.c:2253 author=oranagra reply=true subj=line side=RIGHT cid=755095375
yes, i'm not certain about my suggestion either.
i don't think we need do validate the size (as long as it's just used for progress report)
but anyway, it was just an idea, we can drop it.

## [334] src/aof.c:2257 author=yossigo reply=false subj=line side=RIGHT cid=776836020
```suggestion
        serverLog(LL_WARNING, "Can't open or create append-only dir %s: %s", 
            server.aof_dirname, strerror(errno));
```

## [5] src/config.c:2564 author=oranagra reply=false subj=line side=RIGHT cid=750425302
these new config interfaces need to be documented in redis.conf, and also listed in the top comment of the PR under some "interface changes section"
note that we use the PR top comment as a squash-merge commit comment as also for the purpose of release notes.

## [101] src/config.c:2575 author=oranagra reply=false subj=line side=RIGHT cid=756150206
i think this should not be a config, but rather a DEBUG sub-command (completely unreachable for normal users).
see `SET-ACTIVE-EXPIRE` and alike.
p.s. i haven't looked at the tests yet, but maybe there's a different way to induce a failure without the need of such a config.
maybe by using `rdb-key-save-delay`, and then killing the child from the test (with `kill`)

## [110] src/config.c:2575 author=chenyang8094 reply=true subj=line side=RIGHT cid=756540862
That is what I did before this commit, but I encountered a problem when writing the AOFRW limit test, because I wanted to simulate a scenario where the auto rewrite failed. But after we changed `AOF_REWRITE_LIMITE_THRESHOLD` to 3, it is easy to achieve the goal using kill. So I rolled back the configuration now.

## [115] src/config.c:2575 author=oranagra reply=true subj=line side=RIGHT cid=756762336
FYI: just note i wrote `rdb-key-save-delay` (not `aof-child-rewrite-delay`).

## [120] src/config.c:2575 author=chenyang8094 reply=true subj=line side=RIGHT cid=756839810
I have removed the `aof-child-rewrite-delay` configuration and used the `rdb-key-save-delay` instead.

## [6] src/config.c:2637 author=oranagra reply=false subj=line side=RIGHT cid=750426388
`INT_MIN` doesn't seem right.
This one is just for testing, but worth mentioning in the top comment anyway.
Thing is that some reviewers (mainly the core team), may not bother to read the entire source code, and will approve this PR based on the detailed top comment. 

## [7] src/server.c:3214 author=oranagra reply=false subj=line side=RIGHT cid=750431612
i haven't yet got to read the code behind it, but i wonder why this needs to be in cron, and not just when AOFRW succeeds or an AOF is being loaded after a crash?

## [12] src/server.c:3214 author=chenyang8094 reply=true subj=line side=RIGHT cid=750804192
Yes, I actually thought about it. The `delHistoryAofFilesCron` function will use bio to delete all history files and update the manifest file on the disk simultaneously. In theory, it is not a very heavy operation, so let's move it to backgroundRewriteDoneHandler?

## [14] src/server.c:3214 author=chenyang8094 reply=true subj=line side=RIGHT cid=750832655
There may be this possibility: the user exited the process before cleaning up the history (redis was killed), and the history files will be kept until the next rewrite success.

## [16] src/server.c:3214 author=oranagra reply=true subj=line side=RIGHT cid=750902557
this is why i suggested to call that on startup too.

## [18] src/server.c:3214 author=chenyang8094 reply=true subj=line side=RIGHT cid=750915534
Modified, `delHistoryAofFiles` will be called in three places:
1. When the redis server starts
2. When AOFRW successfully finish
3. When `config set aof-enable-auto-gc yes`

## [175] src/server.c:3611 author=sundb reply=false subj=line side=RIGHT cid=772173860
Here will crash in sentinel mode, it does not call `aofLoadManifestFromDisk`.

## [8] src/server.c:4978 author=oranagra reply=false subj=line side=LEFT cid=750433406
i wonder if we want to keep this field and just transmit 0 (backwards compatibility concerns)?
let's discuss...
p.s. if we don't (or even if we do), let's list this in the top comment's "interface changes section".

## [13] src/server.c:4978 author=chenyang8094 reply=true subj=line side=LEFT cid=750805412
Indeed, maybe some users' monitoring systems rely on this info field, so I keep it and force it to 0.

## [123] src/server.c:4978 author=chenyang8094 reply=true subj=line side=LEFT cid=756884444
@oranagra @yossigo Do you have any suggestions here?

## [206] src/server.c:4978 author=oranagra reply=true subj=line side=LEFT cid=775208493
i concluded that it's ok to just strip down this info field.

## [207] src/server.c:4978 author=oranagra reply=true subj=line side=LEFT cid=775208776
ohh, i now see that since i wrote down this comment we've re-introduced that field and it's always set to 0 (and the top comment already indicates that).
did we decide that in some other comment?
@yossigo WDYT? keep a dead info field always set to 0, or trim it? (i'm leaning towards trimming it)

## [256] src/server.c:4978 author=yossigo reply=true subj=line side=LEFT cid=775403304
I think we can trim it. `INFO` is already free to omit fields depending on settings, so I guess clients should be used to not finding specific fields in some cases.

## [258] src/server.c:4978 author=oranagra reply=true subj=line side=LEFT cid=775455266
ok.. in this case it's a field that's unlikely to be used by clients (apps), just monitoring software.
@chenyang8094 please trim it (again), and update the top comment.

## [9] src/server.h:1302 author=oranagra reply=false subj=line side=RIGHT cid=750441418
in the past, we attempted to avoid using PRId64 and instead just used `long long` (or `long long` casting).

p.s. why is this in server.h? i presume that aof.c is the only one that needs to be aware of these details (including some of the defines above).

## [44] src/server.h:1524 author=oranagra reply=false subj=line side=RIGHT cid=751430918
so is `aof_current_size` the size of all incremental (non history) parts? or also including the base?
let's make the comment clearer.
also maybe rename `aof_newfile_size` to `aof_last_incr_size`? ("new" may be ambiguous)

## [49] src/server.h:1524 author=chenyang8094 reply=true subj=line side=RIGHT cid=751784435
Yes, `aof_newfile_size` only means the last "that is, newly created " INCR AOF,  Its main purpose is to provide the correct position when ftruncate this AOF  file.

I think `aof_last_incr_size` is indeed clearer, modified.

## [37] src/server.h:2396 author=oranagra reply=false subj=line side=RIGHT cid=751320314
let's attempt to have the first word in "API" functions (specifically the non-static ones that are declared in server.h and used in many files), represent the module they belong to.
i.e. these should all start with `aof`, i.e. `aofDelHistoryFiles`.
there is some attempt to do that in redis, and although inconsistent, i think we should try to follow it in new code.

## [335] src/util.c:828 author=yossigo reply=false subj=line side=RIGHT cid=776838066
Suggest to move this check out of this function, as it seems like a generic create-if-not-exists utility function.

## [336] src/util.c:833 author=yossigo reply=false subj=line side=RIGHT cid=776838560
```suggestion
            errno = ENOTDIR;
            return -1;
```

## [171] src/util.c:882 author=oranagra reply=false subj=line side=RIGHT cid=770526837
depending on how often this will be used, maybe we should pre-allocate the `sdsnew` to be reasonably big, so we don't re-allocate it twice in sdscat.
i.e. maybe do `sdsnewlen(SDS_NOINIT)`.

same can be done in dirRemove

## [224] tests/integration/aof-multi-part.tcl:50 author=oranagra reply=false subj=line side=RIGHT cid=775232044
i see many of the tests below are missing some assertion to check that we successfully hit the scenario we aimed for.
i.e. match some log message of the expected failure.

## [225] tests/integration/aof-multi-part.tcl:81 author=oranagra reply=false subj=line side=RIGHT cid=775232825
maybe it'll be better if each of these tests is in a test scope.
like so:
```tcl
test {name / description} {
    create_aof_dir ...
    create_aof_manifest ....

    start_server_aof [list dir $server_path] {
        wait_for_condition 100 50 {
            ! [is_alive $srv]
        } else {
            fail "AOF loading didn't fail"
        }
        assert_something ...
    }

    clean_aof_persistence $aof_dirpath
}
```
i.e. the server is started inside the test and not vice versa.
p.s. this way if the test has some skip tag, the server setup and creation is skipped too

## [302] tests/integration/aof-multi-part.tcl:84 author=oranagra reply=false subj=line side=RIGHT cid=775886149
maybe now you can let go of the numbering (can get out of sync in with future edits) and even most titles (i.e. when the test name has the exact same info)

## [226] tests/integration/aof-multi-part.tcl:424 author=oranagra reply=false subj=line side=RIGHT cid=775233218
do we also have a test that attempts to load an old preamble-rdb file?
i.e. put one in the assets folder and attempt to upgrade from that.

## [227] tests/integration/aof-multi-part.tcl:430 author=oranagra reply=false subj=line side=RIGHT cid=775233394
can't we use `set client [redis_client]`?

## [265] tests/integration/aof-multi-part.tcl:430 author=chenyang8094 reply=true subj=line side=RIGHT cid=775707658
No, will report err:
```
Executing test client: key "host" not known in dictionary.
key "host" not known in dictionary
    while executing
"dict get $srv $property"
```

## [228] tests/integration/aof-multi-part.tcl:454 author=oranagra reply=false subj=line side=RIGHT cid=775233637
that's a long sleep.
maybe this was copied from somewhere, so i'd like to use this opportunity to promote this into a utility function.
it can use shorter sleeps (10ms), and do the verbose puts only in one of 100 iterations

## [303] tests/integration/aof-multi-part.tcl:469 author=oranagra reply=false subj=line side=RIGHT cid=775889432
why is this at the end of the previous test and not the beginning of the next one?

## [307] tests/integration/aof-multi-part.tcl:482 author=oranagra reply=false subj=line side=RIGHT cid=775897321
i didn't bother to download and look at the binary file, so i don't know what's in it.
i assume some of these keys are part of the rdb header, and some are part of the AOF tail?
is that true? maybe add a comment here.

## [312] tests/integration/aof-multi-part.tcl:482 author=chenyang8094 reply=true subj=line side=RIGHT cid=775905163
all k1 k2 and k3 in rdb header， do we need rdb header and aof tail?

## [316] tests/integration/aof-multi-part.tcl:482 author=oranagra reply=true subj=line side=RIGHT cid=775907588
yes, i wanna make sure we can still handle such files (since we no longer generate them)

## [229] tests/integration/aof-multi-part.tcl:487 author=oranagra reply=false subj=line side=RIGHT cid=775234037
let's add some comment that we synthetically create a layout of an interrupted upgrade (interrupted before the rename).
and even describe that it's a folder containing manifest pointing to a missing file, where e file is still outside the folder.

## [230] tests/integration/aof-multi-part.tcl:570 author=oranagra reply=false subj=line side=RIGHT cid=775234483
let's add an assertion for some log message here too (all tests that expects the server to fail starting)

## [231] tests/integration/aof-multi-part.tcl:634 author=oranagra reply=false subj=line side=RIGHT cid=775234957
i think it would be better if the two servers are nested (running at the same time).
```tcl
test {name} {
    setup code....
    start_server 1 {
        start_server 2 {
            all the checks referring to [r -1 command] and [r 0 command]
        }
    }
}
```

## [232] tests/integration/aof-multi-part.tcl:731 author=oranagra reply=false subj=line side=RIGHT cid=775236162
that's a lot of time, let's increase the check interval (`wait_for_condition 1000 10` should be ok, right?)

## [233] tests/integration/aof-multi-part.tcl:776 author=oranagra reply=false subj=line side=RIGHT cid=775236426
maybe use this opportunity to check that the temp files are deleted?
(by counting the number of files in the folder)

## [234] tests/integration/aof-multi-part.tcl:834 author=oranagra reply=false subj=line side=RIGHT cid=775236875
```suggestion
        test "AOF rewrite doesn't open new aof when AOF turn off" {
```

## [235] tests/integration/aof-multi-part.tcl:922 author=oranagra reply=false subj=line side=RIGHT cid=775237242
```suggestion
        test "AOF can produce consecutive sequence number after reload" {
```

## [236] tests/integration/aof-multi-part.tcl:973 author=oranagra reply=false subj=line side=RIGHT cid=775237627
1. let's make sure that all of the above steps got to complete while the `rdb_bgsave_in_progress` is still 1.
2. setting the key-save-delay will not cause the fork to detect it, and we'll still have to wait for it to complete. i guess we have to kill it to proceed.

## [237] tests/integration/aof-multi-part.tcl:975 author=oranagra reply=false subj=line side=RIGHT cid=775237762
let's change the constant sleep with a wait_for_condition.
we can look at the `aof_rewrite_scheduled` INFO field (i.e. assert that it's 1 before, and then wait for it to go to 0 before doing `waitForBgrewriteaof`

## [238] tests/integration/aof-multi-part.tcl:1023 author=oranagra reply=false subj=line side=RIGHT cid=775238110
i'd like to increase the interval (10ms).
the rewrite check in serverCron happens every tick.

## [240] tests/integration/aof-multi-part.tcl:1027 author=oranagra reply=false subj=line side=RIGHT cid=775238900
maybe we can also assert to check that there's no `aof_rewrite_in_progress`?

## [304] tests/integration/aof-multi-part.tcl:1027 author=oranagra reply=false subj=line side=RIGHT cid=775896369
in case it's very fast, we won't get to see it turn to `1`.
i think it's ok to:
1. see the scheduled flag turn off.
2. waitForBgrewriteaof

if for some reason that's not enough, we can wait for `[s total_forks]` to get incremented.

## [239] tests/integration/aof-multi-part.tcl:1030 author=oranagra reply=false subj=line side=RIGHT cid=775238701
the lowest throttle is 1 minute, right?
and in the absence of a throttle, redis would have started a new rewrite every 10 ms, right?
so maybe we can have a shorter sleep?

## [241] tests/integration/aof-multi-part.tcl:1042 author=oranagra reply=false subj=line side=RIGHT cid=775238976
maybe assert here that aof_rewrite_in_progress is 0 too?
i.e. just to make it clear that the next bgrewriteaof is gonna be the one that succeeds.

## [305] tests/integration/aof-multi-part.tcl:1079 author=oranagra reply=false subj=line side=RIGHT cid=775896501
please mention what are we waiting for to happen.

## [306] tests/integration/aof-multi-part.tcl:1099 author=oranagra reply=false subj=line side=RIGHT cid=775896800
the wait_for_condition will fail, there's no need for the assertion

## [216] tests/integration/aof-race.tcl:1 author=oranagra reply=false subj=line side=RIGHT cid=775224918
another case where i'm not clear as to why we need to override default configs with the same value

## [277] tests/integration/aof-race.tcl:1 author=chenyang8094 reply=true subj=line side=RIGHT cid=775738661
Just imitate the previous style {appendonly {yes} appendfilename {appendonly.aof}

## [286] tests/integration/aof-race.tcl:1 author=oranagra reply=true subj=line side=RIGHT cid=775790602
i think we better drop both, it's long to read and maybe slightly confusing.
also since the defaults are never gonna change, even if we refer to these in the code, i'm not sure we should bother to define them.

## [287] tests/integration/aof-race.tcl:1 author=chenyang8094 reply=true subj=line side=RIGHT cid=775792769
Need to remove all the default value override in the existing test?

## [217] tests/integration/aof-race.tcl:7 author=oranagra reply=false subj=line side=RIGHT cid=775224952
why are all of these needed here? AFAICT they're unused. am i missing anything?

## [218] tests/integration/aof.tcl:240 author=oranagra reply=false subj=line side=RIGHT cid=775225503
same question about default configs (here and in the test below)

## [276] tests/integration/aof.tcl:240 author=chenyang8094 reply=true subj=line side=RIGHT cid=775738505
Just imitate the previous style` {appendonly {yes} appendfilename {appendonly.aof}` 

## [219] tests/integration/aof.tcl:268 author=oranagra reply=false subj=line side=RIGHT cid=775225604
that's the same pattern we used in expire.tcl which i suggested to extract to a utility function, right?
same for the test just below, and another one further down.

## [220] tests/integration/aof.tcl:521 author=oranagra reply=false subj=line side=RIGHT cid=775225928
maybe we should have `create_aof` implicitly create the folder when needed?
maybe we could pass it an optional argument to of a string to put in the manifest? or maybe just an optional boolean, in case the manifest content is the same in all the tests that do this?

## [221] tests/integration/aof.tcl:527 author=oranagra reply=false subj=line side=RIGHT cid=775225958
same question about default configs.. why do we need to override them?

## [278] tests/integration/aof.tcl:527 author=chenyang8094 reply=true subj=line side=RIGHT cid=775738687
Just imitate the previous style {appendonly {yes} appendfilename {appendonly.aof}

## [215] tests/unit/aofrw.tcl:47 author=oranagra reply=false subj=line side=RIGHT cid=775219491
why was that sleep needed?
if there's some timing issue, i'd rather add some `wait_for` and finish sooner rather than add another 1 second delay here.
also, if it is really needed, let's add a comment why.

## [213] tests/unit/expire.tcl:311 author=oranagra reply=false subj=line side=RIGHT cid=775219093
isn't that the default? why do we provide file name and dir name here?
just to be explicit since the test refers to them?
maybe instead we should make the test use CONFIG GET?

## [260] tests/unit/expire.tcl:311 author=chenyang8094 reply=true subj=line side=RIGHT cid=775472980
Here I refer to the previous style. Maybe when someone will change the default value in the future?

## [214] tests/unit/expire.tcl:321 author=oranagra reply=false subj=line side=RIGHT cid=775219356
maybe we should put this in utils.tclor in aofmanifest.tcl? (using CONFIG GET)
i imagine there would be other tests that want to access the last aof file.

## [212] tests/unit/other.tcl:130 author=oranagra reply=false subj=line side=RIGHT cid=775218990
why is this reorder needed? the database is empty during this rewrite anyway.
