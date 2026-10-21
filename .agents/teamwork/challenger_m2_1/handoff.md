# Milestone 2 Adversarial Challenge Report: Crash, Rollback & Resumability (AC1, AC2, AC3)

**Agent**: `teamwork_preview_challenger` (challenger_m2_1)  
**Working Directory**: `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_1`  
**Milestone**: Milestone 2 (Resumability, Batch Checkpointing & Dead-Letter Isolation)  
**Authoritative References**: `ORIGINAL_REQUEST.md`, `PROJECT.md`, `DISPATCH.md`  
**Verdict**: **`APPROVE`**

---

## 1. Observation

### 1.1 Evaluated Architecture & Source Code
The synchronization engine and SQLite storage layer were inspected to evaluate AC1, AC2, and AC3:
- `pkg/sync/engine.go:252-309`: `processBatchTx` wraps all product upserts (`executeRecordsTx`), batch checkpoints (`storage.RecordBatchCheckpointTx`), and cumulative job counters (`storage.UpdateJobProgressTx`) in an explicit SQLite transaction `tx`. In-memory mutations (`job.CommittedBatchIndex`, `job.Summary`) only execute upon `tx.Commit()` success.
- `pkg/sync/engine.go:313-377`: `executeJob` consumes stream batches sequentially, dropping previously committed batches (`batch.BatchIndex <= job.CommittedBatchIndex`), enforcing strict sequence monotonicity (`batch.BatchIndex > job.CommittedBatchIndex+1` triggers error), handling mid-batch cancellation via `markInterrupted(job)`, and only transitioning `job.Status` to `COMPLETED` when all stream batches finish.
- `pkg/sync/engine.go:401-464`: `RunJob` enforces mutual exclusion per job ID via `e.activeJobs[jobID]`, initializes or resumes `sync_jobs`, and transitions status to `RUNNING`.
- `pkg/sync/engine.go:466-516`: `ResumeJob` validates job existence, rejects completed jobs with `ErrJobAlreadyCompleted`, transitions interrupted jobs to `RUNNING`, and resumes execution directly from the checkpoint.
- `pkg/sync/engine.go:518-530`: `RecoverOnStartup` transitions stale `RUNNING` or `PENDING` jobs to `INTERRUPTED`.
- `pkg/storage/sqlite.go:50-83`: Embedded SQLite in WAL mode (`modernc.org/sqlite@v1.36.0`) configured with `_pragma=busy_timeout(5000)`, `_pragma=journal_mode(WAL)`, `_pragma=foreign_keys(ON)`, `_txlock=immediate`, and connection pooling serialization (`SetMaxOpenConns(1)`).

### 1.2 Adversarial Test Suite Implemented
An adversarial challenge suite comprising 8 attack vectors was implemented in `/Users/png/01. Project/coderpush/pkg/sync/engine_resumability_challenger_test.go`:
1. `TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle`: Stresses 20 batches (200 records, high collision across 25 SKUs) subjected to 4 mid-stream crashes (at batches 3, 8, 14, 18) simulating both graceful cancellations and ungraceful SIGKILL power loss, reopening fresh SQLite storage handles from disk, and asserting 100% catalog and metric equality against an independent in-memory Ground-Truth Oracle.
2. `TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift`: Resumes a job by feeding previously committed batches modified with altered names, prices, and timestamps; asserts the engine skips them without executing redundant writes or corrupting catalog state.
3. `TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback`: Seeds 10 baseline items, then executes a 40-record batch with 10 existing updates, 20 new items, and 10 corrupt records, abruptly canceling context mid-batch; asserts 0 partial updates, 0 new items, 0 dead-letter issues, and 0 checkpoints survive in SQLite.
4. `TestChallenger_AC3_LifecycleFSM_StrictTransitions`: Seeds jobs in `RUNNING`, `PENDING`, `INTERRUPTED`, `COMPLETED`, and `FAILED` states; asserts `RecoverOnStartup` recovers only `RUNNING` and `PENDING`, completed jobs reject resumption, premature stream closures remain `INTERRUPTED`, and `COMPLETED` is only reached when all batches commit.
5. `TestChallenger_AC2_SequenceGapRejection`: Emits a stream skipping a batch index (batch 0, 1, then 3); asserts engine rejects the gap, leaves batch 3 uncommitted, and marks the job `INTERRUPTED`.
6. `TestChallenger_Concurrency_SingleActiveJobMutex`: 20 concurrent goroutines race to resume the same job; asserts exactly 1 succeeds and 19 are rejected with `ErrJobAlreadyRunning`.
7. `TestChallenger_AC1_WAL_AbruptProcessKill_OrphanFrameRecovery`: Writes uncommitted frames to SQLite WAL and closes handle abruptly; verifies WAL recovery cleanly wipes orphan writes and allows seamless resumption.
8. `TestChallenger_AC2_EmptyBatchResumptionAcrossCrash`: Verifies 0-record batches advance checkpoint monotonically and support crash resumption.

### 1.3 Verbatim Test Execution Results
1. **Multi-Iteration Race Test (`go test -v -race -count=5 ./pkg/sync -run "TestChallenger_|TestAC1_|TestAC2_|TestAC3_"`)**:
   ```
   === RUN   TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle
   --- PASS: TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle (0.36s)
   === RUN   TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift
   --- PASS: TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift (0.04s)
   === RUN   TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback
   --- PASS: TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback (0.09s)
   === RUN   TestChallenger_AC3_LifecycleFSM_StrictTransitions
   --- PASS: TestChallenger_AC3_LifecycleFSM_StrictTransitions (0.18s)
   === RUN   TestChallenger_AC2_SequenceGapRejection
   --- PASS: TestChallenger_AC2_SequenceGapRejection (0.03s)
   === RUN   TestChallenger_Concurrency_SingleActiveJobMutex
   --- PASS: TestChallenger_Concurrency_SingleActiveJobMutex (0.02s)
   === RUN   TestChallenger_AC1_WAL_AbruptProcessKill_OrphanFrameRecovery
   --- PASS: TestChallenger_AC1_WAL_AbruptProcessKill_OrphanFrameRecovery (0.03s)
   === RUN   TestChallenger_AC2_EmptyBatchResumptionAcrossCrash
   --- PASS: TestChallenger_AC2_EmptyBatchResumptionAcrossCrash (0.03s)
   === RUN   TestAC1_CrashMidBatch_RollbackAndDurability
   --- PASS: TestAC1_CrashMidBatch_RollbackAndDurability (0.06s)
   === RUN   TestAC2_ResumeJob_ContinuesDirectlyFromUncommitted
   --- PASS: TestAC2_ResumeJob_ContinuesDirectlyFromUncommitted (0.03s)
   === RUN   TestAC3_StartupRecovery_StateTransitionLifecycle
   --- PASS: TestAC3_StartupRecovery_StateTransitionLifecycle (0.03s)
   [Iterations 2-5 repeated identically]
   PASS
   ok  	coderpush/pkg/sync	6.055s
   ```
   - 55 test executions (11 test suites x 5 iterations) PASSED with 0 data races.
2. **Lint & Formatting Verification**:
   - `go vet ./pkg/sync/...`: Exit code 0, 0 errors.
   - `gofmt -s -l pkg/sync/engine_resumability_challenger_test.go`: Clean (0 output).
3. **Statement Coverage (`go tool cover -func=coverage.out`)**:
   - `pkg/sync`: 94.2% statement coverage.

---

## 2. Logic Chain

1. **AC1 (Mid-Batch Interruption & Atomic Rollback)**:
   - Direct observation: In `TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback` and `TestAC1_CrashMidBatch_RollbackAndDurability`, a context cancellation mid-batch triggers `tx.Rollback()`.
   - Direct observation: Querying SQLite after cancellation shows 0 uncommitted products inserted (`SKU-NEW-*`), 0 uncommitted updates applied (`SKU-PRE-*` retained original price and name), 0 dead-letter issues logged, and no checkpoint row created for the uncommitted batch index.
   - Direct observation: Prior committed batches (Batch 0) remained 100% durable and intact.
   - Inferences: The batch boundary is strictly transactional. Partial batch writes never leak into storage. AC1 is satisfied.

2. **AC2 (Idempotent Resumption Without Re-Processing)**:
   - Direct observation: In `TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift`, when previously committed batches are re-submitted with poisoned payloads, `executeJob` skips them at line 330 (`if batch.BatchIndex <= job.CommittedBatchIndex { continue }`). The catalog products retain their original values, their `updated_at` timestamps are unchanged, and cumulative job summary counters do not double-count.
   - Direct observation: In `TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle`, across 4 sequential crashes and restarts, replaying all 20 batches resulted in exactly 20 checkpoints in `batch_checkpoints` (0 duplicates), and the final catalog and counters matched the theoretical Ground-Truth Oracle with zero drift.
   - Inferences: Resumption starts directly from `committed_batch_index + 1` without re-processing prior batches. AC2 is satisfied.

3. **AC3 (Lifecycle State Transitions & Crash Recovery)**:
   - Direct observation: In `TestChallenger_AC3_LifecycleFSM_StrictTransitions`, jobs left in `RUNNING` or `PENDING` status due to simulated crash are converted to `INTERRUPTED` by `RecoverOnStartup`.
   - Direct observation: `completed_at` is set to `NULL` during recovery and premature stream closures, and is only populated when all batches ($0 \dots \text{TotalBatches}-1$) have committed without error.
   - Direct observation: Attempting to resume or run an already completed job returns `ErrJobAlreadyCompleted`.
   - Inferences: Incomplete updates are never reported as complete, stale running jobs are cleanly recovered on startup, and completion transitions are monotonically guarded. AC3 is satisfied.

---

## 3. Caveats

- **Scope Boundary**: This evaluation focused strictly on AC1, AC2, and AC3 (crash, rollback, and resume lifecycle). Invariant D / AC4 (corrupt record dead-letter quarantine) is concurrently evaluated by peer challenger `challenger_m2_2`.
- **Single-Writer SQLite Locking**: Concurrency tests verify that multiple goroutines within the same process are serialized by `SyncEngine.mu` and `activeJobs`. Multi-process deployments rely on SQLite WAL file locks with `busy_timeout=5000`.

---

## 4. Conclusion

The implementation of AC1, AC2, and AC3 in Milestone 2 is robust, resilient to abrupt termination, and provably crash-safe:
- Mid-batch interruptions guarantee 100% atomic rollback without data corruption.
- Resumption correctly bypasses committed batches and completes without catalog drift.
- Job lifecycle FSM transitions strictly maintain status integrity across restarts.

Explicit Verdict: **`APPROVE`**

---

## 5. Verification Method

To independently execute and verify the adversarial challenger test suite:

1. **Run Full Adversarial Challenger Suite (5 iterations with race detector)**:
   ```bash
   go test -v -race -count=5 ./pkg/sync -run "TestChallenger_|TestAC1_|TestAC2_|TestAC3_"
   ```
   *Expected Result*: All 55 test runs PASS cleanly in ~6 seconds with 0 data races.

2. **Run Individual Adversarial Tests**:
   ```bash
   # Multi-crash chaos with ground-truth oracle
   go test -v -race ./pkg/sync -run "TestChallenger_AC1_AC2_MultiCrashChaosStream_GroundTruthOracle"

   # Poisoned replay attack zero-drift test
   go test -v -race ./pkg/sync -run "TestChallenger_AC2_PoisonedReplayAttack_ZeroCatalogDrift"

   # Mid-batch partial transaction atomicity
   go test -v -race ./pkg/sync -run "TestChallenger_AC1_MidBatchPartialTransaction_AbsoluteRollback"

   # Lifecycle FSM transitions
   go test -v -race ./pkg/sync -run "TestChallenger_AC3_LifecycleFSM_StrictTransitions"
   ```
   *Expected Result*: All tests PASS.

3. **Code Formatting & Lint Check**:
   ```bash
   go vet ./pkg/sync/... && gofmt -s -l pkg/sync/engine_resumability_challenger_test.go
   ```
   *Expected Result*: 0 errors, 0 output lines.
