package mt.taptime.app

import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * A RESULT AFTER THE SCREEN IS GONE IS DROPPED (re-review, 2026-11-18): the position reading of a
 * press answering after Back reached a shut executor and its RejectedExecutionException ended the
 * app. LateWork is MainActivity's only way to the worker.
 */
class LateWorkTest {
    @Test
    fun aShutWorkerDropsTheLateResultInsteadOfThrowing() {
        val worker = Executors.newSingleThreadExecutor()
        val ran = AtomicInteger()
        assertTrue(LateWork.submit(worker, gone = false) { ran.incrementAndGet() })
        worker.shutdownNow()
        worker.awaitTermination(5, TimeUnit.SECONDS)
        // CONTROL: the executor itself throws here -- what crashed the app.
        val threw = try {
            worker.execute { }
            false
        } catch (_: java.util.concurrent.RejectedExecutionException) {
            true
        }
        assertTrue(threw)
        assertFalse("dropped, no throw", LateWork.submit(worker, gone = false) { ran.incrementAndGet() })
        assertEquals(1, ran.get())
    }

    @Test
    fun aGoneScreenSubmitsNothing() {
        val worker = Executors.newSingleThreadExecutor()
        val ran = AtomicInteger()
        assertFalse(LateWork.submit(worker, gone = true) { ran.incrementAndGet() })
        worker.shutdown()
        worker.awaitTermination(5, TimeUnit.SECONDS)
        assertEquals(0, ran.get())
    }

    @Test
    fun theActivityReachesTheWorkerOnlyThroughLateWork() {
        val main = java.io.File(System.getProperty("taptime.mainSrc")!!, "kotlin/mt/taptime/app/MainActivity.kt").readText()
        assertEquals("worker.execute / submit called directly", 0, Regex("""\bworker\.(execute|submit)\s*\(""").findAll(main).count())
        assertEquals(2, Regex("""\bLateWork\.submit\(worker,""").findAll(main).count())
        assertTrue("onDestroy takes back the position asks", Regex("""override fun onDestroy\(\) \{[^}]*location\.cancel\(\)""").containsMatchIn(main))
    }
}
