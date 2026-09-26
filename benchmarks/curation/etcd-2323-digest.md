# digest etcd-2323 : 73 anchored candidates (of 120 total)

## [18] pkg/transport/timeout_transport.go:33 author=xiang90 reply=false subj=line side=RIGHT cid=24785707
do we really need to make this configurable? 


## [39] pkg/transport/timeout_transport.go:33 author=yichengq reply=false subj=line side=RIGHT cid=24791397
hrm.. considering this is a general util package, i would say yes.


## [109] rafthttp/http.go:121 author=xiang90 reply=false subj=line side=RIGHT cid=25125127
ignored unexpected streaming request path %s


## [110] rafthttp/http.go:121 author=xiang90 reply=false subj=line side=RIGHT cid=25125135
this kind of logging message is not very helpful. we, at lease, should explicitly tell when it cannot be parsed and why 


## [104] rafthttp/http.go:122 author=yichengq reply=false subj=line side=RIGHT cid=25119776
Could you illustrate this?
i think strings.contains may miss some case if substring appears in other part in the string.


## [15] rafthttp/msgapp.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24784738
return the encoder interface if you really want to hide the implementation.


## [46] rafthttp/msgapp.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24953978
msgAppEncoder is a optimized encoder for append messages. It ...


## [80] rafthttp/msgapp.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25046135
Why not here? it is only used in this encoder and decoder.


## [89] rafthttp/msgapp.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25099400
Add TODO: move the fs stats and use new metrics


## [90] rafthttp/msgapp.go:27 author=xiang90 reply=false subj=line side=RIGHT cid=25099474
It MUST be used with a paired msgAppDecoder.


## [9] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24784272
change the endpoints to
/stream/all and /stream/apppend

so we do not need to demultiplexing here.

also i am wondering how will this affect the backward support.


## [12] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24784456
this is wired: all msg type can update msgApp decorder.


## [17] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24784944
this is not a reader as far as i can see. no one is reading from it. it receives message from remote and send it to raft.
it is more like a msgApp server. the writer is its the client.


## [19] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24785774
what is this? shouldn't this be the id of the peer? so just id?


## [20] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24785848
why not combine the startPeer and run loop? So we do not really need to keep fields like dialer inside the struct.


## [21] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24785861
a lot of stuff's ownership should be just the run loop.


## [22] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24785904
shouldn't we do this inside the peer run loop?


## [33] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24790843
yep. good point.
i can redirect unrecognizable one to use the /stream/append handler for backward compatibility. Sound good?


## [34] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24790960
i don't quite follow.
maybe only letting msgapp or msgheartbeat update it is better. These messages indicate that the leader is communicating with me.


## [35] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24791166
@xiang90 Do you think it is reasonable to drop the received message here if the buffer is full?
The buffer size is big enough for 1 second gap.
Another solution is to let receiver wait there until the message is consumed.
When this happens, the most possible reason is that CPU is fully utilized.


## [38] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24791353
It reads from the remote by itself. :wink: 
Yep, it behaves much like server-client mode now, considering they use channel to communicate now.
I would stay for a while(of course, before merging it) to see whether it is the best naming.


## [42] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24791499
the run loop is designed as non-blocking. and the msgAppWriter.attchOutgoingConn and writer.attachoOutgoigConn are blocking function. so I don't let it go into the run loop.


## [47] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=24954461
we can have a generic picker so we do not need to leak the extra picking logic.


## [53] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=24970771
p.urlc? p.newURL sounds like a good function name.


## [74] rafthttp/peer.go:22 author=yichengq reply=false subj=line side=RIGHT cid=25045026
i am gonna make the type-specific logic in streamWriter.
picker will become a general stuff, and it will do nothing except pick. gonna combine it with peer.


## [91] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=25099685
a peer is a remote raft node. local raft node sends message over peer.
a peer has two underlying mechanism to send out a message: stream and pipeline.
A stream is ... Peer also has a optimized stream for sending msgApp since...
A pipeline is ... 


## [92] rafthttp/peer.go:22 author=xiang90 reply=false subj=line side=RIGHT cid=25099729
// for testing
pausec chan struct{}
resumec chan struct{}


## [96] rafthttp/peer.go:89 author=xiang90 reply=false subj=line side=RIGHT cid=25104060
i am thinking if we can return something better. not big issue


## [97] rafthttp/peer.go:97 author=xiang90 reply=false subj=line side=RIGHT cid=25104173
this might be one reason you actually want the raft message go through peer?


## [105] rafthttp/peer.go:97 author=yichengq reply=false subj=line side=RIGHT cid=25120477
yep. that is the original design. To do it, i can make writer close the underlying connection if higher-term msgapp comes.


## [0] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24783837
why do we need this? cant we let encodeTo return an error?


## [1] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24783948
should not this be an empty raft heartbeat message?


## [2] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24783960
what does this mean to a user who consumes the log?


## [3] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24783982
why do not attach the reset method to the streamWriter?


## [4] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24784068
put a empty line between working and the chans.


## [5] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24784170
after looking at the code, i found the only necessary interface is encode. 

we should create a new encoder every time the "info" changed. also i do not think it is clear to let a encoder take a http.Header named as info and the function is actually called loadMetadata.


## [8] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24784222
should not this return a streamer? or why do we call it streamDailer?


## [14] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24784585
change this to a chan. it is better to use chan to do communication rather than a func call in go.


## [23] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790467
It is used to check whether the message can be encoded by the encoder. It is called when deciding which one should take charge of sending this message, which happens before encodeTo. This is a non-blocking function, so it will not block the decision process.


## [24] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790523
It is used for HTTP connection heartbeat, not raft heartbeat. They are in different levels.
Current I use the big-endian zero to indicate the heartbeat.
raft heartbeat has the restriction that `decodeFrom` needs to understand the raft heartbeat too, which is not the case for msgApp stream.


## [25] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790566
It can see that the message is dropped intentionally by rafthttp.
But if that happens, that means they are messages for previous terms, so the message is quite useless. I will delete the log.


## [26] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790645
iirc, you mean that add a `func (cw *streamWriter) reset()`.
reset is a helper function to manage the working connection and channel vars used in the run loop.  Making it a function will make it more complicated IMO.


## [28] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24790672
it is not the responsibility of encoder.


## [29] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24790693
it does not matter. it simplifies the code.


## [30] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790788
I think this idea is good. I will try it in this way.
a `encoderFactory` that creates a `encoder` each time the information is updated.


## [31] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24790800
i call it this way because it dials to a stream endpoint in the way to build a stream.


## [32] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24790833
please drop the factory name... just new 


## [36] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24791262
recvFunc uses channel inside to do communication.
I use recvFunc here because the same logic is also used at https://github.com/coreos/etcd/pull/2323/files#diff-288ade94a6578e3a03cc7336398b3565R98
I understand a channel makes it more golang style, but it does harm by increasing the duplicated code here.


## [43] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=24791632
they are different things. For msgApp stream connection, it is only used to send msgApp. It is weird to send msgHeartbeat through it.


## [44] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24794558
i actually do not care as long as you do not write some random uint64 through the network. 

Define the application heartbeat protobuf message at least if you want. 


## [51] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24955567
move this logic out of the function and call it stop?


## [52] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24970565
empty line between msgAppTerm and the two writers


## [56] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24970891
move this two interfaces out of the stream.go


## [57] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24970921
linkHeartbeatMsg // a link heartbeat message is ...


## [59] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24971012
rename this to streamController which implements the picker interface


## [61] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24971073
how about returning an error instead of closing the conn in this func?


## [65] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24971423
why do we need the dialer? can we just do the dial each time with the information? 


## [68] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=24971609
comment // do nothing for heartbeat, TODO: record the recent activity of remote peer


## [75] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25045433
hrm.. i think stream is clear enough and simpler.


## [76] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25045814
Both are fine. I prefer this one because it is easy to think that the function takes the ownership of connection from now on.


## [79] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25046050
i cannot follow on this one. it points out what bad is happening in rafthttp.


## [82] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25048876
what does buffer is blocked means to a end-user?


## [84] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25049316
it means that rafthttp is doing bad things.
It is much like debug log. We are mixing these two things together now, and gonna separate them.


## [98] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25105024
well... i do not quite like this function closure. can we make it a method on streamWriter?


## [100] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25105117
rafthttp: failed to heartbeat on stream %d. restarting a new stream...


## [101] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25105190
rafthttp: failed to send message on stream %d. restarting a new stream...


## [103] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25106084
can we put decode into another long running go-routine and it can greatly simplify the code logic.


## [106] rafthttp/stream.go:3 author=yichengq reply=false subj=line side=RIGHT cid=25120516
lots of variables are defined in run loop region. putting them into the struct looks verbosal. i don't know which one is better. any thoughts?


## [112] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25125173
basically, you want to use a method on a receiver to implement an interface or avoid name collisions. When you are not sure or it creates inconvenience, use normal function calls.  


## [114] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25125210
``` go
for {
rd := cr.dail()
cr.decodeLoop(rd)
...
}
```


## [116] rafthttp/stream.go:3 author=xiang90 reply=false subj=line side=RIGHT cid=25125225
a empty line between islinkhearbeatmessage and the default case? it would be more clear


## [81] rafthttp/stream.go:140 author=yichengq reply=false subj=line side=RIGHT cid=25046374
it is a writer/server. you give connection to it, and it will manage it for you then. Let me do the change we discussed first, which makes it clear.


## [115] rafthttp/stream.go:240 author=xiang90 reply=false subj=line side=RIGHT cid=25125220
why not return the error and let the outside to parse the error?

