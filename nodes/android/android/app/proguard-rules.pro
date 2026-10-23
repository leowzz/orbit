# Paho 1.2.5 creates its logger by reflection and loads network transports via
# ServiceLoader. Removing their constructors makes MQTT fail before connecting.
# Keep this small dependency intact in both Android applications.
-keep class org.eclipse.paho.client.mqttv3.** { *; }
