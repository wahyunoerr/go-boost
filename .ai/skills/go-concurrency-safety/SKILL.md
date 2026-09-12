# Skill: Go Concurrency & Goroutine Safety

Use this skill when implementing concurrent code, worker pools, background tasks, or caching in Go.

## Rules & Patterns
1. **Never leak goroutines**: Every spawned goroutine must have a deterministic termination path via \x60ctx.Done()\x60 or channel close.
2. **Use errgroup for fan-out / fan-in**:
   \x60\x60\x60go
   g, ctx := errgroup.WithContext(ctx)
   for _, item := range items {
       item := item
       g.Go(func() error {
           return process(ctx, item)
       })
   }
   if err := g.Wait(); err != nil {
       return err
   }
   \x60\x60\x60
3. **Mutex Hygiene**: Always \x60defer mu.Unlock()\x60 immediately after \x60mu.Lock()\x60.
4. **Channel Ownership**: The sender/producer owns the channel and is the only one responsible for closing it.
