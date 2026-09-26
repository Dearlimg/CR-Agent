# digest gin-2632 : 8 anchored candidates (of 12 total)

## [2] README.md:2155 author=thinkerou reply=false subj=line side=RIGHT cid=571749253
should remove some unused tab/space, thanks!

## [3] context.go:756 author=thinkerou reply=false subj=line side=RIGHT cid=571750863
```
			if !cidr.Contains(remoteIP) {
                             continue
                         }
			for _, headerName := range c.engine.RemoteIPHeaders {
				ip, valid := validateHeader(c.requestHeader(headerName))
				if valid {
					return ip
				}
			}
```
suggestion, thanks!

## [6] context.go:756 author=manucorporat reply=true subj=line side=RIGHT cid=572035605
refactored it a little bit, review again... now thee logic is better separated between validating the trusted proxy and parsing the header, also a nw AP

```
ip, trusted = c.RemoteIP()
```

allows to even implement your own logic, or trust othe headers that might not be even related with IP!

## [11] context.go:756 author=agmt reply=true subj=line side=RIGHT cid=694643333
Do you expect that HTTP proxy running on `c.RemoteIP()` resets `X-Forwarded-For`? Because if it appends, then we can't inherit trustiness of `c.RemoteIP()` to all other proxies.

## [10] context_test.go:1433 author=agmt reply=false subj=line side=RIGHT cid=694639407
If we trust proxy `40.40.40.40`, but not trust `30.30.30.30` (proxy it is or not), then ClientIP should be `30.30.30.30` as `20.20.20.20` was set by somebody untrusted.
Please do not forget that `X-Forwarded-For` is appended, so it should be processed right-to-left:
```
right-most IP address is the IP address of the most recent proxy and the left-most IP address is the IP address of the originating client.
```

## [0] gin.go:332 author=austinheap reply=false subj=line side=RIGHT cid=571725830
Seems worth applying some regex magic here?

## [1] gin.go:332 author=javierprovecho reply=true subj=line side=RIGHT cid=571726255
Maybe there are more corner cases that we originally thought. 😅 

self-note: ipv6 compatibility (?)

## [8] gin.go:332 author=javierprovecho reply=true subj=line side=RIGHT cid=573373500
@austinheap solved at https://github.com/gin-gonic/gin/pull/2632/commits/7e649c347fed4f4322d5a3eb893b8eff17c6d843
