package main

import (
 "encoding/json"
 "os"
 "path/filepath"
 "strings"
)
func restoreCheckpoints(){ entries,_:=os.ReadDir(".checkpoints"); for _,e:=range entries { if e.IsDir()||!strings.HasSuffix(e.Name(),".json"){continue}; b,err:=os.ReadFile(filepath.Join(".checkpoints",e.Name())); if err!=nil{continue}; var j ReviewJob; if json.Unmarshal(b,&j)==nil { if j.Status=="running"||j.Status=="queued" {j.Status="failed";j.Error="服务重启后任务已暂停，可依据 trace 重试"}; jobs.Lock(); jobs.m[j.ID]=&j; jobs.Unlock() } } }
