package mt.taptime.app

import java.util.concurrent.ExecutorService
import java.util.concurrent.RejectedExecutionException

// A RESULT THAT ARRIVES AFTER THE SCREEN IS GONE IS DROPPED, NEVER A CRASH (re-review, 2026-11-18).
// MainActivity.onDestroy shuts its worker down; a callback that comes later -- the position reading
// of a press answering after Back on API 26-30 -- used to hand its work to the shut executor, whose
// RejectedExecutionException ended the app on the main thread. Every hand-over to the worker goes
// through here: nothing is submitted once the activity is gone, and a refusal is swallowed. What is
// dropped is a press whose screen no longer exists; nothing was sent for it.
object LateWork {
    /** Submits [task] unless [gone]; false when it was not run (gone, or the executor shut). */
    fun submit(executor: ExecutorService, gone: Boolean, task: Runnable): Boolean {
        if (gone || executor.isShutdown) return false
        return try {
            executor.execute(task)
            true
        } catch (_: RejectedExecutionException) {
            false
        }
    }
}
