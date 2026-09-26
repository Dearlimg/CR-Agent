# digest gin-2767 : 11 anchored candidates (of 14 total)

## [6] .gitignore:7 author=rw-access reply=false subj=line side=RIGHT cid=663160428
restore newline
```suggestion
tmp.out

```

## [3] .gitignore:8 author=qm012 reply=true subj=line side=RIGHT cid=659415807
thanks for your review,has been reset,can you tell me why? please I'll remember (same file: .vscode)

## [0] tree.go:413 author=rw-access reply=false subj=line side=RIGHT cid=659322066
these names are a little too short and cryptic for my taste. mind making them longer? (i.e. `recordIndex`)

## [2] tree.go:413 author=qm012 reply=true subj=line side=RIGHT cid=659414745
thanks for your review, more comments have been added.

## [5] tree.go:466 author=rw-access reply=false subj=line side=RIGHT cid=663154654
my personal preference is to keep the parentheses, as they make it more clear you're assigning a boolean expression
```suggestion
					value.tsr = (path == "/" && n.handlers != nil)
```

## [8] tree.go:466 author=qm012 reply=true subj=line side=RIGHT cid=663359292
Thank you for your review, I see what you mean, but for a obsessive-compulsive disorder patients, in fact the parentheses is invalid and can be ignored, just had prompt effect (but not enough concise), if the judge conditions for more than two or more, or is too long, can adopt the way of line, I'm sorry, I want to insist on my opinion.

## [11] tree.go:466 author=qm012 reply=true subj=line side=RIGHT cid=663596584
#### There will also be differences in style
https://github.com/gin-gonic/gin/blob/9c27053243cb24ecc90d01c8ff379bd98fed9c8e/tree.go#L502

#### It makes sense under conditions like this

```go
var condition = (a &b) || 
                       (c && d)
```
https://github.com/gin-gonic/gin/blob/9c27053243cb24ecc90d01c8ff379bd98fed9c8e/tree.go#L558



## [12] tree.go:570 author=rw-access reply=false subj=line side=RIGHT cid=664093314
```suggestion
			// level 2 router not found and latestNode.wildChild is true
```

## [4] tree_test.go:162 author=rw-access reply=false subj=line side=RIGHT cid=663143713
can you add more routes with multiple params?
I also want to confirm something like this:
```
/something/:paramname/thirdthing
/something/secondthing/test
```

If you someone does `GET /something/secondthing/thirdthing` that should have a 404. because `GET /something/secondthing` is explicitly created, it gets its own tree and won't ever fall back to `/something/:paramname`

## [9] tree_test.go:162 author=qm012 reply=true subj=line side=RIGHT cid=663359520
Thank you for your review. I need some time to do the test. The final result should be 404 is correct

## [10] tree_test.go:162 author=qm012 reply=true subj=line side=RIGHT cid=663461645
hi @rw-access 
1. Added more unit tests
2. This scene I tested ` GET/something/secondthing thirdthing ` matching ` / something / : paramname/thirdthing ` is correct
    When n.indices matches a child node, 'line 271 n.hildren [len(n.hildren)-1]' will be executed if it does not
```
`GET /something/secondthing/abc` 404
`GET /something/secondthing/ff` 404
```
<details>
<summary>tree_node.png</summary>
     <div>
        <img src="https://user-images.githubusercontent.com/67568757/124377236-96c04c00-dcdd-11eb-931b-fc3b6f9b4fc7.png">
    </div>
</details>
