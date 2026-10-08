package dev.orbit.orbit_android

import com.google.protobuf.Timestamp
import orbit.v1.View.DeviceView
import orbit.v1.View.UsageView
import org.junit.Assert.*
import org.junit.Test

class PowerPolicyTest {
    @Test fun retriesBackOffToFiveMinutesAndResetAfterRecovery() {
        val backoff = RetryBackoff { 0 }
        assertEquals(listOf(15000L, 30000L, 60000L, 120000L, 240000L, 300000L, 300000L),
            List(7) { backoff.nextDelayMillis() })
        backoff.reset()
        assertEquals(15000L, backoff.nextDelayMillis())
    }
    @Test fun leaseRenewalDoesNotChangeContentButAmountDoes() {
        val original = DeviceView.newBuilder().setUsage(UsageView.newBuilder().setActualCostMicros(1000000))
            .setRetainUntil(Timestamp.newBuilder().setSeconds(100)).build()
        val renewed = original.toBuilder().setRetainUntil(Timestamp.newBuilder().setSeconds(200))
            .setUsage(original.usage.toBuilder().setFreshUntil(Timestamp.newBuilder().setSeconds(150)))
            .build()
        assertEquals(ViewPolicy.content(original), ViewPolicy.content(renewed))
        assertNotEquals(ViewPolicy.content(original), ViewPolicy.content(renewed.toBuilder()
            .setUsage(renewed.usage.toBuilder().setActualCostMicros(2000000)).build()))
    }
}
