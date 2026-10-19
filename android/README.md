# Android native packaging

Only the `arm64-v8a` ABI has a verified native build in the repository. Do **not**
ship x86, x86_64 or armeabi-v7a in the same application unless the matching
binaries are added and tested.

Copy `native/assets/libztgo_android_arm64.so` to
`app/src/main/jniLibs/arm64-v8a/libztgo.so` before gomobile/Gradle packaging.
The loader uses Android's packaged `libztgo.so`, not executable cache files.

Android app Gradle configuration example:

```kotlin
android {
    defaultConfig {
        ndk { abiFilters += setOf("arm64-v8a") }
    }
}
```

The CI `llvm-readelf -lW` assertion requires `0x4000` LOAD alignment on this
binary. An Android device runtime and APK smoke test remain a separate
validation requirement.
