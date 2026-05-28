cmake_minimum_required(VERSION 3.16)

project(watchdog_cpp_consumer_example LANGUAGES CXX)

find_package(watchdog_cpp CONFIG REQUIRED)

add_executable(watchdog_cpp_consumer main.cpp)
target_link_libraries(watchdog_cpp_consumer PRIVATE watchdog::cpp)
