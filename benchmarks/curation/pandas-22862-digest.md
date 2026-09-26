# digest pandas-22862 : 216 anchored candidates (of 282 total)

## [70] doc/source/whatsnew/v0.24.0.txt:243 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224699544
why is the `.values` needed? Without it it also converts to object dtype of scalars correctly?

## [99] doc/source/whatsnew/v0.24.0.txt:243 author=TomAugspurger reply=true subj=line side=RIGHT cid=224797313
`PeriodIndex.astype(object)` converts to an `Index[Period]`. This block is about converting to an `ndarray[Period]` (same for interval).

## [134] doc/source/whatsnew/v0.24.0.txt:243 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225055441
Whoops, yeah of course :) 
That said, maybe pointing users only to `asarray` is enough?

## [147] doc/source/whatsnew/v0.24.0.txt:285 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226280176
I am still not fully sure that we should do this 
(could maybe also leave it for follow-up instead of changing in this PR?)

## [47] pandas/core/arrays/categorical.py:1265 author=jbrockmendel reply=false subj=line side=RIGHT cid=222175631
At the moment the cython code backing `take_1d` is partially-converted from requiring np.ndarray to more general typed memoryviews.  It might be worth looking into what it would take to make cython recognize EAs the way it does ndarrays.

## [48] pandas/core/arrays/categorical.py:1265 author=TomAugspurger reply=true subj=line side=RIGHT cid=224109834
> make cython recognize EAs the way it does ndarrays.

I haven't written much cython, but that sounds difficult since EA places no restrictions on how the data are actually stored. But if the underlying storage does implement the buffer protocol, it'd be nice to be able to use that down in our algos... I'm just not sure whether the EA would implement the buffer protocol as well, or whether pandas would be responsible for extracting the storage array that does implement it.

## [202] pandas/core/arrays/categorical.py:2430 author=jreback reply=false subj=line side=RIGHT cid=226628007
I would make this a function  I think as we are likely to do this in other places as well (or maybe already) do, e.g. in csv parsing with category?

## [203] pandas/core/arrays/categorical.py:2430 author=jreback reply=true subj=line side=RIGHT cid=226628185
this is basically what ``catetorical_array`` (generic construtor for Categoricals should be doing anyhow)

## [252] pandas/core/arrays/categorical.py:2430 author=TomAugspurger reply=true subj=line side=RIGHT cid=226709963
This called from the categorical constructor.

Ideally we would have a single place that does all this. Right now it feels scattered over `_sanitize_array`, the Index constructor, and probably a few other places.

## [148] pandas/core/arrays/datetimelike.py:485 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226280704
Possible follow-up one: should we rather overwrite `_addsub_int_array` in PeriodArray instead of doing an if/else here? 
(didn't check what the general pattern is for that)

## [188] pandas/core/arrays/datetimelike.py:485 author=TomAugspurger reply=true subj=line side=RIGHT cid=226344412
This seems like a textbook example of where overriding would be a good thing.

## [146] pandas/core/arrays/datetimelike.py:530 author=jbrockmendel reply=false subj=line side=RIGHT cid=226118531
In this case can just pass `freq="infer"` instead of `**kwargs`

## [161] pandas/core/arrays/datetimes.py:806 author=jreback reply=false subj=line side=RIGHT cid=226324135
``from pandas.core.arrays import PeriodArray``

## [204] pandas/core/arrays/period.py:18 author=jreback reply=false subj=line side=RIGHT cid=226628336
can you isort (if you have not done), and remov from the non-checking list

## [230] pandas/core/arrays/period.py:18 author=TomAugspurger reply=true subj=line side=RIGHT cid=226659380
Going to wait on that since there are other outstanding PRs touching imports.

## [149] pandas/core/arrays/period.py:70 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226281624
I thought we now tested that this was indeed returning NotImplemented?

## [184] pandas/core/arrays/period.py:70 author=TomAugspurger reply=true subj=line side=RIGHT cid=226332972
That's in https://github.com/pandas-dev/pandas/pull/23155 (not merged yet, but I think ready to go).

We'll need to re-implement / adjust the test, since it uses `EA.__add__(Series[EA])`, which isn't defined for PeriodArray. We can do `__sub__`.

## [246] pandas/core/arrays/period.py:70 author=TomAugspurger reply=true subj=line side=RIGHT cid=226679595
I had to add an xfail for `PeriodArray == Series[PeriodArray]`. We don't return NotImplemented for comparison yet. But I think changing ops is out of scope for this PR.

## [260] pandas/core/arrays/period.py:75 author=jbrockmendel reply=false subj=line side=RIGHT cid=226800296
Yes, return NotImplemented.  Also presumably ABCDataFrame.

## [271] pandas/core/arrays/period.py:75 author=TomAugspurger reply=true subj=line side=RIGHT cid=226813016
This will fail some tests because we reverse the error message.

```
arrays/period.py:86: AssertionError: "Input has different freq=A-DEC from PeriodIndex" does not match "Input has different freq=M from PeriodIndex(freq=A-DEC)"
```

I think that's OK though.

## [223] pandas/core/arrays/period.py:76 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226652565
We had a discussion related to this yesterday.

Some time ago, we added this explanation to the Index class:

https://github.com/pandas-dev/pandas/blob/145c2275e3560edc30ff418a57df67ba3c4c30d6/pandas/core/indexes/base.py#L702-L732

referring to `_values` as the "best array representation", so the attribute that will always be loss-less, and thus always will return the ExtensionArray if the Index (or Series) is backed by an ExtensionArray. 

By using this, that decouples this PR from the question what the public `.values` should do (keep returning ndarray of objects, or start returning PeriodArray?)





## [7] pandas/core/arrays/period.py:83 author=jbrockmendel reply=false subj=line side=RIGHT cid=221090009
No, but this is broken in the status quo, #21793

## [50] pandas/core/arrays/period.py:90 author=TomAugspurger reply=false subj=line side=RIGHT cid=224243718
This should be doable I think, assuming that the array can be converted to a PeriodArray.

## [13] pandas/core/arrays/period.py:109 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=221168608
Maybe mention what this `freq` is (physically)? An Offset subclass ?

## [100] pandas/core/arrays/period.py:110 author=TomAugspurger reply=true subj=line side=RIGHT cid=224797734
The various `*_range` function will return a `PeriodIndex` (Intervalndex, DatetimeIndex)`. Does it make sense to add a keyword to get the array instead of an index?

## [110] pandas/core/arrays/period.py:110 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=224822455
Ah, yes of course. I don't think it is a big short term problem that the `*_range` functions create index objects (if passed to eg Series, they do the correct thing), so let's leave that for a future issue.

## [9] pandas/core/arrays/period.py:117 author=jbrockmendel reply=true subj=line side=RIGHT cid=221090557
Part of why the constructors for the existing mixins are bare-bones is because there are comments in the Index subclasses suggesting things like start/end should be taken out of them.

## [14] pandas/core/arrays/period.py:117 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221169782
As I mentioned on the mailing list, I would rather go for a very simple constructor (basically what `_simple_new` is now below). I don't think our array classes should have a `__new__`. 
If it is too much work for this PR to refactor this `__new__` method, we could also leave it for now as is but under another name (eg `_complex_new` :-))

## [17] pandas/core/arrays/period.py:117 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221170773
And I think we could then combine `_simple_new` and `_from _ordinals` ?

## [23] pandas/core/arrays/period.py:117 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221192807
To maybe clarify what I mean, a small change to IntervalArray: https://github.com/pandas-dev/pandas/compare/master...jorisvandenbossche:intervalarray?expand=1 (needs better naming of course): remove `fastpath` (should be done anyway as it is not used), and use `__init__` instead of `__new__`. Of course, the functionality of passing a single array-like (of scalars or of interval dtype) what is now in the `__new__` could still be kept in the `__init__` if we find that important.

## [24] pandas/core/arrays/period.py:117 author=TomAugspurger reply=true subj=line side=RIGHT cid=221375429
No preference on doing it as part of this PR or another. The diff is already impossible to read, so I may as well try doing it here.

## [77] pandas/core/arrays/period.py:147 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224703904
we could also call the `values` argument here `ordinals`, to be more explicit?

## [105] pandas/core/arrays/period.py:147 author=TomAugspurger reply=true subj=line side=RIGHT cid=224801847
Haven't thought about this longer than 5 seconds, but I think that's a fine idea.

We'll need to be careful about the Index containers reaching into `Index.values.<stuff>`.

Raymond Hettinger tweeted https://www2.ccs.neu.edu/research/demeter/demeter-method/LawOfDemeter/paper-boy/demeter.pdf out the other day about this.

We can maybe make a private property that aliases `ordinals`, similar to `_ndarray_values`.

## [72] pandas/core/arrays/period.py:150 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224701322
Is it needed to accept PeriodIndex here? (for IntegerArray we didn't do that in the end, and I don't think there were problems with that) 
If something is not ordinals/freq, we can always use `period_array` function

## [206] pandas/core/arrays/period.py:153 author=jreback reply=false subj=line side=RIGHT cid=226628846
shouldn't this be simpler and instead just call ``period_array``?

## [225] pandas/core/arrays/period.py:153 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226655115
See the discussion in https://github.com/pandas-dev/pandas/issues/23212. From there, I understood that you were fine with keeping `__init__` limited in functionality, and having `period_array` that can do inference.

## [255] pandas/core/arrays/period.py:153 author=jreback reply=true subj=line side=RIGHT cid=226712576
sorry, yes you are right, this is what we do in IntegerArray. I would maybe document this very clearly, e.g. ``__init__`` is exactly construction from the underlying primitive array and has NO inference to be clear.

## [86] pandas/core/arrays/period.py:163 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224707312
IMO it might make sense to move this to a function instead of keeping it on the class (but that is certainly not essential for this PR)

## [207] pandas/core/arrays/period.py:166 author=jreback reply=false subj=line side=RIGHT cid=226629141
I think we *always* need to copy here, e.g. if you pass in an ndarray with copy=False this makes the internals mutable which is not great

## [224] pandas/core/arrays/period.py:166 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226654571
I think we should certainly have the *option* to not copy. 
Assume things like `take` or `fillna`: here we already create a new ndarray, that does not need to be copied. 

Can you explain a bit more in detail what you are thinking about, where you see a problem in the internals?

## [233] pandas/core/arrays/period.py:166 author=TomAugspurger reply=true subj=line side=RIGHT cid=226661582
(deleted an incorrect comment).

I think I was following Series, Index, and IntegerArray, IntervalArray, SparseArray, which don't copy (index is special because it's immutable).

We should be internally consistent among the array classes. `Categorical` doesn't have a copy parameter.

## [235] pandas/core/arrays/period.py:166 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226665726
The default of numpy arrays of `copy=True` is of course a safer default. But we should indeed try to be consistent. `IntegerArray` we can still change. For SparseArray, I suppose the default of not copy will in practice only be true if you already pass sparse values + sparse index. For `Categorical.from_codes` (the equivalent fast constructor) has no copy argument, and does not necessarily copy, but this can still depend whether the codes are eg converted from int64 to int8 depending on the number of categories.

## [256] pandas/core/arrays/period.py:166 author=jreback reply=true subj=line side=RIGHT cid=226713032
this is very tricky. If there are outside refernces here the I think we *need* to copy. You are right technically we don't need to and for Series we do have this behavior. But this is an outside leakage. I guess ``copy=True`` as a default just works here then.

## [259] pandas/core/arrays/period.py:166 author=TomAugspurger reply=true subj=line side=RIGHT cid=226739948
So a default of `copy=True` for now?



## [273] pandas/core/arrays/period.py:166 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226946552
I am fine with doing that (`copy=True` the default), but that will need some updates in most places where we are using the constructor I think.

## [277] pandas/core/arrays/period.py:166 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226949619
I am fine with that (`copy=True` as the default), but this will need an update in most places where the constructor is used I think.

## [279] pandas/core/arrays/period.py:166 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226952454
I am fine with doing that (copy=True the default), but that will need some updates in most places where we are using the constructor I think.

## [12] pandas/core/arrays/period.py:175 author=jbrockmendel reply=false subj=line side=RIGHT cid=221094183
The `isinstance(data, Period)` here should be redundant.

Is scalar_data_error used anywhere else?  If not it can probably be inlined.

## [261] pandas/core/arrays/period.py:183 author=jbrockmendel reply=false subj=line side=RIGHT cid=226800550
On the call we discussed avoiding `_data`.  Did that get un-done?

## [267] pandas/core/arrays/period.py:183 author=TomAugspurger reply=true subj=line side=RIGHT cid=226812594
IIRC, we talked about two things

1. Avoiding the name `._data` for the attribute storing the actual values (conflict with use in blocks, and elsewhere)
2. Standardize the name(s) used by the array

I've punted on 1 since I don't know what a better name would be, and that would require additional changes in DatetimeLikeArrayMixin, which uses `._data`, in addition to `.values` and `._ndarray_values`.

PeriodArray should just use `.asi8` for places it needs an integer array (one more push coming fixing a few ndarray_values I missed). 

## [272] pandas/core/arrays/period.py:183 author=jbrockmendel reply=true subj=line side=RIGHT cid=226814240
> I've punted on 1 since I don't know what a better name would be

On the call we discussed setting `_ndarray_values` directly in `__init__`.  Don't worry about this too much; if I'm the only one with a strong opinion I can push to change it in a follow-on PR.

## [191] pandas/core/arrays/period.py:209 author=TomAugspurger reply=true subj=line side=RIGHT cid=226481321
`dt64arr_to_periodarr` checks that first thing.

## [10] pandas/core/arrays/period.py:234 author=jbrockmendel reply=false subj=line side=RIGHT cid=221091016
possibly self._fill_value or self._na_value or something?  Those attributes exist in a few places but I don't think get used often.

## [11] pandas/core/arrays/period.py:247 author=jbrockmendel reply=false subj=line side=RIGHT cid=221091154
Off topic: this kind of thing could definitely go in a core.arrays.common mixin shared across pandas-internals EAs

## [16] pandas/core/arrays/period.py:247 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221170398
Not all of the arrays will support this (but maybe enough do to share it ...), eg IntervalArray has two arrays, Categorical in principle has also both codes and categories

(in any case, I would also leave that for later, if we have the multiple arrays, we can see which functionality is common and can be moved to a mixin/superclass, but now with the different arrays in different PRs this will be a bit difficult)

## [25] pandas/core/arrays/period.py:247 author=TomAugspurger reply=true subj=line side=RIGHT cid=221376119
Will postpone (though certainly all of DatetimeArray, TimedeltaArray, and PeriodArray will share this).

## [73] pandas/core/arrays/period.py:266 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224702416
Is this `_simple_new` still needed? This is basically the `__init__` now?

## [132] pandas/core/arrays/period.py:268 author=jbrockmendel reply=false subj=line side=RIGHT cid=225012659
Does calling the base constructor for simple_new make sense?

## [138] pandas/core/arrays/period.py:268 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225060139
I think I commented it somewhere else, but I think we should look into just merging the two in the default `__init__`. `_simple_new` is basically doing exactly the same

## [31] pandas/core/arrays/period.py:274 author=jbrockmendel reply=false subj=line side=RIGHT cid=221434982
Should the `deep` arg be passed through to `self._data.copy()`?

## [102] pandas/core/arrays/period.py:274 author=TomAugspurger reply=true subj=line side=RIGHT cid=224798834
The code is correct, but confusing (we pass through `freq=dtype` later). I'll clarify. 

## [78] pandas/core/arrays/period.py:275 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224704131
`_from_periods` already does the same, so it should not be needed here

## [75] pandas/core/arrays/period.py:281 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224703495
I don't see a `_values_for_factorize`, but I would expect this to be the ordinals, and that we could use here the simpler constructor?

## [103] pandas/core/arrays/period.py:281 author=TomAugspurger reply=true subj=line side=RIGHT cid=224800273
Fixed by implementing `_values_for_factorize`.

## [76] pandas/core/arrays/period.py:288 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224703747
`__new__` doesn't exist anymore.

Should we keep this method? It's a bit redundant, but has a more explicit name which might be useful in some cases?

## [104] pandas/core/arrays/period.py:288 author=TomAugspurger reply=true subj=line side=RIGHT cid=224800814
I needed it for my own sanity to figure out which methods were using ndarray[int] and which were using ndarray[period]. I think we'll be able to remove it before merging this PR.

## [208] pandas/core/arrays/period.py:307 author=jreback reply=false subj=line side=RIGHT cid=226629444
dont' we have a default repr that should work here?

## [226] pandas/core/arrays/period.py:307 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226655575
Not yet, but we should indeed have it. See also https://github.com/pandas-dev/pandas/issues/22846 
But let's put this on the list of follow-up issues?


## [209] pandas/core/arrays/period.py:314 author=jreback reply=false subj=line side=RIGHT cid=226629613
is the super definition ok here instead?

## [227] pandas/core/arrays/period.py:314 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226656905
`DatetimeLikeArrayMixin` has it indeed, so that can be removed here

## [79] pandas/core/arrays/period.py:315 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224704564
We should decide whether to simply use `cls(..)` in such cases, or `_simple_new` or `_from_ordinals`. 
I would at least say to drop `_simple_new`

## [234] pandas/core/arrays/period.py:318 author=TomAugspurger reply=true subj=line side=RIGHT cid=226664697
Done. The syntax is for wrapping long lines is kind of ugly :/

```python
    def __setitem__(
            self,
            key,   # type: Union[int, Sequence[int], Sequence[bool]]
            value  # type: Union[NaTType, Period, Sequence[Period]]
        ):
        # type: (...) -> None
```


## [150] pandas/core/arrays/period.py:323 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226287586
There are a few places in this file where `asi8` gets calculated as `asi8 = self._ndarray_values.view('i8')`, which can be updated. 

Further, `asi8` gets inherited from datetimelike as `self.values.view('i8')` (and `self.values` itself is already `self._data.view(np.ndarray)`), which does not seem to be necessary for PeriodArray? If we keep the `asi8` property, I would overwrite it here to more directly return the ordinals, that will be clearer in the code I think.

## [181] pandas/core/arrays/period.py:323 author=TomAugspurger reply=true subj=line side=RIGHT cid=226330740
I think this was an attempt to remove `PeriodArray.asi8`. But, I suppose that's a useful property (unlike `.base` and `.flags`).

## [185] pandas/core/arrays/period.py:323 author=TomAugspurger reply=true subj=line side=RIGHT cid=226335670
To be clear the choice is between

1. Implementing `PeriodArray.view` and re-using `DatetimelikeArrayMixin.asi8`
2. Implementing `PeriodArray.asi8` as `return self._ndarary_values`.

I think 2 is better.

## [186] pandas/core/arrays/period.py:323 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226336549
Agreed

(or as `return self._data`, one level of redirection less)

## [189] pandas/core/arrays/period.py:323 author=jbrockmendel reply=true subj=line side=RIGHT cid=226351060
-1 on `self._data`.  I think `asi8` is the most explicit option

## [38] pandas/core/arrays/period.py:328 author=TomAugspurger reply=true subj=line side=RIGHT cid=222059547
I've been waffling on what to do with error messages. If we do

```python
idx = pd.period_range("2017", periods=2, freq="D")
idx.freq = "A"
```

What do we want the warning message to be? Implementation-wise, `PeriodIndex.freq` just refers to `DatetimeIndex._data.freq`, i.e. `PeriodArray.freq`. But do users care about that, or do they just care that setting `.freq` won't work for period-type data? 

## [40] pandas/core/arrays/period.py:328 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=222074167
What do you mean exactly? 
About what to do with the actual wording the warning message? (PeriodIndex vs PeriodArray)

## [41] pandas/core/arrays/period.py:328 author=TomAugspurger reply=true subj=line side=RIGHT cid=222075151
Exactly. If PeriodArray isn't part of the public API (which is TBD), it probably shouldn't be showing up in warning messages.

## [42] pandas/core/arrays/period.py:328 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=222075966
In this specific case, we can keep the warning at the PeriodIndex level IMO. Setting the freq on PeriodArray can simply be disallowed, what we will do in the future for PeriodIndex as well?

## [43] pandas/core/arrays/period.py:328 author=TomAugspurger reply=true subj=line side=RIGHT cid=222077364
This was deprecated in 0.23 (https://github.com/pandas-dev/pandas/pull/20772) so we'll have to allow setting PeriodArray.freq for at least a version.

## [44] pandas/core/arrays/period.py:328 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=222078301
But it maybe doesn't need to be through the public property? You can set `_freq` here, but already disallow setting the public `freq` on the array

## [46] pandas/core/arrays/period.py:328 author=jbrockmendel reply=true subj=line side=RIGHT cid=222131844
> If PeriodArray isn't part of the public API (which is TBD), it probably shouldn't be showing up in warning messages.

Dangit, I hate to keep coming back to this, but this wouldn't be an issue with inheritance and the existing warning message...

## [151] pandas/core/arrays/period.py:333 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226288997
For backwards compatibility, I think we need to allow that for now? (we are still using np.nan almost everywhere as the missing value indicator, something we should eventually change IMO, but that's another discussion, so until then it would be strange to not allow it here )

## [165] pandas/core/arrays/period.py:333 author=jreback reply=true subj=line side=RIGHT cid=226326411
I think we *always* will want to support this. it is pretty ingrained. We don't want / need to think about which dtype we are in when setting null value, simply using ``np.nan`` as our generic value to set is enough.

## [129] pandas/core/arrays/period.py:346 author=jbrockmendel reply=false subj=line side=RIGHT cid=224979163
Isn’t this already in the datetimelikemixin?

## [139] pandas/core/arrays/period.py:346 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225061729
I think we should also look if this property is actually needed. Where is it used? In things that are common to datetime like arrays?

Because internally here, in many places we also use `_data`, which is basically the same?

For DatetimeArray, there is of course a clearer difference between `_data` and `asi8` (where that would be datetime64 vs int64, while here both are int64). That might be enough reason to keep to share code between the datetimelikes.

And we also have `_ndarray_values` that is used in some places here in the class. 

I think for PeriodArray-only internal implementations, we should maybe mainly be using `_data` ? 

## [80] pandas/core/arrays/period.py:357 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224705091
Not just `PeriodDtype(freq=self.freq)` ? (it seems strange to need to use `from_string`, as there is not string here)

## [106] pandas/core/arrays/period.py:357 author=TomAugspurger reply=true subj=line side=RIGHT cid=224802391
That does seem strange... I'd rather just set a `_dtype` in the init, and rather have `freq` be `PeriodArray.dtype.freq`.

## [192] pandas/core/arrays/period.py:365 author=TomAugspurger reply=true subj=line side=RIGHT cid=226481789
Inherited seems OK (aside from lack of examples).

## [107] pandas/core/arrays/period.py:366 author=TomAugspurger reply=true subj=line side=RIGHT cid=224803081
Seems unlikely. I assume that's a copy-paste from DatetimeIndex, where freq may be None.

## [82] pandas/core/arrays/period.py:374 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224705350
Yes, I would do that, keep the "cruft" to deal with deprecations in PeriodIndex, to have a cleaner new PeriodArray ?

## [83] pandas/core/arrays/period.py:379 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224705679
This is also needed for some PeriodIndex compatibility? (although I don't directly see any usage in the PeriodIndex implementation) 
We should look into how to avoid having this I think.

## [85] pandas/core/arrays/period.py:383 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=224706805
Ah, I suppose this is again to deal with the deprecation? I would deal with this in Index/PeriodIndex

## [127] pandas/core/arrays/period.py:383 author=jbrockmendel reply=true subj=line side=RIGHT cid=224965718
> I would deal with this in Index/PeriodIndex

Agreed.  IIRC this is currently done in IndexOpsMixin.

Isn't the point of `.data` to point to the underlying data that this might be a view on?  Realizing an object array seems weird for that

## [211] pandas/core/arrays/period.py:390 author=jreback reply=false subj=line side=RIGHT cid=226630318
this is only used on line 405  yes? if so maybe move this there? 

whey do we need to mask the value? I don't recall doing this anywhere else, e.g. shouldnt' setitem (on the PeriodArray) actually handle this?

## [236] pandas/core/arrays/period.py:390 author=TomAugspurger reply=true subj=line side=RIGHT cid=226668595
I think the implementation is correct.

If we didn't mask `value`, then we'd try to do something like

`array([0, 1, None])[False, False, True].__setitem__([1, 2, 3])`, which would fail since you're setting an array of length 3 into an array of length 1 (the mask original array).

## [237] pandas/core/arrays/period.py:390 author=TomAugspurger reply=true subj=line side=RIGHT cid=226668805
And it needs to be outside the `if mask.any()` so that we raise if they do something like


```
array([1, 2, 3]).fillna([1, 2])
```

i.e. filling with an array of the wrong length.

## [257] pandas/core/arrays/period.py:390 author=jreback reply=true subj=line side=RIGHT cid=226713263
that *should* fail, why are you allowing it?

## [212] pandas/core/arrays/period.py:417 author=jreback reply=false subj=line side=RIGHT cid=226631003
.value_counts has a dropna parameter but guess can't use this as we need to convert to a primitive type and there is a separate isna check done in value_counts, hmm. ought to think about letting a ``mask`` pass thru (which we did recently with nanops.

## [228] pandas/core/arrays/period.py:417 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226657922
Sidenote: since we are considering to remove `value_counts` from the interface (https://github.com/pandas-dev/pandas/issues/22843), maybe we can already remove it here (or at least not put a lot of effort in it)

## [268] pandas/core/arrays/period.py:436 author=TomAugspurger reply=true subj=line side=RIGHT cid=226812600
`_data` is an ndarray, which doesn't have a notion of a deep copy.

## [33] pandas/core/arrays/period.py:467 author=jbrockmendel reply=false subj=line side=RIGHT cid=221435328
Good catch, maybe better to use shallow_copy?

## [133] pandas/core/arrays/period.py:474 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=225055180
@jbrockmendel I don't think we should use `self._isnan` here. This is a concept from `Index`, that we should not need to copy for the arrays (we will need to move back `_isnan` to the Index anyway I think, as there are some Index internal methods that make use of it)

## [141] pandas/core/arrays/period.py:474 author=jbrockmendel reply=true subj=line side=RIGHT cid=225212400
The topic of whether _isnan belongs in the EA subclasses I think should be discussed separately/later.  As long as it does exist, it should be used instead of duplicated.

## [196] pandas/core/arrays/period.py:477 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226590616
Where does it occur that `x` is already a Period? 
That seems that `_box_func` is then used in two very different circumstances?

## [197] pandas/core/arrays/period.py:477 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226591325
Ah, I see this was coming from the Index implementation

## [199] pandas/core/arrays/period.py:477 author=TomAugspurger reply=true subj=line side=RIGHT cid=226615876
I'm looking into this now. I feel like we shouldn't have to change it.

## [200] pandas/core/arrays/period.py:477 author=TomAugspurger reply=true subj=line side=RIGHT cid=226616532
Ahh I see.

So if I do something basic like `period_index[0]` `DatetimelikeIndex.__getitem__` does

```
    169         getitem = self._data.__getitem__
    170         if is_int:
    171             val = getitem(key)
--> 172             return self._box_func(val)
 ```

`getitem` is `PeriodArray.__getitem__`, which has to already box ordinals as Periods (to fit the EA API). So we try to rebox it.

I was hoping to simplify by moving these to a single `_box_func`, but I think this is more complicated.


## [152] pandas/core/arrays/period.py:515 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226291626
related to my comment above. Instead of changing this, I would rather update `asi8` definition to be this (or otherwise remove asi8 completely)

And why is the `view('i8')` needed? We know this is always already the case no?

## [169] pandas/core/arrays/period.py:515 author=jreback reply=true subj=line side=RIGHT cid=226327222
I agree ``.asi8`` is a very common method on the dateimelikes

## [130] pandas/core/arrays/period.py:524 author=jbrockmendel reply=false subj=line side=RIGHT cid=224979203
If fromordinals became simplenew, wouldn’t this be valid for all 3 datetimelike?

## [26] pandas/core/arrays/period.py:535 author=TomAugspurger reply=false subj=line side=RIGHT cid=221376346
This is a bit ugly, but I think unavoidable.

## [131] pandas/core/arrays/period.py:543 author=jbrockmendel reply=false subj=line side=RIGHT cid=224979228
What happens if PeriodArray is passed as index here?

## [153] pandas/core/arrays/period.py:648 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226293183
`_add_delta_td` already returns an EA, so you can directly return instead of passing to `type(self)(..)` ?

## [193] pandas/core/arrays/period.py:814 author=TomAugspurger reply=true subj=line side=RIGHT cid=226482977
Why's that? pandas will be cached at this point, so it's quite fast.

## [87] pandas/core/arrays/period.py:821 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224708630
Deal with this in PeriodIndex? Or maybe, to avoid to need to something different in each of DatetimeIndex/PeriodIndex, make this here a private function, so it does not clutter the public API of PeriodArray?

## [88] pandas/core/arrays/period.py:846 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224708883
This is temporary no? (once DatetimeArray is done as well, we don't have this complex inheritance anymore?)

## [274] pandas/core/arrays/period.py:853 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226947560
Could you still add the basic types? (with a `# type` comment) That at least makes it a bit easier to see what the function should be doing, without going to look at the parent's docstring?

## [278] pandas/core/arrays/period.py:853 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226949999
Could you still add some basic type information (eg a `# type` comment), so at least from reading that you can have an idea what the method should be doing without going to look for the parent docstring? (like is done eg here https://github.com/pandas-dev/pandas/blob/4cac923706bac52f34cab24b7d5df1d1a0f15cb9/pandas/core/arrays/datetimes.py#L459-L460)

## [269] pandas/core/arrays/period.py:854 author=TomAugspurger reply=true subj=line side=RIGHT cid=226812658
To support `PeriodIndex.item`, while avoiding unnecessary methods on PeriodArray.

But I see now it would be clearer to just implement this logic in `PeriodIndex itself.

## [89] pandas/core/arrays/period.py:857 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224709092
`self._data` instead of `self.values` ? And shouldn't the actual integer dtype be passed? (eg if you want to convert it to int32 ?)

## [155] pandas/core/arrays/period.py:857 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226298859
this one 
(or at least `_ndarray_values`)

## [51] pandas/core/arrays/period.py:871 author=TomAugspurger reply=false subj=line side=RIGHT cid=224244898
This is for `Index.item()` which uses `.values.item`

## [60] pandas/core/arrays/period.py:871 author=jbrockmendel reply=true subj=line side=RIGHT cid=224275171
This can be done in a follow-up, but things like this that are currently in `IndexOpsMixin` could go into something like `core.arrays.common.ArrayMixin`

## [90] pandas/core/arrays/period.py:871 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=224709398
This could also be moved to `PeriodIndex` ?

## [264] pandas/core/arrays/period.py:873 author=jbrockmendel reply=false subj=line side=RIGHT cid=226801543
Any reason not to keep the arithmetic ops organized together?  (e.g. add_delta_tdi above)

## [91] pandas/core/arrays/period.py:889 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224709786
Should we also accept strings here? Or have a separate function for that (something like `to_period` similar as we have `to_datetime`)

## [215] pandas/core/arrays/period.py:889 author=jreback reply=true subj=line side=RIGHT cid=226631821
we should prob have a separate function (there is already an issue for this). as you get into cases where you can have formatting, eg.. ``2012Q1`` and provide fromat strings.

## [216] pandas/core/arrays/period.py:889 author=jreback reply=true subj=line side=RIGHT cid=226632007
though would not be averse to actually calling ``to_period`` here with a default format on an inferred string type.

## [229] pandas/core/arrays/period.py:889 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226658892
In any case, adding the ability of parsing strings is out of scope for this PR I would say, so we can discuss later where to add it.

## [240] pandas/core/arrays/period.py:889 author=TomAugspurger reply=true subj=line side=RIGHT cid=226671071
This already works. I'll add an example.

```
In [6]: pd.core.arrays.period_array(['2012Q1', '2013Q1'], freq='Q')
Out[6]:
<PeriodArray>
['2012Q1', '2013Q1']
Length: 2, dtype: period[Q-DEC]
```

I'm not sure what's required for `freq` to be inferred correctly, but `libperiod.extract_freq` and `libperiod.extract_ordinals` are doing all the heavy lifting here.

## [241] pandas/core/arrays/period.py:889 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226673440
Ah, cool, didn't know that :) 
(it's of course limited to standard formatted strings I suppose?)

## [177] pandas/core/arrays/period.py:896 author=jreback reply=false subj=line side=RIGHT cid=226328631
shouldn't you do this 2nd check first? e.g. a dt64 Series ?

## [194] pandas/core/arrays/period.py:896 author=TomAugspurger reply=true subj=line side=RIGHT cid=226483764
I suppose so, so that `period_array(Series[datetime64])` works. Will add a test.

## [140] pandas/core/arrays/period.py:908 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=225062355
Same question as on other PR: is this needed? Where is it used? (in any case not in this file)

## [178] pandas/core/arrays/period.py:909 author=jreback reply=false subj=line side=RIGHT cid=226328867
this is quite fast for the vast majority of things, only object dtype is fully inferred

## [195] pandas/core/arrays/period.py:909 author=TomAugspurger reply=true subj=line side=RIGHT cid=226517591
Yeah, we're going to have objects here typically though.

For a length 1,000 array of period objects we spend 70us in `lib.infer_dtype` and 330us in the actual constructor, so about 20% of the time is just to raise this error message. I refactored it a bit to just check for `is_float_dtype`.

## [217] pandas/core/arrays/period.py:915 author=jreback reply=false subj=line side=RIGHT cid=226632205
what is the purpose of ordinal? its not in the doc-string

## [239] pandas/core/arrays/period.py:915 author=TomAugspurger reply=true subj=line side=RIGHT cid=226670567
Passing `ordinal` triggers the same behavior as `PeriodArray.__new__(ordinal=...)`.

I'll see if this is actually used anywhere.

## [242] pandas/core/arrays/period.py:915 author=TomAugspurger reply=true subj=line side=RIGHT cid=226674183
Based on a brief survey, it seems like it's just used in tests. The only callers are `PeriodIndex(ordinal=...)`.

```
pandas/tests/indexes/period/test_period.py
426:        idx1 = PeriodIndex(ordinal=[-1, 0, 1], freq='A')
427:        idx2 = PeriodIndex(ordinal=np.array([-1, 0, 1]), freq='A')

```

```
pandas/tests/indexes/common.py
318:                result = index_type(ordinal=index.asi8, copy=False,

```

So the question is: do we want to give users the ability to construct a `PeriodIndex` / `PeriodArray` from an array of integer ordinals + a freq? I don't think this is likely to occur, so I'd recommend:

1. removing the `ordinal` argument from `period_array`
2. deprecating `ordinal` in the `PeriodIndex` constructor, in favor of passing a `PeriodArray`.

## [249] pandas/core/arrays/period.py:930 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226701139
How can this case be distinguished from ordinals?

## [250] pandas/core/arrays/period.py:930 author=TomAugspurger reply=true subj=line side=RIGHT cid=226701864
magic? No idea though. That's what scares me about using them. Should I just remove that?

## [251] pandas/core/arrays/period.py:930 author=TomAugspurger reply=true subj=line side=RIGHT cid=226702377
Looks like it all comes down to whatever `Period` does on a scalar.

```python
In [7]: pd.Period(999, freq='D')
---------------------------------------------------------------------------
ValueError                                Traceback (most recent call last)
<ipython-input-7-cd7bbf684368> in <module>
----> 1 pd.Period(999, freq='D')

~/sandbox/pandas/pandas/_libs/tslibs/period.pyx in pandas._libs.tslibs.period.Period.__new__()
   2448                 value = str(value)
   2449             value = value.upper()
-> 2450             dt, _, reso = parse_time_string(value, freq)
   2451             if dt is NaT:
   2452                 ordinal = iNaT

~/sandbox/pandas/pandas/_libs/tslibs/parsing.pyx in pandas._libs.tslibs.parsing.parse_time_string()
    126         yearfirst = get_option("display.date_yearfirst")
    127
--> 128     res = parse_datetime_string_with_reso(arg, freq=freq,
    129                                           dayfirst=dayfirst,
    130                                           yearfirst=yearfirst)

~/sandbox/pandas/pandas/_libs/tslibs/parsing.pyx in pandas._libs.tslibs.parsing.parse_datetime_string_with_reso()
    152
    153     if not _does_string_look_like_datetime(date_string):
--> 154         raise ValueError('Given date string not likely a datetime.')
    155
    156     try:

ValueError: Given date string not likely a datetime.

In [8]: pd.Period(1000, freq='D')
Out[8]: Period('1000-01-01', 'D')

```

## [265] pandas/core/arrays/period.py:971 author=jbrockmendel reply=false subj=line side=RIGHT cid=226801719
This is for constructing from a single scalar?  Why is that supported? (i wonder that for PeriodIndex too)

## [0] pandas/core/dtypes/dtypes.py:671 author=jbrockmendel reply=false subj=line side=RIGHT cid=221088104
What's the logic for why iNaT instead of NaT?

## [1] pandas/core/dtypes/dtypes.py:671 author=TomAugspurger reply=true subj=line side=RIGHT cid=221088606
It should be NaT. I got the user-facing and physical-storage semantics backwards.

## [29] pandas/core/dtypes/generic.py:56 author=jbrockmendel reply=false subj=line side=RIGHT cid=221399184
Should ABCPeriodIndex be updated to recognize PeriodArray?

## [37] pandas/core/dtypes/generic.py:56 author=TomAugspurger reply=true subj=line side=RIGHT cid=222058918
I don't think so, should it? A PeriodArray isn't an Index / PeriodIndex.

## [45] pandas/core/dtypes/generic.py:56 author=jbrockmendel reply=true subj=line side=RIGHT cid=222131490
I guess that comment was implicitly assuming inheritance instead of composition.  Never mind.

## [52] pandas/core/frame.py:5117 author=TomAugspurger reply=false subj=line side=RIGHT cid=224249213
I was a little confused by all this. Opened https://github.com/pandas-dev/pandas/issues/23079. I think that should be resolved, and then these workarounds can hopefully be removed.

## [67] pandas/core/indexes/accessors.py:50 author=jbrockmendel reply=false subj=line side=RIGHT cid=224277609
This changes the mutability of the returned object.  Is that intentional?

## [113] pandas/core/indexes/base.py:232 author=jbrockmendel reply=false subj=line side=RIGHT cid=224877552
This, edits in core.accessor, and I think in tseries.frequencies could be separated into a small orthogonal PR

## [66] pandas/core/indexes/base.py:309 author=jbrockmendel reply=false subj=line side=RIGHT cid=224277446
Should the `isinstance(data, PeriodIndex)` check below (L376) be deleted?

## [179] pandas/core/indexes/base.py:316 author=TomAugspurger reply=true subj=line side=RIGHT cid=226329867
We require that `Index(..., dtype=object)` always return an `Index`, and not a subclass. So this makes `Index(PeriodArray, dtype='object')` and `Index[ndarray[object]]`

## [180] pandas/core/indexes/period.py:48 author=jreback reply=false subj=line side=RIGHT cid=226330293
I think @jbrockmendel consolidated these accessors already somewhere

## [218] pandas/core/indexes/period.py:67 author=jreback reply=false subj=line side=RIGHT cid=226634366
data -> values for consistency (or change all to data)

## [97] pandas/core/indexes/period.py:90 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224720871
I think this delegation is a good idea for the dtype-specific ones, but it might be more explicit to keep general ones as `size` and `shape` as actual properties calling the underlying values (but could be shared for the different index classes based on EA). 
Anyway, not that important for this PR, this can be refactored after converting all Arrays.

## [53] pandas/core/indexes/period.py:98 author=TomAugspurger reply=false subj=line side=RIGHT cid=224251732
Are people OK with PeriodIndex also being an accessor? That seemed like the easiest way to dispatch things down to PeriodArray.

## [59] pandas/core/indexes/period.py:98 author=jbrockmendel reply=true subj=line side=RIGHT cid=224274953
Is performance affected?  I'm still in the "inheritance is the easiest way to dispatch" camp, but recognize that I've lost this one.

## [111] pandas/core/indexes/period.py:98 author=TomAugspurger reply=true subj=line side=RIGHT cid=224859104
Beyond an extra function call (or two), I don't think so, though I will check. These aren't descriptors so we aren't changing getattribute.

## [182] pandas/core/indexes/period.py:98 author=jreback reply=true subj=line side=RIGHT cid=226330759
we could make a generic mechanism to do this at some point. We are eventually going to be doing this with all Index.

## [187] pandas/core/indexes/period.py:98 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226338235
Related to that, as I think this is for a follow-up, how do we keep track of the follow-up ideas? Update a list in the top-post (to not have it somewhere in the middle be hidden by github)

## [20] pandas/core/indexes/period.py:203 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=221172880
Should we standardize on using `_values` for the underlying EA for now, so some classes (like DatetimeIndex) can still return ndarray for `.values`, but so internally we can use a certain attribute consistently?

## [93] pandas/core/indexes/period.py:204 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=224713311
Or `self._data._data` to avoid extra indirection

## [2] pandas/core/indexes/period.py:224 author=jbrockmendel reply=false subj=line side=RIGHT cid=221088890
Yah, I was thinking this when reading `asfreq` a few lines up.  Maybe a `_from_period_array` or more generally `_from_ea`?


## [21] pandas/core/indexes/period.py:224 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221173909
In eg IntervalIndex, we kept the name `_simple_new` for the constructor that takes the EA. I think this is also in the same spirit as what `_simple_new` did before (getting the raw ordinals).

I would not call it `_from_period_array`, since then the name will be different for each class.

## [94] pandas/core/indexes/period.py:234 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224713486
I would move this up to under the `__new__` since this is basically the `__init__`

## [114] pandas/core/indexes/period.py:234 author=jbrockmendel reply=true subj=line side=RIGHT cid=224878691
Definitely put the constructors together

## [115] pandas/core/indexes/period.py:244 author=jbrockmendel reply=false subj=line side=RIGHT cid=224878784
I think the docstring is now inaccurate, but otherwise this is really nice

## [116] pandas/core/indexes/period.py:254 author=jbrockmendel reply=false subj=line side=RIGHT cid=224879039
No love for the get-rid-of-_from_ordinals idea?

## [95] pandas/core/indexes/period.py:257 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224719579
So from some printing in the tests, some exploration on what is passed here:

- PeriodArray and int64 ndarray ordinals. I think those both are fine, it will probably hard to avoid mixing both? Or do we want a separate one for ordinals?
- object array of Periods. 
  - One example of this is `PeriodIndex.difference` (from the base Index implementation). This base implementation basically works, except that there is a `sorting.safe_sort` call on the resulting PeriodArray, which destroys the PeriodArray. But this is of course solvable in `sorting.safe_sort`, by making that EA aware.
  - So I think eventually we could try to solve all those cases where object is passed. But I would say, let's leave that for follow-ups ?
- None -> this is from plain `self.shallow_copy()` calls without arguments. This is fine I think.


## [117] pandas/core/indexes/period.py:257 author=jbrockmendel reply=true subj=line side=RIGHT cid=224879778
This is basically what motivated #23095.  Even if the solution is unwanted there, I think it identifies all the extant places where unwanted types are currently passed to _shallow_copy

## [118] pandas/core/indexes/period.py:273 author=jbrockmendel reply=false subj=line side=RIGHT cid=224880268
Yah, I'm not wild about this.  I still think the best option is to nail down the constructors before doing the whole PeriodArray changeover.

## [119] pandas/core/indexes/period.py:302 author=jbrockmendel reply=false subj=line side=RIGHT cid=224880704
Where is this (and _maybe_box_as_index) used?

## [135] pandas/core/indexes/period.py:302 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225057162
It's used in the `sort_values` and `_concat_same_dtype` implementations that are shared between the datetimelike indexes 
(but those might certainly be considered for a re-thinking in a follow-up PR I think)

## [143] pandas/core/indexes/period.py:302 author=jbrockmendel reply=true subj=line side=RIGHT cid=225214095
> It's used in the `sort_values` and `_concat_same_dtype` implementations that are shared between the datetimelike indexes

The only place I'm seeing it is in `astype` (in master)

## [144] pandas/core/indexes/period.py:302 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225226648
I was talking about this branch I think (I don't think it is used in master?)

## [158] pandas/core/indexes/period.py:303 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226303936
I think this method can be removed. It is only used in `_new_PeriodIndex`, where it can be replaced with combination of PeriodArray and simple_new I think). 
Also, docstring is not up to date.

## [4] pandas/core/indexes/period.py:313 author=jbrockmendel reply=false subj=line side=RIGHT cid=221089419
Maybe take this opportunity to move away from `_data`?  Brainstorm: `_initvalues`, `_i8values`

## [22] pandas/core/indexes/period.py:313 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=221175241
For IntervalIndex and CategoricalIndex we use `_data` as well (@jbrockmendel the reason you mention this is to not have the confusion with BlockManager `_data` of DataFrame/Series ?)

But going from our overview table

https://github.com/pandas-dev/pandas/blob/d115900f4f80d2f9016b41041bde1564980415b3/pandas/core/indexes/base.py#L690-L722

we could maybe also use the `_values` directly? Instead of having `_values` point to `_data` ?

## [120] pandas/core/indexes/period.py:331 author=jbrockmendel reply=false subj=line side=RIGHT cid=224881125
On principle shouldn't we be avoiding DatetimeIndex._simple_new here?  The performance impact can't be that big a deal can it?

Sidenote: probably should have a to_index method for DatetimeArray etc, so this last line just becomes `return result.to_index(name=self.name)`

## [136] pandas/core/indexes/period.py:331 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225058219
> On principle shouldn't we be avoiding DatetimeIndex._simple_new here?

In principle yes, but since our default constructors are that convoluted (and that is not something we can easily change), I think we should consider the `_simple_new` as an "internal public" method, meaning that we can use it throughout the pandas codebase (so outside it's own class definition where one can do `self._simple_new`)

> probably should have a to_index method for DatetimeArray etc

Personally, I think Arrays should be completely ignorant of the Index concept. That keeps a clearer separation of concerns

## [142] pandas/core/indexes/period.py:331 author=jbrockmendel reply=true subj=line side=RIGHT cid=225212807
> Personally, I think Arrays should be completely ignorant of the Index concept

That's fair.  `arr.to_index()` really isn't any less verbose than `pd.Index(arr)`

## [121] pandas/core/indexes/period.py:343 author=jbrockmendel reply=false subj=line side=RIGHT cid=224881690
Is there an overarching logic behind when you're using .values vs when ._data?  The latter I guess is slightly more performant, but the former seems easier to remember as "this is always lossless (or at least will be)"

## [231] pandas/core/indexes/period.py:376 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226660339
I think @jbrockmendel is also removing it in another PR

## [243] pandas/core/indexes/period.py:376 author=TomAugspurger reply=true subj=line side=RIGHT cid=226674629
Will wait on https://github.com/pandas-dev/pandas/pull/23215#discussion_r226662611

## [122] pandas/core/indexes/period.py:410 author=jbrockmendel reply=false subj=line side=RIGHT cid=224881866
Any particular reason for PeriodIndex instead of type(self) or shallow_copy?

## [244] pandas/core/indexes/period.py:430 author=TomAugspurger reply=true subj=line side=RIGHT cid=226674863
That's the wrong shift :), what we call `_time_shift` internally now. We want the `Series.shift` kind here.

## [123] pandas/core/indexes/period.py:484 author=jbrockmendel reply=false subj=line side=RIGHT cid=224882398
Recently implemented wrap_array_method.  I think the thing to do here is edit that function so that this becomes a one-liner `_box_values_as_index = wrap_array_method("_box_values_as_index", pin_name=True)` 

## [137] pandas/core/indexes/period.py:484 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=225058937
BTW, how can this actually work, as PeriodArray has no such method?

## [221] pandas/core/indexes/period.py:533 author=jreback reply=false subj=line side=RIGHT cid=226635446
this looks like a new name, do we really need this?

## [232] pandas/core/indexes/period.py:533 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=226660670
See the todo, it can be removed once the split is complete (also the other Datetime-like index/array are splitted). Or at least that is how I understand the comment.

## [245] pandas/core/indexes/period.py:533 author=TomAugspurger reply=true subj=line side=RIGHT cid=226677470
Well, apparently I changed the behavior. On master we return an object-dtype `Index[Period]`, but on this PR it returns a `PeriodIndex`. I'll try removing.

## [49] pandas/core/reshape/reshape.py:102 author=TomAugspurger reply=false subj=line side=RIGHT cid=224239688
I don't intend for this to be included in the final PR. Opened https://github.com/pandas-dev/pandas/issues/23077 to solve this for all EAs.

## [65] pandas/io/pytables.py:2487 author=jbrockmendel reply=false subj=line side=RIGHT cid=224277087
We discussed this briefly but never came to a conclusion.  Were you not on board with the idea of making _simple_new the lowest-level constructor for cross-class consistency?

## [198] pandas/tests/arrays/test_period.py:54 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226592944
`._data` instead? (or `.asi8`) Since we want to get rid of values?

## [276] pandas/tests/arrays/test_period.py:176 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=226948444
Isn't this already covered by the base extension tests?

## [281] pandas/tests/arrays/test_period.py:176 author=TomAugspurger reply=true subj=line side=RIGHT cid=227435293
Ah... never mind. It's only tested via `Series[ExtensionArray].__setitem__`. The actual error raised in ExtensionArray.__setitem__` is not tested. Will open an issue.

## [6] pandas/tests/dtypes/test_common.py:615 author=jbrockmendel reply=false subj=line side=RIGHT cid=221089685
This is inevitably going to be a big PR before its through.  Might be worth separating out easy bits like this.

## [28] pandas/tests/extension/conftest.py:38 author=TomAugspurger reply=false subj=line side=RIGHT cid=221376717
Previously, you could end up with a `Series([NotImplementedError] * count])` if you didn't override this.

## [34] pandas/tests/extension/conftest.py:38 author=jbrockmendel reply=true subj=line side=RIGHT cid=221746148
Is this specific this to this PR, or could it go in a logically-independent PR?

## [55] pandas/tests/indexes/period/test_formats.py:121 author=TomAugspurger reply=false subj=line side=RIGHT cid=224254046
I haven't looked into why, but the repr in a series now has an extra space.

## [253] pandas/tests/indexes/period/test_tools.py:109 author=TomAugspurger reply=true subj=line side=RIGHT cid=226711722
Had a bug in an earlier version. Wanted to make sure I didn't regress.

## [56] pandas/tests/io/formats/test_format.py:1723 author=TomAugspurger reply=false subj=line side=LEFT cid=224254183
Two changes (sorry)

1. newlines for readability
2. that extra space in the repr.

## [57] pandas/tests/scalar/period/test_period.py:1044 author=TomAugspurger reply=false subj=line side=RIGHT cid=224254398
I should be able to revert this now that we infer period dtype in Series

## [63] pandas/tests/series/test_datetime_values.py:200 author=jbrockmendel reply=false subj=line side=RIGHT cid=224276483
Won't this introduce a flake8 complaint?

## [58] pandas/tests/series/test_period.py:56 author=TomAugspurger reply=false subj=line side=RIGHT cid=224254693
This is a breaking change. I can probably live with it though.

## [62] pandas/tests/series/test_period.py:56 author=jbrockmendel reply=true subj=line side=RIGHT cid=224276293
Can this have a big TODO or FIXME next to it?  pandas is generally very good about not having commented-out code.

## [112] pandas/tests/series/test_period.py:56 author=TomAugspurger reply=true subj=line side=RIGHT cid=224859609
Just wanted to call attention to it before removing. This won't be merged.

Are people OK with that? Do we need to support `.fillna` changing the dtype of an EA?

## [61] pandas/tseries/frequencies.py:236 author=jbrockmendel reply=false subj=line side=RIGHT cid=224276134
If you're OK with it, I'm going to copy this and a other non-central things into a hodge-podge PR; the smaller diff here should make review easier.

## [36] pandas/util/testing.py:1054 author=jbrockmendel reply=false subj=line side=RIGHT cid=221804860
Possibly add corresponding case to `tm.assert_equal`?

## [98] pandas/util/testing.py:1054 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=224721889
Should we rather add a general `assert_array_equal` which can deal with all our own Array classes? 
(but again, not necessarily for this PR)

## [108] pandas/util/testing.py:1054 author=TomAugspurger reply=true subj=line side=RIGHT cid=224804924
Are people opposed to singledispatch?

```python
In [1]: import pandas as pd

In [2]: import pandas.util.testing as tm

In [3]: from functools import singledispatch

In [4]: @singledispatch
   ...: def assert_equal(left, right, **kwargs):
   ...:     pass  # probably raise for unidentified type

In [5]: @assert_equal.register(pd.Series)
   ...: def _(left, right, **kwargs):
   ...:     return tm.assert_series_equal(left, right, **kwargs)

In [6]: @assert_equal.register(pd.DataFrame)
   ...: def _(left, right, **kwargs):
   ...:     return tm.assert_frame_equal(left, right, **kwargs)
```

this would let 3rd part EAs register their own `assert_my_EA_equal` checks, and we can use them without having to know about them ahead of time.

## [109] pandas/util/testing.py:1054 author=TomAugspurger reply=true subj=line side=RIGHT cid=224805057
(again, not for this PR, and I think singledispatch is new in Py3, so not till the new year).
