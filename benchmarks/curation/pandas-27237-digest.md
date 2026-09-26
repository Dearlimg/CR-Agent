# digest pandas-27237 : 168 anchored candidates (of 256 total)

## [129] doc/source/getting_started/basics.rst:1815 author=jreback reply=false subj=line side=RIGHT cid=368745369
can you show s1, then show the sorting in another ipython block

## [146] doc/source/getting_started/basics.rst:1815 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=370577401
> then show the sorting in another ipython block

It doesn't need to be in another ipython block, then get nicely shown under each other also within the same ipython block?

## [224] doc/source/user_guide/basics.rst:1789 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407341453
```suggestion
function to apply to the index being sorted. For `MultiIndex` objects,
```

## [240] doc/source/user_guide/basics.rst:1803 author=jreback reply=false subj=line side=RIGHT cid=415383536
can you show s1 here, then use a new ipython block for the sorting examples, its *much* easier to read

## [243] doc/source/user_guide/basics.rst:1804 author=jreback reply=false subj=line side=RIGHT cid=415383670
add a link tot he basics.sort_value_key section

## [190] doc/source/user_guide/basics.rst:1819 author=jreback reply=false subj=line side=RIGHT cid=405695181
can you add the same example that you have for DataFrame below int he whatsnew

## [208] doc/source/user_guide/basics.rst:1860 author=jreback reply=false subj=line side=RIGHT cid=407256866
show a multi-index example here (and not in whatsnew)

## [19] doc/source/whatsnew/v1.0.0.rst:154 author=jreback reply=false subj=line side=RIGHT cid=352381284
add an example in the appropriate user docs section as well using key

## [50] doc/source/whatsnew/v1.0.0.rst:154 author=TomAugspurger reply=true subj=line side=RIGHT cid=354984462
This will probably go in `doc/source/getting_started/basics.rst`.

## [63] doc/source/whatsnew/v1.0.0.rst:154 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359483773
I added this to the inline docs in `core/generic.py`. I can add it to `doc/source/getting_started/basics.rst`, but it didn't seem as natural there.

## [130] doc/source/whatsnew/v1.0.0.rst:190 author=jreback reply=false subj=line side=RIGHT cid=368745467
can you add a reference to the docs.

also add the issue number here.

## [150] doc/source/whatsnew/v1.0.0.rst:190 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371581277
Sorry this week has been crazy. How do I reference the docs here?

## [114] doc/source/whatsnew/v1.0.0.rst:200 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363187157
```suggestion
:meth:`DataFrame.sort_values`, :meth:`DataFrame.sort_index`, :meth:`Series.sort_values`,
```

(it's twice series.sort_index)

## [78] doc/source/whatsnew/v1.0.0.rst:214 author=WillAyd reply=false subj=line side=RIGHT cid=359546891
Not sure what happened here but can you revert all of this indentation?

## [80] doc/source/whatsnew/v1.0.0.rst:214 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359561888
fixed, must have happened by mistake in some formatting change. 

## [98] doc/source/whatsnew/v1.0.0.rst:225 author=jreback reply=false subj=line side=RIGHT cid=361989842
also i think its worthwhile to add a sub-section to showcase this (e.g. put above other enhancements)

## [99] doc/source/whatsnew/v1.0.0.rst:225 author=jreback reply=true subj=line side=RIGHT cid=361989920
show a simple example, then point to the docs pages

## [101] doc/source/whatsnew/v1.0.0.rst:225 author=jacobaustin123 reply=true subj=line side=RIGHT cid=362093570
I added an example and then said to "see examples and documentation in :meth:`DataFrame.sort_values`".

## [184] doc/source/whatsnew/v1.1.0.rst:47 author=WillAyd reply=false subj=line side=RIGHT cid=404917268
Just for the sake of explicitness, can you add an example here for DataFrame along with Series? I think helpful to highlight the direction in which the sort is applied

## [185] doc/source/whatsnew/v1.1.0.rst:47 author=jacobaustin123 reply=true subj=line side=RIGHT cid=404979151
Added an example for a DataFrame and updated the What's New. 

## [192] doc/source/whatsnew/v1.1.0.rst:47 author=jreback reply=true subj=line side=RIGHT cid=405704992
add a reference to the new sort_key reference

## [226] doc/source/whatsnew/v1.1.0.rst:47 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407342869
```suggestion
to each column of a DataFrame before sorting is performed (:issue:`27237`). See
```

Apart from the typo, I find "each column" a bit confusing, as it is of course only applied to those columns that are used for sorting?

## [255] doc/source/whatsnew/v1.1.0.rst:47 author=jreback reply=false subj=line side=RIGHT cid=415947476
can you also add the orginal issue number here (or rather replace this issue number )

## [206] doc/source/whatsnew/v1.1.0.rst:48 author=jreback reply=false subj=line side=RIGHT cid=407256816
you don't need to mention sort_index here.

## [209] doc/source/whatsnew/v1.1.0.rst:48 author=jreback reply=true subj=line side=RIGHT cid=407257087
you can mention in the user docs if you want. that is much more important that this here and should be a superset.

## [227] doc/source/whatsnew/v1.1.0.rst:57 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407344174
```suggestion
Note how this is sorted with capital letters first. If we apply the :meth:`Series.str.lower` method, we get
```

(then it should become a link)

## [245] doc/source/whatsnew/v1.1.0.rst:58 author=jreback reply=false subj=line side=RIGHT cid=415383901
same show s, then show the sorting in *another* blcok

## [225] doc/source/whatsnew/v1.1.0.rst:61 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407342708
```suggestion
   s.sort_values(key=lambda x: x.str.lower())
```

## [186] doc/source/whatsnew/v1.1.0.rst:67 author=WillAyd reply=false subj=line side=RIGHT cid=404984520
```suggestion
   df = pd.DataFrame({'a' : ['C', 'C', 'a', 'a', 'B', 'B'], 'b': [1, 2, 3, 4, 5, 6]})
```

## [187] doc/source/whatsnew/v1.1.0.rst:68 author=WillAyd reply=false subj=line side=RIGHT cid=404984712
```suggestion
   df.sort_values(by=['a', 'b'], key=lambda col: col.str.lower() if col.name == 'a' else -col)
```

## [205] doc/source/whatsnew/v1.1.0.rst:71 author=jreback reply=false subj=line side=RIGHT cid=407256560
can you just use col.str.lower() this is really odd formatting and confusing for an example

## [207] doc/source/whatsnew/v1.1.0.rst:75 author=jreback reply=false subj=line side=RIGHT cid=407256843
i would move this comment to the sort_key section

## [244] doc/source/whatsnew/v1.1.0.rst:80 author=jreback reply=false subj=line side=RIGHT cid=415383770
show df, then in another block show the sorting

## [194] pandas/core/arrays/categorical.py:1564 author=jreback reply=false subj=line side=RIGHT cid=406855045
you might want to give a sample of the key function here

## [201] pandas/core/arrays/categorical.py:1564 author=jacobaustin123 reply=true subj=line side=RIGHT cid=406949964
Thinking more about this, I'm inclined to remove the key function here. I can't think of a use case for `Categorical` because it supports basically no vectorized operations and `cat.map` doesn't work well with key sorting because it just transforms the `codes`.

## [115] pandas/core/arrays/categorical.py:1574 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363187852
```suggestion
        key : callable, default None
```

## [51] pandas/core/arrays/categorical.py:1576 author=TomAugspurger reply=true subj=line side=RIGHT cid=354985000
@ja3067 can you add this. `.. versionadded:: 1.0.0`.

## [139] pandas/core/arrays/categorical.py:1579 author=TomAugspurger reply=false subj=line side=RIGHT cid=369097109
Is this description correct? Is `key` applied to every value or to the array?

## [140] pandas/core/arrays/categorical.py:1579 author=jacobaustin123 reply=true subj=line side=RIGHT cid=369120192
Did you change the behavior for `Categorical`? This looks like the way I implemented it originally. 

## [142] pandas/core/arrays/categorical.py:1579 author=jacobaustin123 reply=true subj=line side=RIGHT cid=369131107
It has the new behavior now, we just hadn't updated the documentation. 

## [160] pandas/core/arrays/categorical.py:1581 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371596911
No we don't enforce types of the object after applying the key function. 

## [20] pandas/core/arrays/categorical.py:1582 author=jreback reply=false subj=line side=RIGHT cid=352381301
can you type the added keyword (bonus for typing other things)

## [43] pandas/core/frame.py:4717 author=jacobaustin123 reply=true subj=line side=RIGHT cid=352409399
This wasn't typed because of a mypy issue I can't figure out how to fix. The generic.py `sort_values` function has the `by` parameter defaulted to `None`, while `frame.py` doesn't have the default. This causes a mypy issue because the inheritance pattern is kind of screwed up, but leaving key untyped seemingly confuses mypy enough so it doesn't raise a warning. For reference the error is `Signature of "sort_values" incompatible with supertype "NDFrame"`. I'm inclined to `type : ignore` it, but maybe there's a solution?

## [49] pandas/core/frame.py:4717 author=jacobaustin123 reply=true subj=line side=RIGHT cid=354952869
@jreback Also this – I don't see a way around the basic incompatibility of the `Series` and `NDFrame` `sort_values` functions. 

## [52] pandas/core/frame.py:4717 author=TomAugspurger reply=true subj=line side=RIGHT cid=354985881
cc @simonjayhawkins on the typing question.

## [53] pandas/core/frame.py:4717 author=TomAugspurger reply=true subj=line side=RIGHT cid=354986410
Can you add docs on what is passed to the `key` function? Is it the `Series`, obtained from `self[by]`?

## [56] pandas/core/frame.py:4717 author=WillAyd reply=true subj=line side=RIGHT cid=355151998
Yea this seems tricky to resolve; I think OK as a follow up

## [57] pandas/core/frame.py:4717 author=WillAyd reply=true subj=line side=RIGHT cid=355152068
Actually if you can type `key` and ignore error code specific to inheritance would be best. Can do this as of mypy 0.730

http://mypy-lang.blogspot.com/2019/09/mypy-730-released.html

## [60] pandas/core/frame.py:4717 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359478339
I added #type: ignore[override] to ignore this. However, the current release of flake8 raises spurious errors for this syntax, so we will have to add `# NOQA` on those two lines until the next `pyflakes` release (it has been fixed in master, see https://github.com/PyCQA/pyflakes/issues/475). 

## [8] pandas/core/frame.py:4718 author=WillAyd reply=false subj=line side=RIGHT cid=349181817
Can you post the error(s) these were causing? We typically try to avoid ignore

## [11] pandas/core/frame.py:4718 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349224112
The error is `pandas/core/frame.py:4718: error: Signature of "sort_values" incompatible with supertype "NDFrame"`. The problem seems to be caused by the `by` argument in `sort_values`. Basically, it's an optional first argument in `core/generic.py` but it's required in `core/frame.py`, and it doesn't exist in `core/series.py`, even though `Frame` and `Series` inherit from `NDFrame`. I don't see any easy way to fix this. In fact, I'm surprised it hasn't set off mypy before this change.

## [14] pandas/core/frame.py:4718 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349887671
@WillAyd what do you think? The problem is that `sort_values` in `generic.py` uses

```python
 4149     def sort_values(
 4150         self,
 4151         by=None,
 4152         axis=0,
 4153         ascending=True,
 4154         inplace=False,
 4155         kind="quicksort",
 4156         na_position="last"
 4157     ):
```

and in `core/frame.py` it is

```python
4718     def sort_values(
4719         self,
4720         by,
4721         axis=0,
4722         ascending=True,
4723         inplace=False,
4724         kind="quicksort",
4725         na_position="last",
4726         key: Optional[Callable] = None
4727     ):
```

so `by` has a default in the base class, but no default in the child. There also isn't ven a `by` keyword in `core/series.py`. This isn't related to a change I made. It just seems to be a kind of unfortunate inheritance issue related to the different needs of `series` and `frame`.

## [15] pandas/core/frame.py:4718 author=WillAyd reply=true subj=line side=RIGHT cid=349893923
Does fixing it in the base class resolve? I think preferable to all of the type ignores if that is all that is required

## [16] pandas/core/frame.py:4718 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349932149
@WillAyd I don't think we can fix it by changing the base class. The `generic.py` `sort_values` has `by` as the first argument, while the `series.py` `sort_values` doesn't even have `by` as an argument (for obvious reasons). `frame.py` has it has a first argument. I don't see any change that can fix it – the APIs are just fundamentally different. 

## [17] pandas/core/frame.py:4718 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349932367
The one fix would just be to make `sort_values` in the NDFrame base class use `def sort_values(*args, **kwargs)`, and let child classes redefine it.

## [18] pandas/core/frame.py:4718 author=jacobaustin123 reply=true subj=line side=RIGHT cid=351886961
@WillAyd I was able to make some changes to the child classes that fixed the type errors without any major changes. Type annotations are a little less verbose, but there are no `type:ignore` calls anywhere.

## [5] pandas/core/frame.py:4856 author=WillAyd reply=false subj=line side=RIGHT cid=318690472
Just annotate this as `Optional[Callable]` (applicable in a few places)

## [116] pandas/core/frame.py:4875 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363188110
```suggestion
                "Key sorting for DataFrame.sort_values has not been implemented"
```

## [1] pandas/core/frame.py:4883 author=jreback reply=false subj=line side=RIGHT cid=300730391
this is not a good place for this kind of change at all, rather pass things thru to the functions actually doing the work (lexsort_indexer).

## [3] pandas/core/frame.py:4883 author=jacobaustin123 reply=true subj=line side=RIGHT cid=300842813
Ok. I can pass most things to lexsort_indexer, but sort_index is complicated for multi-indexes. I feel like the map function should be applied to each multi-index tuple, but the sorting functions are generally applied to the codes and not the values themselves. I can change the sort_values functions to defer work to lexsort_indexer or nargsort, but I think we should keep using index.map(foo) for the sort_index functions. 

## [126] pandas/core/frame.py:4902 author=jreback reply=false subj=line side=RIGHT cid=366145493
should create something in pandas._typing for this, maybe SortByKey

## [133] pandas/core/frame.py:4902 author=jreback reply=true subj=line side=RIGHT cid=368745838
can you create this (ok to put in pandas._typing)

## [196] pandas/core/frame.py:4924 author=jreback reply=false subj=line side=RIGHT cid=406855481
hmm, can we handle this inside `ensure_key_mapped`, e.g. by passing an optional `level` arg?

## [138] pandas/core/frame.py:4934 author=TomAugspurger reply=true subj=line side=RIGHT cid=369096700
Yeah. This will be solved when the docstring NDFrame.sort_values is just moved to DataFrame.sort_values.

## [82] pandas/core/frame.py:4937 author=TomAugspurger reply=false subj=line side=RIGHT cid=359882783
@simonjayhawkins is this the best way to do this ignore?

## [88] pandas/core/frame.py:4937 author=simonjayhawkins reply=true subj=line side=RIGHT cid=360016855
my preference would be to not include the codes until the flake issues are resolved, see #29197

## [92] pandas/core/frame.py:4937 author=jacobaustin123 reply=true subj=line side=RIGHT cid=360103166
@TomAugspurger Do you think we should just `# type: ignore` it unconditionally, and then change it once `pyflakes` has a new release?

## [103] pandas/core/frame.py:4937 author=WillAyd reply=true subj=line side=RIGHT cid=362679441
I would go with @simonjayhawkins here if no strong objections against; we have #30451 open anyway

## [2] pandas/core/frame.py:4980 author=jacobaustin123 reply=true subj=line side=RIGHT cid=300837896
Would you just like `key : typing.Callable`? And for Index support, do you mean the `Index.sort_values()` function? 

## [147] pandas/core/frame.py:5034 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=370579064
If this is a vectorized function, I suppose it is giving all values, not only the non-missing ones?

## [149] pandas/core/frame.py:5034 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=370582171
And the same comment apply to some of the other docstrings as well (I suppose a left-over from when it was a scalar function)

## [169] pandas/core/frame.py:5045 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=372248894
What happens if the index is a MultiIndex? Does the output still need to be of the same shape?

## [177] pandas/core/frame.py:5045 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=372391633
Ah, or I suppose the levels of the MultiIndex are given one by one to the function similarly as for DataFrame? (if so, that can be clarified in the docs)

## [181] pandas/core/frame.py:5045 author=jacobaustin123 reply=true subj=line side=RIGHT cid=372543448
At the moment, the entire `MultiIndex` is passed to the key function. The only comparison done is `len(initial) = len(final)`. We don't enforce anything beyond that. 

## [182] pandas/core/frame.py:5045 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=372615050
Are we OK with this? (I am not really sure myself) It seems a bit inconsistent with DataFrame wich gets done column by column. But of course it's also not a fully valid comparison. 

## [183] pandas/core/frame.py:5045 author=jacobaustin123 reply=true subj=line side=RIGHT cid=372617827
We can do it the other way. Per-level instead of the whole index. I mildly prefer that, and it would be easy to implement. 

## [158] pandas/core/generic.py:1605 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371596508
Because while most methods expect `_get_label_or_level_values` to return the underlying `arraylike`, we need the `Series` object itself to apply the keys functions to in `DataFrame::sort_values`. Otherwise we wouldn't be able to do things like `col.str.lower()`.

## [162] pandas/core/generic.py:1605 author=jreback reply=true subj=line side=RIGHT cid=371702302
no you dont - just try it, you can always wrap the return in a Series if you need
but this raw argument is pretty smelly and needs to go

## [148] pandas/core/generic.py:4077 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=370580077
It's not clear to me whether it gets the full dataframe, or only a subset of the dataframe for the columns the user specified to sort by.

## [151] pandas/core/generic.py:4077 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371583205
Only the subset of the columns selected in `by`. 

## [117] pandas/core/generic.py:4126 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363190341
```suggestion
        key : callable, default None
```

## [118] pandas/core/generic.py:4128 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363190573
Can you mention here that this is only implemented for Series?

## [166] pandas/core/generic.py:4145 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=371901615
Add an example with key here? (I assume that is the reason the 4th column was added?)

## [167] pandas/core/generic.py:4145 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371911999
Added. Seemed to have been removed at some point.

## [228] pandas/core/generic.py:4220 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407345276
```suggestion
        >>> df.sort_values(by='col4', key=lambda col: col.str.lower())
```

## [119] pandas/core/generic.py:4242 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363190701
```suggestion
        key : callable, default None
```

## [197] pandas/core/generic.py:5335 author=jreback reply=false subj=line side=RIGHT cid=406855884
hmm i think these have already been changed in master. can you avoid doing non-essential changes.

## [204] pandas/core/generic.py:5335 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407134347
Sorry I reverted these changes. Not sure how they were introduced. Probably black.

## [120] pandas/core/indexes/base.py:4207 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363190827
```suggestion
        key : callable, default None
```

## [6] pandas/core/indexes/base.py:4428 author=WillAyd reply=false subj=line side=RIGHT cid=318690731
Probably need to delete space after `key` to pass CI

## [79] pandas/core/indexes/base.py:4441 author=WillAyd reply=false subj=line side=RIGHT cid=359547730
Not a huge deal so no action required, but just a general comment  for large-is PRs better to leave things like this alone to minimize diff

## [170] pandas/core/indexes/base.py:4488 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=372249339
Does the output needs to be an index, or can it just be an array-like?

## [180] pandas/core/indexes/base.py:4488 author=jacobaustin123 reply=true subj=line side=RIGHT cid=372542482
In this case, yes. It has to return an Index. We could try to rewrap the arraylike in an `Index`, but that's kind of hard if it's supposed to be a `MultiIndex`, for example. I can just add an `if not isinstance(index, Index): index = Index(index)`.

## [104] pandas/core/indexes/datetimelike.py:172 author=WillAyd reply=false subj=line side=RIGHT cid=362679692
Is this required? Seems a little strange to add in a Mixin

## [105] pandas/core/indexes/datetimelike.py:172 author=jacobaustin123 reply=true subj=line side=RIGHT cid=362839111
@WillAyd this is a mypy issue I haven't been able to figure out. It seems like type annotations on the mixin raise errors since it detects that `error: "DatetimeIndexOpsMixin" has no attribute "_get_attributes_dict"`. I'm not sure what to do about this except add the assert error.

## [106] pandas/core/indexes/datetimelike.py:172 author=jacobaustin123 reply=true subj=line side=RIGHT cid=362846707
I've noticed that nothing in the `DatetimeIndexOpsMixin` is type annotated. Mypy does not seem to have a good system for handling them (see [here](https://stackoverflow.com/questions/51930339/how-do-i-correctly-add-type-hints-to-mixin-classes) or [here](https://github.com/python/mypy/issues/5837), for example). I would vote for not type annotating this function until we settle on a way to type annotate mixins generally. 

## [107] pandas/core/indexes/datetimelike.py:172 author=jacobaustin123 reply=true subj=line side=RIGHT cid=362846748
I've noticed that nothing in the `DatetimeIndexOpsMixin` is type annotated. Mypy does not seem to have a good system for handling them (see [here](https://stackoverflow.com/questions/51930339/how-do-i-correctly-add-type-hints-to-mixin-classes) or [here](https://github.com/python/mypy/issues/5837), for example). I would vote for not type annotating this function until we settle on a way to type annotate mixins generally. 

## [229] pandas/core/indexes/datetimelike.py:176 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407346544
We will actually need to check here if `idx` is still a DatetimeIndex? Because otherwise, I assume the below code won't work. 
For example, when doing `dtidx.sort_values(key=lambda x: x.month)`

## [231] pandas/core/indexes/datetimelike.py:176 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=407348254
I noticed below that `ensure_key_mapped` is actually converting back to the original type. Which is good in general, but for index means it converts back to the original Index subclass, while I think we should probably use the base class `Index(..)` constructor in that case.

But I am also fine to leave this as a follow-up enhancement.

## [232] pandas/core/indexes/datetimelike.py:176 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407539020
I fixed this. See what you think about the current version. Basically, .asi8 ignores the type, so it doesn't matter. This works as intended, but it's a _little_ bit of a hack.

## [234] pandas/core/indexes/datetimelike.py:176 author=jorisvandenbossche reply=true subj=line side=RIGHT cid=407637157
Can you add a test for this?

`asi8` returns None for the base Index class, so I don't think it will work for all cases. For example, assume you would sort a DatetimeIndex on the name of the month (a bit contrieved, but to show the point): `key=lambda idx: idx.month_name()`)

## [236] pandas/core/indexes/datetimelike.py:176 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407649942
Right that makes sense. Could we just always use the first branch for `return_indexer` instead of the second branch that uses `_simple_new`? I'm not sure what the whole section with `freq` is trying to do. Performance optimization? 

## [237] pandas/core/indexes/datetimelike.py:176 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407655745
As you mentioned above, this does work:

```python
    if not isinstance(result, type_of_values):  # recover from type error
        try:
            if isinstance(values, Index):
                result = Index(result)
            else:
                result = type_of_values(result)
        except TypeError:
            raise TypeError("User-provided `key` function returned an invalid type.")
```

The code is a little ugly, but it works for any kind of Index. Thoughts?

## [238] pandas/core/indexes/datetimelike.py:185 author=jacobaustin123 reply=false subj=line side=RIGHT cid=408195487
@jreback do you know what this second branch is here for? I'm tempted to make the `argsort` method the default since it's necessary for `key` sorting, unless it's an important optimization.

## [44] pandas/core/indexes/datetimelike.py:282 author=jacobaustin123 reply=true subj=line side=RIGHT cid=352409606
@jreback This is a mypy issue – if we type the `key` argument, mypy tries to type infer the function, and because the Mixin doesn't define `_get_attributes_dict`, for example, we get a mypy error `error: "DatetimeIndexOpsMixin" has no attribute "_get_attributes_dict"`, since `_get_attributes_dict` is defined in the `Index` class. I think we probably have to keep the Mixin untyped, or else annotate the class heavily to tell mypy a subclass will define these functions.

## [83] pandas/core/indexes/datetimelike.py:282 author=TomAugspurger reply=true subj=line side=RIGHT cid=359883289
can this just be `assert isinstance(self, Index)`?

Or does `self = cast(DatetimeIndex, self)`?

## [26] pandas/core/indexes/datetimelike.py:285 author=jreback reply=false subj=line side=RIGHT cid=352381484
I would wrap these up in an ``ensure_key_mapped(self, ey)`` and put this function in pandas/core/sorting.py as this is used in multiple places.

## [27] pandas/core/indexes/datetimelike.py:285 author=jreback reply=true subj=line side=RIGHT cid=352381516
i wouldn't use map either rather a think a list-comprehension is fine (you need to mask the null values)

## [45] pandas/core/indexes/datetimelike.py:285 author=jacobaustin123 reply=true subj=line side=RIGHT cid=352410100
I wanted to have an `ensure_key_mapped` function, but it has to work on a lot of different kinds of objects (indices, frames, series, numpy arrays, etc.) How would you handle this? Right now I have a function

```python
def ensure_key_mapped(values, key):
    from pandas.core.generic import NDFrame
    from pandas.core.index import Index

    if not key:
        return values
        
    elif isinstance(values, NDFrame) or isinstance(values, Index):
        return values.map(key, na_action='ignore')
    elif isinstance(values, list):
        return [key(value) for value in values]
    elif isinstance(values, np.ndarray):
        def map_f(values, f):
            return lib.map_infer_mask(values, f, isna(values).view(np.uint8))

        return map_f(values, key)
    else:
        raise TypeError(f"Could not map key to object of type {type(values)}")
```

that works, but it's a little janky. 

## [46] pandas/core/indexes/datetimelike.py:285 author=jreback reply=true subj=line side=RIGHT cid=352410678
list and ndarray branches can be combined 
you don’t need to use .map at all
so i think you can combined almost everything 

put this in place then keep trying to simplify 

## [47] pandas/core/indexes/datetimelike.py:285 author=jacobaustin123 reply=true subj=line side=RIGHT cid=352647953
@jreback is there a reason to avoid `.map`? It seems like it simplifies the implementation, since there's a lot of logic in `Index::map` and `IndexOpsMixin:_map_values` that I don't want to have to reproduce here. In fact, the logic at `core/base.py:1168` seems to be exactly what I want, and that gets called by `Series::map` and `Index::map`.

## [48] pandas/core/indexes/datetimelike.py:285 author=jacobaustin123 reply=true subj=line side=RIGHT cid=354952416
@jreback Everything else is done – I can try and change it to avoid map, but that seems like it means coding separate logic for all the different `Index` subclasses. 

## [59] pandas/core/indexes/datetimelike.py:285 author=jreback reply=true subj=line side=RIGHT cid=356061216
ok, if you can consolidate the code then will have a look

## [67] pandas/core/indexes/datetimelike.py:285 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359484656
@jreback take a look at the current consolidated version with `core/sorting.py` and `ensure_key_mapped`.

## [12] pandas/core/indexes/datetimelike.py:301 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349226350
Problem here is `pandas/core/indexes/datetimelike.py:301: error: "DatetimeIndexOpsMixin" has no attribute "_get_attributes_dict"
pandas/core/indexes/datetimelike.py:314: error: "DatetimeIndexOpsMixin" has no attribute "_simple_new"`. Adding the `key` argument causes this, even if no other changes are made. Maybe mypy is getting confused by some inheritance thing?

## [13] pandas/core/indexes/datetimelike.py:301 author=jacobaustin123 reply=true subj=line side=RIGHT cid=349238992
It can be fixed either by adding something like


```python3
if not isinstance(self, Index):
    raise TypeError("sort_values must be called on an Index object")
```

or by adding methods like `_get_attributes_dict` to the `DatetimeIndexOpsMixin` class.

## [198] pandas/core/indexes/multi.py:2193 author=jreback reply=false subj=line side=RIGHT cid=406856170
see my comments, i would rather do this *inside* ensure_key_mapped

## [218] pandas/core/internals/blocks.py:11 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407308193
Was failing CI checks. Fixed in master now.

## [121] pandas/core/series.py:2797 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363191045
```suggestion
        key : callable, default None
```

## [171] pandas/core/series.py:2835 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=372250722
The argsort requirement on the output seems very specific (maybe rather confusing for users than helpful). Maybe saying "array-like" is sufficient? 

BTW, it seems that a function that returns a list is also fine, so the requirement isn't even that strict.

## [29] pandas/core/series.py:2895 author=jreback reply=false subj=line side=RIGHT cid=352381548
blank line, add a line of expl what you are doing

## [122] pandas/core/series.py:3014 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363191164
```suggestion
        key : callable, default None
```

## [253] pandas/core/series.py:3043 author=jreback reply=false subj=line side=RIGHT cid=415794205
in followon you can use `extract_array` here

## [123] pandas/core/series.py:3102 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363191295
```suggestion
        >>> s = pd.Series([1, 2, 3, 4, 5, 6, 7, 8])
```

## [172] pandas/core/series.py:3167 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=372251538
I wouldn't give this example, as for that you should rather use `ascending=False` I think? 
Maybe just a similar example with the lower case strings (but where the strings are in the index here)?

## [211] pandas/core/sorting.py:287 author=jreback reply=false subj=line side=RIGHT cid=407257378
is this not wrapped at a higher level? if not, why not?

## [219] pandas/core/sorting.py:287 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407308339
I thought it made sense to add an optional `key` argument to `lexsort_indexer` and `nargsort`. That means any new methods that use these can add an optional `key` function if desired.

## [250] pandas/core/sorting.py:304 author=jreback reply=false subj=line side=RIGHT cid=415384829
Can you have a 1-line summary & then blank line, then sentences.

## [248] pandas/core/sorting.py:316 author=jreback reply=false subj=line side=RIGHT cid=415384614
rename this to level, for consistency with other apis

## [84] pandas/core/sorting.py:319 author=TomAugspurger reply=false subj=line side=RIGHT cid=359883815
A docstring here would be great to have.

## [220] pandas/core/sorting.py:322 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407308772
So this is tricky. I can't import MultiIndex into `sorting.py` because it creates a circular dependency that makes everything fail. I couldn't think of a way around this. 

## [230] pandas/core/sorting.py:324 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=407347105
Indentation is off here (4 spaces too much)

## [85] pandas/core/sorting.py:329 author=TomAugspurger reply=false subj=line side=RIGHT cid=359884679
Why do you need this closure? Can we call `lib.map_infer_mask` directly?

## [86] pandas/core/sorting.py:329 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359932775
Yes. Updated. The closure was just used to simplify the implementation in another internal function that used it. 

## [95] pandas/core/sorting.py:331 author=jreback reply=false subj=line side=RIGHT cid=361988416
can use ABCIndex here (and then can import from the top)

## [102] pandas/core/sorting.py:331 author=jacobaustin123 reply=true subj=line side=RIGHT cid=362093959
`ABCIndexClass` works, but not `ABCIndex`. That fails on `RangeIndex`. Is that OK?

## [157] pandas/core/sorting.py:337 author=jreback reply=false subj=line side=RIGHT cid=371594067
we don’t actually care about the types here right?
meaning if values is a Categoral
then result might be of might not be 

## [159] pandas/core/sorting.py:337 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371596705
No. We could enforce types if we wanted, but it hasn't seemed necessary.

## [161] pandas/core/sorting.py:337 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371597231
@TomAugspurger can you explain what the `TODO: should ensure_key_mapped convert to an array?` was referring to? `ensure_key_mapped` needs to return an Index so we can call methods like `sortlevel`. 

## [163] pandas/core/sorting.py:337 author=jreback reply=true subj=line side=RIGHT cid=371702983
> No. We could enforce types if we wanted, but it hasn't seemed necessary.

if we are not enforcing types (ok by me); then update the doc string (eg a Categorical is not required only a same length array-like)

## [164] pandas/core/sorting.py:337 author=jacobaustin123 reply=true subj=line side=RIGHT cid=371895480
Ok. The only problem is that it can violate the return type – we don't type annotate it, but the documentation specifies that it returns a `Categorical` or None. But I updated the documentation.

## [199] pandas/core/sorting.py:350 author=jreback reply=false subj=line side=RIGHT cid=406856513
I would move MultiIndex.apply_key here and make it a function that takes a MI and simply call this from ensure_key_mapper.

## [200] pandas/core/sorting.py:350 author=jacobaustin123 reply=true subj=line side=RIGHT cid=406948877
My reasoning for keeping this outside of `ensure_key_mapped` is simply that we want `ensure_key_mapped` to be a generic function that takes any object and tries to apply a key function to it. My feeling for the API is that calling functions should be responsible for handling any kind of preprocessing they want to do, instead of cluttering it with `isinstance` checks. I also though the `MultiIndex.apply_key` was a useful function, sort of like map that allows you to transform a `MultiIndex`. If you think this is best, I'm happy to do it though.

## [202] pandas/core/sorting.py:350 author=jreback reply=true subj=line side=RIGHT cid=406967396
I don think its a generally useful function and is very specific to this case. we want to move all unecessary logic out of the sorting functions.

## [203] pandas/core/sorting.py:350 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407012679
Ok I moved it to `sorting.py` and added a ton of documentation. And dealt with some crazy linting issues.

## [239] pandas/core/sorting.py:361 author=jacobaustin123 reply=false subj=line side=RIGHT cid=408196466
Is there a way to do this (construct a DataFrame along either row or column axis) that's better than transposing it like this?

## [214] pandas/core/sorting.py:390 author=jreback reply=false subj=line side=RIGHT cid=407257549
you don't need the else here at all, pls outdent these

## [124] pandas/core/sorting.py:391 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=363192637
Should there be some ExtensionArray support here?

## [222] pandas/core/sorting.py:400 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407309475
This was added because the original Issue that made us add keys wanted to use this `natsort` package to generate keys for DataFrames. It actually works, but it takes in an Index and produces a list of tuples. By adding this, it allows something that takes a `Series` or `Index` and returns a list or ndarray to potentially return successfully. I thought this was reasonably safe and it makes the method more flexible. 

## [217] pandas/core/sorting.py:403 author=jreback reply=false subj=line side=RIGHT cid=407257762
use from e, so you can see the original error message

## [221] pandas/core/sorting.py:403 author=jacobaustin123 reply=true subj=line side=RIGHT cid=407309011
Should I just avoid catching the error at all then? 

## [174] pandas/tests/frame/methods/test_sort_values.py:400 author=jorisvandenbossche reply=false subj=line side=RIGHT cid=372252463
Can you clean-up those remaining breakpoints?

## [108] pandas/tests/frame/methods/test_sort_values.py:521 author=jreback reply=false subj=line side=RIGHT cid=363115524
instead of commenting out can you just xfail them and refer to the issue number

## [110] pandas/tests/frame/methods/test_sort_values.py:522 author=jreback reply=false subj=line side=RIGHT cid=363120719
you can just xfail the entire class here; provide the message including the issue number IN the xfail reason); also ok with putting an xfail on each individual test instead.

do not skip tests.

## [111] pandas/tests/frame/methods/test_sort_values.py:522 author=jacobaustin123 reply=true subj=line side=RIGHT cid=363121536
Some of the tests are fixtured. They succeed for some of the fixture parameters, and fail for others (they succeed for the None key but they fail for the identity key). I didn't see how I could resolve that. 

## [89] pandas/tests/frame/test_sorting.py:22 author=simonjayhawkins reply=false subj=line side=RIGHT cid=360027401
can you use the comment as a docstring? see #19159

## [39] pandas/tests/frame/test_sorting.py:84 author=jreback reply=false subj=line side=RIGHT cid=352381750
move the fixtures to the very top of the file, give it a description

## [40] pandas/tests/frame/test_sorting.py:529 author=jreback reply=false subj=line side=RIGHT cid=352381796
can you add the issue number as a comment

## [42] pandas/tests/frame/test_sorting.py:529 author=jreback reply=false subj=line side=RIGHT cid=352381840
can you make a new test class to group the new tests

## [41] pandas/tests/frame/test_sorting.py:544 author=jreback reply=false subj=line side=RIGHT cid=352381814
what is the point of a dtype here? we have fixtures for this, but not sure what you are testing

## [74] pandas/tests/frame/test_sorting.py:544 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359486097
it didn't have a purpose. I removed the parametrize call.

## [54] pandas/tests/frame/test_sorting.py:558 author=TomAugspurger reply=false subj=line side=RIGHT cid=354987382
nitpick: missing an `s` on values. Rename to `test_sort_values_key` (here and elsewhere).

## [62] pandas/tests/frame/test_sorting.py:558 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359481068
fixed in `frame/test_sorting.py` and `series/test_sorting.py`. 

## [55] pandas/tests/frame/test_sorting.py:572 author=TomAugspurger reply=false subj=line side=RIGHT cid=354987539
Can you add a test here with multiple columns in `by`?

## [61] pandas/tests/frame/test_sorting.py:572 author=jacobaustin123 reply=true subj=line side=RIGHT cid=359480596
I added this under `test_sort_values_by_key` in `frame/test_sorting.py`. 

## [90] pandas/tests/indexing/multiindex/test_sorted.py:32 author=simonjayhawkins reply=false subj=line side=RIGHT cid=360030434
can you move the fixture in `pandas/tests/frame/test_sorting.py` into conftest.py and re-use? (may require a more descriptive name to avoid clashes.)

## [94] pandas/tests/indexing/multiindex/test_sorted.py:32 author=jacobaustin123 reply=true subj=line side=RIGHT cid=360106554
renamed the fixture `test_key` and moved to `conftest.py`. 

## [97] pandas/tests/series/conftest.py:36 author=jreback reply=false subj=line side=RIGHT cid=361988655
same as above, would be ok with just moving these fixtures to pandas/conftest.py (as they are the same)
