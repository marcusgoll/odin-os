# Phase 17c Bootstrap Locking Plan

1. Add focused `bootstrap.Load` concurrency tests for shared fresh runtime roots.
2. Implement an `ODIN_ROOT`-scoped bootstrap lock around the shared bootstrap path.
3. Make migration application re-check schema state after lock acquisition and inside the migration transaction.
4. Add operator docs for bootstrap locking and a short audit note documenting the resolved race.
5. Run focused bootstrap tests, then full verification.
