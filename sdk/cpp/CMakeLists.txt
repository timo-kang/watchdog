cmake_minimum_required(VERSION 3.16)

project(watchdog_cpp VERSION 0.1.0 LANGUAGES CXX)

include(CMakePackageConfigHelpers)
include(GNUInstallDirs)

add_library(watchdog_cpp INTERFACE)
add_library(watchdog::cpp ALIAS watchdog_cpp)

set_target_properties(watchdog_cpp PROPERTIES EXPORT_NAME cpp)

target_compile_features(watchdog_cpp INTERFACE cxx_std_17)
target_include_directories(
  watchdog_cpp
  INTERFACE
    $<BUILD_INTERFACE:${CMAKE_CURRENT_SOURCE_DIR}/include>
    $<INSTALL_INTERFACE:${CMAKE_INSTALL_INCLUDEDIR}>)

set(WATCHDOG_CPP_DEFAULT_BUILD_TOOLS OFF)
if(CMAKE_SOURCE_DIR STREQUAL PROJECT_SOURCE_DIR)
  set(WATCHDOG_CPP_DEFAULT_BUILD_TOOLS ON)
endif()

option(WATCHDOG_CPP_BUILD_EXAMPLES "Build watchdog C++ SDK examples" ${WATCHDOG_CPP_DEFAULT_BUILD_TOOLS})
option(WATCHDOG_CPP_BUILD_TESTS "Build watchdog C++ SDK tests" ${WATCHDOG_CPP_DEFAULT_BUILD_TOOLS})

if(WATCHDOG_CPP_BUILD_EXAMPLES)
  add_executable(watchdog_send_heartbeat examples/send_heartbeat.cpp)
  target_link_libraries(watchdog_send_heartbeat PRIVATE watchdog::cpp)

  add_executable(watchdog_emit_soem_probe examples/emit_soem_probe.cpp)
  target_link_libraries(watchdog_emit_soem_probe PRIVATE watchdog::cpp)
endif()

if(WATCHDOG_CPP_BUILD_TESTS)
  enable_testing()

  add_executable(watchdog_reporter_test tests/reporter_test.cpp)
  target_link_libraries(watchdog_reporter_test PRIVATE watchdog::cpp)
  add_test(NAME watchdog_reporter_test COMMAND watchdog_reporter_test)

  add_executable(watchdog_ethercat_probe_test tests/ethercat_probe_test.cpp)
  target_link_libraries(watchdog_ethercat_probe_test PRIVATE watchdog::cpp)
  add_test(NAME watchdog_ethercat_probe_test COMMAND watchdog_ethercat_probe_test)
endif()

install(DIRECTORY include/ DESTINATION ${CMAKE_INSTALL_INCLUDEDIR})
install(TARGETS watchdog_cpp EXPORT watchdog_cppTargets)

configure_package_config_file(
  cmake/watchdog_cppConfig.cmake.in
  "${CMAKE_CURRENT_BINARY_DIR}/watchdog_cppConfig.cmake"
  INSTALL_DESTINATION "${CMAKE_INSTALL_LIBDIR}/cmake/watchdog_cpp")

write_basic_package_version_file(
  "${CMAKE_CURRENT_BINARY_DIR}/watchdog_cppConfigVersion.cmake"
  VERSION ${PROJECT_VERSION}
  COMPATIBILITY SameMajorVersion)

install(
  EXPORT watchdog_cppTargets
  NAMESPACE watchdog::
  DESTINATION "${CMAKE_INSTALL_LIBDIR}/cmake/watchdog_cpp")

install(
  FILES
    "${CMAKE_CURRENT_BINARY_DIR}/watchdog_cppConfig.cmake"
    "${CMAKE_CURRENT_BINARY_DIR}/watchdog_cppConfigVersion.cmake"
  DESTINATION "${CMAKE_INSTALL_LIBDIR}/cmake/watchdog_cpp")
