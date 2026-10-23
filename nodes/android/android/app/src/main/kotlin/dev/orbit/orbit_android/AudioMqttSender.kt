package dev.orbit.orbit_android

import org.eclipse.paho.client.mqttv3.*

/** QoS 0 completion is local send success; no subscription or application ACK. */
class AudioMqttSender(
    private val client: MqttAsyncClient,
    private val topic: String,
    private val dispatch: (() -> Unit) -> Unit,
) {
    fun send(payload: ByteArray, complete: (Boolean) -> Unit) {
        client.publish(topic, payload, 0, false, null, object : IMqttActionListener {
            override fun onSuccess(token: IMqttToken?) { dispatch { complete(true) } }
            override fun onFailure(token: IMqttToken?, error: Throwable?) { dispatch { complete(false) } }
        })
    }
}
