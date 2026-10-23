package dev.orbit.orbit_android

import org.eclipse.paho.client.mqttv3.*
import org.eclipse.paho.client.mqttv3.persist.MemoryPersistence
import org.junit.Assert.*
import org.junit.Test
import java.io.DataInputStream
import java.net.InetAddress
import java.net.ServerSocket
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

class AudioMqttSenderTest {
    private fun packet(input: DataInputStream): Pair<Int, ByteArray> {
        val header = input.readUnsignedByte()
        var length = 0
        var multiplier = 1
        do {
            val digit = input.readUnsignedByte()
            length += (digit and 127) * multiplier
            multiplier *= 128
        } while (digit and 128 != 0)
        return header to ByteArray(length).also { input.readFully(it) }
    }

    @Test fun localPublishCompletesWithoutSubscriberOrAnyPublishAck() {
        val server = ServerSocket(0, 1, InetAddress.getByName("127.0.0.1"))
        val received = CountDownLatch(1)
        val releaseServer = CountDownLatch(1)
        val failure = AtomicReference<Throwable?>()
        val broker = Thread {
            try {
                server.accept().use { socket ->
                    socket.soTimeout = 5000
                    val input = DataInputStream(socket.getInputStream())
                    assertEquals(0x10, packet(input).first) // CONNECT
                    socket.getOutputStream().apply { write(byteArrayOf(0x20, 2, 0, 0)); flush() }
                    repeat(2) { index ->
                        val (header, payload) = packet(input)
                        assertEquals(0x30, header) // PUBLISH, QoS 0, not retained. No SUBSCRIBE.
                        val body = DataInputStream(payload.inputStream())
                        assertEquals("orbit/v1/nodes/test/audio/segments", body.readUTF())
                        assertEquals("segment-$index", String(body.readBytes(), Charsets.UTF_8))
                        // Deliberately send no PUBACK and no Core stored receipt.
                    }
                    received.countDown()
                    releaseServer.await(5, TimeUnit.SECONDS)
                }
            } catch (error: Throwable) { failure.set(error); received.countDown() }
        }.apply { isDaemon = true; start() }
        val client = MqttAsyncClient("tcp://127.0.0.1:${server.localPort}", "audio-send-test", MemoryPersistence())
        try {
            client.connect(MqttConnectOptions().apply {
                mqttVersion = MqttConnectOptions.MQTT_VERSION_3_1_1
                connectionTimeout = 3
            }).waitForCompletion(5000)
            val sent = CountDownLatch(2)
            val sender = AudioMqttSender(client, "orbit/v1/nodes/test/audio/segments", { it() })
            repeat(2) { index -> sender.send("segment-$index".toByteArray()) { success ->
                if (!success) failure.set(AssertionError("Local send failed"))
                sent.countDown()
            } }
            assertTrue("Local publish must complete without an ACK", sent.await(5, TimeUnit.SECONDS))
            assertTrue(received.await(5, TimeUnit.SECONDS))
            failure.get()?.let { throw it }
        } finally {
            runCatching { client.disconnectForcibly(0, 0, false) }
            runCatching { client.close(true) }
            releaseServer.countDown()
            server.close()
            broker.join(1000)
        }
    }
}
