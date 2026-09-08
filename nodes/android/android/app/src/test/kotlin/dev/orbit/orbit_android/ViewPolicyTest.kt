package dev.orbit.orbit_android

import com.google.protobuf.Timestamp
import orbit.v1.Common.Metadata
import orbit.v1.View.DeviceView
import orbit.v1.View.Freshness
import org.junit.Assert.*
import org.junit.Test

class ViewPolicyTest {
    private val now = 1700000000000L
    private fun stamp(ms: Long) = Timestamp.newBuilder().setSeconds(ms / 1000).build()
    private fun view() = DeviceView.newBuilder().setNodeId("phone").setCoreEpoch("epoch")
        .setMetadata(Metadata.newBuilder().setProducerId("core").setRevision(1).setProducedAt(stamp(now)))
        .setRetainUntil(stamp(now + 60000)).build()
    @Test fun rejectsWrongNodeExpiredAndDuplicate() {
        val view = view()
        assertTrue(ViewPolicy.valid(view, null, "phone", now))
        assertFalse(ViewPolicy.valid(view, null, "other", now))
        assertFalse(ViewPolicy.valid(view, null, "phone", now + 60000))
        assertFalse(ViewPolicy.valid(view, view, "phone", now))
        assertFalse(ViewPolicy.valid(view.toBuilder().setCoreEpoch("old").setMetadata(view.metadata.toBuilder()
            .setProducedAt(stamp(now - 1000))).build(), view, "phone", now))
    }
    @Test fun acceptsNewerRevisionAndNewEpoch() {
        val view = view()
        assertTrue(ViewPolicy.valid(view.toBuilder().setMetadata(view.metadata.toBuilder().setRevision(2)).build(), view, "phone", now))
        assertTrue(ViewPolicy.valid(view.toBuilder().setCoreEpoch("new").setMetadata(view.metadata.toBuilder()
            .setProducedAt(stamp(now + 1000))).build(), view, "phone", now + 1000))
    }
    @Test fun expiresSectionsIndependentlyOfConnection() {
        assertEquals("数据正常", ViewPolicy.freshness(Freshness.FRESHNESS_FRESH, stamp(now + 1000), now))
        assertEquals("数据已过期", ViewPolicy.freshness(Freshness.FRESHNESS_FRESH, stamp(now), now))
        assertEquals("离线", ViewPolicy.freshness(Freshness.FRESHNESS_OFFLINE, stamp(now + 1000), now))
    }
    @Test fun validatesAddressAndTopicIdentity() {
        NodeConfig("ssl://broker.example.com:8883", "phone-1", "user", "password").validate()
        assertEquals("ssl://broker:8883", NodeConfig.from(mapOf("uri" to "mqtts://broker:8883", "nodeId" to "phone")).uri)
        for (config in listOf(NodeConfig("https://broker:443", "phone", "", ""),
            NodeConfig("ssl://broker:8883", "phone/#", "", ""), NodeConfig("ssl://user:secret@broker:8883", "phone", "", ""))) {
            assertThrows(IllegalArgumentException::class.java) { config.validate() }
        }
    }
}
