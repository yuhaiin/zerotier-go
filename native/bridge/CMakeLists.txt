cmake_minimum_required(VERSION 3.15)
add_library(ztgo SHARED ztgo.cpp "${CMAKE_SOURCE_DIR}/osdep/OSUtils.cpp")
target_include_directories(ztgo PRIVATE "${CMAKE_SOURCE_DIR}/include")
target_compile_features(ztgo PRIVATE cxx_std_17)
target_link_libraries(ztgo PRIVATE zerotier-core nlohmann_json::nlohmann_json)
if(WIN32)
  target_link_libraries(ztgo PRIVATE ws2_32)
endif()
set_target_properties(ztgo PROPERTIES OUTPUT_NAME "ztgo")

# Android 15+ devices can use 16 KiB virtual memory pages. Every PT_LOAD
# segment must be aligned to 0x4000, not the old 0x1000 default.
if(ANDROID)
  target_link_options(ztgo PRIVATE "-Wl,-z,max-page-size=16384" "-Wl,-z,common-page-size=16384")
endif()
