import com.google.protobuf.gradle.*

plugins {
    id("com.android.application")
    id("com.google.protobuf") version "0.9.5"
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// CI supplies a persistent release keystore through environment variables.
val releaseStore = providers.environmentVariable("ANDROID_KEYSTORE_PATH").orNull
fun signingSecret(name: String): String = providers.environmentVariable(name).orNull
    ?.takeIf { it.isNotBlank() } ?: error("Missing Android release signing variable: $name")

android {
    namespace = "dev.orbit.orbit_android"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "dev.orbit.orbit_android"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = 26
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (releaseStore != null) {
            create("release") {
                storeFile = file(releaseStore)
                storePassword = signingSecret("ANDROID_KEYSTORE_PASSWORD")
                keyAlias = signingSecret("ANDROID_KEY_ALIAS")
                keyPassword = signingSecret("ANDROID_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            // Never silently distribute an APK signed with the debug key.
            signingConfig = signingConfigs.findByName("release")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

// Compile the canonical Orbit schema; no copied wire definitions.
android.sourceSets.getByName("main").proto { srcDir("../../../../proto") }
protobuf {
    protoc { artifact = "com.google.protobuf:protoc:3.25.5" }
    generateProtoTasks {
        all().configureEach { builtins { maybeCreate("java").option("lite") } }
    }
}
dependencies {
    implementation("com.google.protobuf:protobuf-javalite:3.25.5")
    implementation("org.eclipse.paho:org.eclipse.paho.client.mqttv3:1.2.5")
    testImplementation("junit:junit:4.13.2")
}

// Debug builds remain available without release credentials.
gradle.taskGraph.whenReady {
    if (allTasks.any { it.project == project && it.name.contains("Release") }) {
        require(!releaseStore.isNullOrBlank()) {
            "Release builds require ANDROID_KEYSTORE_PATH and Android signing variables; see docs/releases.md"
        }
    }
}
