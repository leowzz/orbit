package dev.orbit.orbit_android

import org.eclipse.paho.client.mqttv3.MqttException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Test
import java.util.MissingResourceException

class MqttErrorsTest {
    @Test fun missingComponentDoesNotBlameNetworkOrCredentials() {
        val message = OrbitMqtt.safeError(MissingResourceException("private detail", "logger", "key"))
        assertEquals("MQTT 组件初始化失败，请更新应用", message)
    }

    @Test fun mqttFailuresRemainDistinguishable() {
        assertEquals("认证失败或没有连接权限", OrbitMqtt.safeError(MqttException(5)))
        assertEquals("连接超时", OrbitMqtt.safeError(MqttException(32000)))
        assertEquals("MQTT 操作失败（32109）", OrbitMqtt.safeError(MqttException(32109)))
    }

    @Test fun diagnosticMessagesDoNotExposeExceptionDetails() {
        for (error in listOf(
            IllegalArgumentException("password=private-detail"),
            MqttException(IllegalStateException("password=private-detail")),
        )) {
            assertFalse(OrbitMqtt.safeError(error).contains("private-detail"))
        }
    }
}
