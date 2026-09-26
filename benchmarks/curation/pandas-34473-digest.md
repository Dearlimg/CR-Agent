# digest pandas-34473 : 74 anchored candidates (of 97 total)

## [95] asv_bench/benchmarks/io/json.py:87 author=arw2019 reply=false subj=line side=RIGHT cid=443817568
@jreback Not sure where we want the `longints` to go - whether into `ints` or separately. I would think we also want to make sure we have both positive and negative long ints in there?

## [59] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1114 author=WillAyd reply=false subj=line side=RIGHT cid=442408578
```suggestion
            Buffer_Reserve(enc, szlen);
```

This might be your outstanding issue. This macro reserves a much larger buffer than we know we need here

## [0] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=WillAyd reply=false subj=line side=RIGHT cid=434706498
The implementation here should be pretty similar to the UTF8 case but remove the `Buffer_AppendCharUnchecked(enc, '\"');` lines. You also don't need the `enc->forceASCII` check and can just do `1Buffer_EscapeStringUnvalidated`

## [2] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=WillAyd reply=true subj=line side=RIGHT cid=434715017
I think you will also need to define a function pointer for a callback that converts the object to a string representation

See:

https://github.com/pandas-dev/pandas/blob/e79487d8e50a03be87188608cd1379330a4fd224/pandas/_libs/src/ujson/python/objToJSON.c#L2116

And 

https://github.com/pandas-dev/pandas/blob/e79487d8e50a03be87188608cd1379330a4fd224/pandas/_libs/src/ujson/python/objToJSON.c#L1659

As examples

## [37] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=arw2019 reply=true subj=line side=RIGHT cid=441961279
So this works up to line 1112, in that
``` C++
 value = enc->getBigNumStringValue(obj, &tc, &szlen);
```
gets the correct string (I checked with a print statement). But there's still a problem because we exit with an `OverflowError` via this line (checked with a print statement)
https://github.com/pandas-dev/pandas/blob/5d69786af46291abe727b290a84aa9d2663cd50c/pandas/_libs/src/ujson/python/objToJSON.c#L2289
I feel like this something to do with the buffer and the length of the string we're printing out somehow not being right...?

## [38] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=WillAyd reply=true subj=line side=RIGHT cid=441965192
This goes back to L1636. CPython manages a global error and we never explicitly clear it on that line. You could all `PyErr_Clear()` but it actually would be better if you used `PyLong_AsLongLongAndOverflow` and checked for a non-zero overflow

https://docs.python.org/3/c-api/long.html#c.PyLong_AsLongLongAndOverflow

## [39] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=arw2019 reply=true subj=line side=RIGHT cid=441988211
Ok! I'll go with `PyLong_AsLongLongAndOverflow`.

Quick question, though: when I do
``` C++
        int *_overflow = 0;
        GET_TC(tc)->longValue = PyLong_AsLongLongAndOverflow(obj, _overflow);
```
I'm making a mistake (there's a segfault) - but not sure why

## [40] pandas/_libs/src/ujson/lib/ultrajsonenc.c:1119 author=WillAyd reply=true subj=line side=RIGHT cid=442244800
The way it is written you are telling the program to write the overflow result to memory location 0, which the program almost assuredly doesn't have access to (hence segfault)

Just declare the int and use the address of operator

```c
int overflow;
GET_TC(tc)->longValue = PyLong_AsLongLongAndOverflow(obj, &overflow);
```

## [1] pandas/_libs/src/ujson/python/objToJSON.c:109 author=WillAyd reply=false subj=line side=RIGHT cid=434706919
You would want a pointer to a char here, but it's probably easiest to not add another struct member and just write to `cStr` for now

## [81] pandas/_libs/src/ujson/python/objToJSON.c:122 author=WillAyd reply=false subj=line side=RIGHT cid=442886769
minor nit but can you revert any added / removed blank lines in the PR? Should minimize the diff a bit more

## [34] pandas/_libs/src/ujson/python/objToJSON.c:139 author=WillAyd reply=false subj=line side=RIGHT cid=441848946
Wherever you declare this object to be in this struct needs to match the subsequent definition. So if you place this here then down on L2199 need to make sure it appears in the same position when defining

## [47] pandas/_libs/src/ujson/python/objToJSON.c:1632 author=WillAyd reply=false subj=line side=RIGHT cid=442356287
```suggestion
        int overflow = 0;
```

No need to prepend an underscore here; may be confusing to authors who expect that to mean something

## [46] pandas/_libs/src/ujson/python/objToJSON.c:1634 author=WillAyd reply=false subj=line side=RIGHT cid=442356056
```suggestion
        exc = GET_TC(tc)->LongValue == -1 && PyErr_Occurred();
```

Then can simplify condition on next line to just if `(_overflow && !exc)`

## [50] pandas/_libs/src/ujson/python/objToJSON.c:1634 author=WillAyd reply=false subj=line side=RIGHT cid=442385931
```suggestion
        int err;
```

Should just stick with int for now; would need a larger discussion around using C99 types (probably OK but not sure what Windows platforms support)

## [51] pandas/_libs/src/ujson/python/objToJSON.c:1634 author=WillAyd reply=true subj=line side=RIGHT cid=442386268
Though you can also just use `exc`? I don't think need a new variable here

## [56] pandas/_libs/src/ujson/python/objToJSON.c:1634 author=arw2019 reply=true subj=line side=RIGHT cid=442393309
I changed it because using `exc` was throwing an error:
```
pandas/_libs/src/ujson/python/objToJSON.c:1634:13: error: assignment to ‘PyObject *’ {aka ‘struct _object *’} from ‘int’ makes pointer from integer without a cast [-Werror=int-conversion]
         exc = (GET_TC(tc)->longValue == -1) && PyErr_Occurred();
             ^
```

## [57] pandas/_libs/src/ujson/python/objToJSON.c:1634 author=WillAyd reply=true subj=line side=RIGHT cid=442394615
I think you need `&& (PyErr_Occurred() != NULL)` as that function returns a PyObject pointer

## [41] pandas/_libs/src/ujson/python/objToJSON.c:1636 author=WillAyd reply=false subj=line side=RIGHT cid=442347635
I could be wrong but I don’t think you need the error check here

## [42] pandas/_libs/src/ujson/python/objToJSON.c:1636 author=WillAyd reply=true subj=line side=RIGHT cid=442349022
Nevermind i think this is ok. 

For performance though I think should check that the return value is -1 first before checking for an error, as the latter can be expensive so good to short circuit

## [43] pandas/_libs/src/ujson/python/objToJSON.c:1636 author=arw2019 reply=true subj=line side=RIGHT cid=442352792
Ok! Added that. Does seem to work without the error check also though

## [49] pandas/_libs/src/ujson/python/objToJSON.c:1637 author=WillAyd reply=false subj=line side=RIGHT cid=442385452
```suggestion
        if (overflow){
```

I think from the docstring overflow would only get set when there is no error, so don't need both

## [61] pandas/_libs/src/ujson/python/objToJSON.c:1638 author=WillAyd reply=false subj=line side=RIGHT cid=442435964
I think we do need to add a `PyErr_Clear();` within this branch to prevent this from incorrectly propogating an OverflowError (sorry think I mistakenly asked you to remove before)

## [64] pandas/_libs/src/ujson/python/objToJSON.c:1638 author=arw2019 reply=true subj=line side=RIGHT cid=442582102
I think that - if I understand the docstring correctly - `PyLong_AsLongLongAndOverflow` doesn't raise an OverflowError (but will raise an error if there's a problem other than Overflow)

One thing I know is this runs ok (including tests) without the `PyErr_Clear()` though I guess it doesn't hurt to add that in?

## [66] pandas/_libs/src/ujson/python/objToJSON.c:1638 author=WillAyd reply=true subj=line side=RIGHT cid=442585143
Yea I think you are right. Let’s keep this as is

## [6] pandas/_libs/src/ujson/python/objToJSON.c:1639 author=WillAyd reply=false subj=line side=RIGHT cid=435331391
Do this in the `Object_getBigNumStringValue` function you have defined

## [72] pandas/_libs/src/ujson/python/objToJSON.c:2115 author=arw2019 reply=false subj=line side=RIGHT cid=442621135
@WillAyd This looks like it frees up `cStr`

## [73] pandas/_libs/src/ujson/python/objToJSON.c:2115 author=WillAyd reply=true subj=line side=RIGHT cid=442624853
I don't think that is getting hit here. If it would then you would be double freeing those bytes and get a segfault anyway

## [74] pandas/_libs/src/ujson/python/objToJSON.c:2115 author=arw2019 reply=true subj=line side=RIGHT cid=442626488
Sorry I meant 
``` C
PyObject_Free(GET_TC(tc)->cStr);
```
I think `Object_endTypeContext` gets executed in 
https://github.com/pandas-dev/pandas/blob/e6e08890cc8bd1b162b920cf5a526a433bab8b30/pandas/_libs/src/ujson/lib/ultrajsonenc.c#L1112

## [75] pandas/_libs/src/ujson/python/objToJSON.c:2115 author=WillAyd reply=true subj=line side=RIGHT cid=442633877
Can you check that the free on cStr gets hit there? If so then good to go

## [65] pandas/_libs/src/ujson/python/objToJSON.c:2117 author=arw2019 reply=false subj=line side=RIGHT cid=442583690
@WillAyd Reallocating `bytes` is an outstanding issue like we discussed a while back. I know we need to free it up after we're done writing the output - but within this function bytes is undeclared, I think

## [68] pandas/_libs/src/ujson/python/objToJSON.c:2117 author=WillAyd reply=true subj=line side=RIGHT cid=442585616
Bytes is assigned to cStr but you won’t free it in this branch. We need an extra branch that checks for the JT_BIGNUM type and frees cStr in that case

## [70] pandas/_libs/src/ujson/python/objToJSON.c:2117 author=arw2019 reply=false subj=line side=RIGHT cid=442616582
@WillAyd I know this isn't right (as is `ujson.encode` core dumps) but is it the right idea? If yes what should I look to fix

## [71] pandas/_libs/src/ujson/python/objToJSON.c:2117 author=WillAyd reply=true subj=line side=RIGHT cid=442619367
Just clear cStr - you don't need bigNum_bytes

## [14] pandas/_libs/src/ujson/python/objToJSON.c:2131 author=WillAyd reply=false subj=line side=RIGHT cid=436401679
I think should use Str instead of Repr here 

## [8] pandas/_libs/src/ujson/python/objToJSON.c:2132 author=WillAyd reply=false subj=line side=RIGHT cid=436305458
just FYI `obj` is a pointer to a PyObject

## [16] pandas/_libs/src/ujson/python/objToJSON.c:2132 author=WillAyd reply=false subj=line side=RIGHT cid=436401921
Make sure you Py_DECREF these objects when they are no longer needed or else this will leak memory

## [25] pandas/_libs/src/ujson/python/objToJSON.c:2132 author=WillAyd reply=false subj=line side=RIGHT cid=438225666
You don't need a new variable here. If required just cast outLen when passed to the PyUnicode_AsUTF8AndSize function

## [33] pandas/_libs/src/ujson/python/objToJSON.c:2132 author=arw2019 reply=false subj=line side=RIGHT cid=438428260
The compiler still complains here:
```
pandas/_libs/src/ujson/python/objToJSON.c:2132:53: error: expected expression before ‘<’ token
     const char *str = PyUnicode_AsUTF8AndSize(repr, <Py_ssize_t> *_outLen);
```

## [5] pandas/_libs/src/ujson/python/objToJSON.c:2133 author=WillAyd reply=false subj=line side=RIGHT cid=435331036
Yea you need to modify the outLen pointer to let the serializer know how many bytes it should write out

## [12] pandas/_libs/src/ujson/python/objToJSON.c:2133 author=WillAyd reply=false subj=line side=RIGHT cid=436401611
You will want PyUnicode_AsUTF8AndSize here. You can pass outLen as the second argument

## [21] pandas/_libs/src/ujson/python/objToJSON.c:2133 author=WillAyd reply=false subj=line side=RIGHT cid=437902865
This array is managed on the stack, so it automatically gets cleaned up when it goes out of scope, which isn’t what you want (and the reason for multiple memcpy calls)

Instead just malloc _outLen bytes and then memcpy to that, before assign that to cStr

## [22] pandas/_libs/src/ujson/python/objToJSON.c:2133 author=arw2019 reply=true subj=line side=RIGHT cid=437916708
 > Instead just malloc _outLen bytes and then memcpy to that, before assign that to cStr

Ok, great! I fixed that.

One thing I'm stuck on is converting from the argument of `PyUnicode_AsUTF8AndSize` (which has to be a `Py_ssize_t`) to `_outLen` (which is C `size_t`)...

Also, just to check: I do need dereference `_outLen` in the calls to `memcpy` and `malloc`?

## [30] pandas/_libs/src/ujson/python/objToJSON.c:2133 author=WillAyd reply=false subj=line side=RIGHT cid=438411949
```suggestion
    const char *str = PyUnicode_AsUTF8AndSize(repr, <Py_ssize_t> *_outLen);
```

## [13] pandas/_libs/src/ujson/python/objToJSON.c:2135 author=WillAyd reply=true subj=line side=RIGHT cid=436401657
Nothing has been allocated yet so shouldn’t be freeing here. I think this is undefined behavior and most likely would segfault 

## [19] pandas/_libs/src/ujson/python/objToJSON.c:2135 author=WillAyd reply=false subj=line side=RIGHT cid=437870883
If this object gets released would also free bytes, so need to memcpy first. 

Also just use Py_DECREF for these as simple enough to see lifecycle

## [20] pandas/_libs/src/ujson/python/objToJSON.c:2135 author=arw2019 reply=true subj=line side=RIGHT cid=437896904
Ok! Now I memcpy bytes into `tc->cStr` before freeing up `repr`, `str` and `bytes`.

Looking at `tc->cStr` I think it  gets freed-up in `Object_endTypeContext`:
https://github.com/pandas-dev/pandas/blob/c45e92c3956fd2638980ac46e6e93ec3b6cc7c52/pandas/_libs/src/ujson/python/objToJSON.c#L2109

## [23] pandas/_libs/src/ujson/python/objToJSON.c:2135 author=WillAyd reply=false subj=line side=RIGHT cid=438224302
```suggestion
    char* bytes = malloc(*_outLen);
```

Need to dereference not take the address of here

## [15] pandas/_libs/src/ujson/python/objToJSON.c:2136 author=WillAyd reply=false subj=line side=RIGHT cid=436401896
So to manage the state properly you should allocate storage for bytes and memcpy whatever PyUnicode_AsUTF8AndSize returns to it. You will need to then free that memory in the callback function for ObjectEnd

## [24] pandas/_libs/src/ujson/python/objToJSON.c:2136 author=WillAyd reply=false subj=line side=RIGHT cid=438224693
```suggestion
    memcpy(bytes, str, *_outLen);
```

## [26] pandas/_libs/src/ujson/python/objToJSON.c:2137 author=WillAyd reply=false subj=line side=RIGHT cid=438226051
```suggestion
    GET_TC(tc)->cStr = bytes;
```

## [27] pandas/_libs/src/ujson/python/objToJSON.c:2139 author=WillAyd reply=false subj=line side=RIGHT cid=438226142
```suggestion
    Py_DECREF(repr);
```

## [77] pandas/_libs/src/ujson/python/objToJSON.c:2139 author=WillAyd reply=false subj=line side=RIGHT cid=442635466
```suggestion
    char* bytes = PyObject_Malloc(*_outLen + 1);
```
Since you noticed this is already getting freed in the context end, we should use the equivalent python malloc to match that

## [62] pandas/_libs/src/ujson/python/objToJSON.c:2140 author=WillAyd reply=false subj=line side=RIGHT cid=442478455
```suggestion
    char* bytes = malloc(*_outLen + 1);
```

## [29] pandas/_libs/src/ujson/python/objToJSON.c:2141 author=WillAyd reply=false subj=line side=RIGHT cid=438226465
We don't want to free here. This should go to the callback for the end of the type context for the object

## [63] pandas/_libs/src/ujson/python/objToJSON.c:2141 author=WillAyd reply=false subj=line side=RIGHT cid=442478547
```suggestion
    memcpy(bytes, str, *_outLen + 1);
```

## [58] pandas/_libs/src/ujson/python/objToJSON.c:2145 author=WillAyd reply=false subj=line side=RIGHT cid=442400748
```suggestion
```

This could cause strange behavior  (Py_DECREF is only for Python objects)

## [11] pandas/_libs/src/ujson/python/objToJSON.c:2194 author=arw2019 reply=false subj=line side=RIGHT cid=436323240
This line throws a compiler error:
```
pandas/_libs/src/ujson/python/objToJSON.c:2194:9: error: initialization of ‘JSINT32 (*)(void *, JSONTypeContext *)’ {aka ‘int (*)(void *, struct __JSONTypeContext *)’} from incompatible pointer type ‘const char * (*)(void *, JSONTypeContext *, size_t *)’ {aka ‘const char * (*)(void *, struct __JSONTypeContext *, long unsigned int *)’} [-Werror=incompatible-pointer-types]
```

## [17] pandas/_libs/src/ujson/python/objToJSON.c:2194 author=WillAyd reply=true subj=line side=RIGHT cid=436402041
You will need to update the PyEncoder struct to have the appropriate member definitions

## [32] pandas/_libs/src/ujson/python/objToJSON.c:2194 author=WillAyd reply=true subj=line side=RIGHT cid=438421861
If you can address this and the comment around the free call I _think_ things will work leaking some memory but can continue to address that

## [86] pandas/io/json/_json.py:725 author=WillAyd reply=false subj=line side=RIGHT cid=442981752
I think you might have an issue merging master - none of this should have changed

## [80] pandas/tests/io/json/test_pandas.py:1246 author=arw2019 reply=false subj=line side=RIGHT cid=442641428
@jreback This is the full integration test you asked for 

## [91] pandas/tests/io/json/test_pandas.py:1249 author=jreback reply=false subj=line side=RIGHT cid=443156841
can you use series here (and just df below)

## [78] pandas/tests/io/json/test_pandas.py:1259 author=WillAyd reply=false subj=line side=RIGHT cid=442636280
It's a little tedious now since we can't roundtrip these but can you manually construct the expected JSON string and compare the result to that?

## [35] pandas/tests/io/json/test_ujson.py:563 author=jreback reply=false subj=line side=RIGHT cid=441868531
can you also add a full integration test in json/test_pandas.py

## [60] pandas/tests/io/json/test_ujson.py:563 author=WillAyd reply=true subj=line side=RIGHT cid=442409782
Should also parametrize this to exceed the minimum supported native size

## [85] pandas/tests/io/json/test_ujson.py:563 author=WillAyd reply=true subj=line side=RIGHT cid=442887750
Can you do the parametrization of a large negative value here too?

## [88] pandas/tests/io/json/test_ujson.py:563 author=WillAyd reply=false subj=line side=RIGHT cid=443065197
```suggestion
    @pytest.mark.parametrize("bigNum", [sys.maxsize + 1, -(sys.maxsize + 2)])
```

To exceed the negative range would have to add 2

## [54] pandas/tests/io/json/test_ujson.py:567 author=arw2019 reply=false subj=line side=RIGHT cid=442387623
@WillAyd Seeing an error when I run pytest on this:
```
================================================================================== short test summary info ==================================================================================
FAILED pandas/tests/io/json/test_ujson.py::TestUltraJSONTests::test_encode_numeric_overflow_nested - OverflowError: Unterminated UTF-8 sequence when encoding string
1 failed, 993 passed, 10 skipped, 37 xfailed, 27 warnings in 58.32s
```

## [7] pandas/tests/io/json/test_ujson.py:568 author=WillAyd reply=false subj=line side=RIGHT cid=435331578
```suggestion
        assert str(encoding) == json.dumps(big_num)
```

## [44] pandas/tests/io/json/test_ujson.py:568 author=WillAyd reply=false subj=line side=RIGHT cid=442355389
```suggestion
        assert str(big_num) == json.dumps(big_num)
```

## [4] pandas/tests/io/json/test_ujson.py:569 author=WillAyd reply=false subj=line side=RIGHT cid=434894195
This would require fixing both to_json and read_json which could be a rather large PR. I think OK to just compare the output of to_json to the expected string for now

## [92] pandas/tests/io/json/test_ujson.py:570 author=jreback reply=false subj=line side=RIGHT cid=443156882
is there an issue for this? don't leave in commented code, rather add an xfail test

## [94] pandas/tests/io/json/test_ujson.py:570 author=arw2019 reply=true subj=line side=RIGHT cid=443343358
added the test. Also added xfail tests for this in `test_pandas.py`. 

## [52] pandas/tests/io/json/test_ujson.py:591 author=arw2019 reply=false subj=line side=RIGHT cid=442386852
@WillAyd @jreback Do we want to add a test for nesting with `x=sys.maxsize+1`? That would also pass now 
